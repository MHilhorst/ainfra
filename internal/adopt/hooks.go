package adopt

import (
	"crypto/sha1"
	"encoding/json"
	"errors"
	"fmt"
	iofs "io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/MHilhorst/ainfra/internal/manifest"
)

// readHooks scans <dir>/.claude/settings.json for the "hooks" block and
// synthesizes one manifest.Hook entry per (event, matcher, command) triple it
// finds. IDs are stable: event + matcher + sha8(command).
func readHooks(dir string) (map[string]manifest.Hook, []Warning, error) {
	path := filepath.Join(dir, ".claude", "settings.json")
	raw, err := os.ReadFile(path)
	if errors.Is(err, iofs.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("adopt: read %s: %w", path, err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, nil, fmt.Errorf("adopt: parse %s: %w", path, err)
	}
	hooksBlock, ok := doc["hooks"].(map[string]any)
	if !ok || len(hooksBlock) == 0 {
		return nil, nil, nil
	}

	out := map[string]manifest.Hook{}
	var warnings []Warning

	events := make([]string, 0, len(hooksBlock))
	for ev := range hooksBlock {
		events = append(events, ev)
	}
	sort.Strings(events)

	for _, event := range events {
		groups, ok := hooksBlock[event].([]any)
		if !ok {
			continue
		}
		for _, group := range groups {
			gm, ok := group.(map[string]any)
			if !ok {
				continue
			}
			matcher, _ := gm["matcher"].(string)
			hooksList, _ := gm["hooks"].([]any)
			for _, h := range hooksList {
				hm, ok := h.(map[string]any)
				if !ok {
					continue
				}
				command, _ := hm["command"].(string)
				if command == "" {
					continue
				}
				timeoutSec, _ := hm["timeout"].(float64)
				id := synthesizeHookID(event, matcher, command)
				out[id] = manifest.Hook{
					Event:   event,
					Matcher: matcher,
					Command: command,
					Timeout: int(timeoutSec) * 1000, // settings.json stores seconds; manifest stores ms.
				}
			}
		}
	}

	if len(out) > 0 {
		warnings = append(warnings, Warning{
			Message: "adopt: observed hooks can't be mapped back to ainfra IDs without the applied ledger; synthesized stable IDs from event+matcher+sha8-of-command",
		})
	}
	return out, warnings, nil
}

// synthesizeHookID builds a stable, human-readable hook id from its event,
// matcher, and command, suffixed with a short hash so two hooks sharing the
// same (event, matcher) but differing in command don't collide.
func synthesizeHookID(event, matcher, command string) string {
	parts := []string{strings.ToLower(event)}
	if matcher != "" {
		parts = append(parts, slug(matcher))
	}
	sum := sha1.Sum([]byte(command))
	parts = append(parts, fmt.Sprintf("%x", sum[:4]))
	return strings.Join(parts, "-")
}

// slug normalizes an arbitrary matcher string into something safe for use as
// part of a YAML map key — lowercase, alphanumerics and dashes only.
func slug(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '|' || r == ' ' || r == '_' || r == '/' || r == '.':
			b.WriteRune('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		out = "any"
	}
	return out
}

// hooksDirExists reports whether <dir>/.claude/hooks/ exists; used by the
// orchestrator to surface a notice that bundled hook scripts may need manual
// declaration.
func hooksDirExists(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, ".claude", "hooks"))
	return err == nil && info.IsDir()
}

var _ = iofs.ErrNotExist // keep import in case future readers want it
