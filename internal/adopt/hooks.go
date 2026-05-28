package adopt

import (
	"crypto/sha1"
	"encoding/json"
	"errors"
	"fmt"
	iofs "io/fs"
	"os"
	"sort"
	"strings"

	"github.com/MHilhorst/ainfra/internal/manifest"
)

// readHooks scans the given settings.json for the "hooks" block and
// synthesizes one manifest.Hook entry per (event, matcher, command) triple it
// finds. IDs are stable: event + matcher + sha8(command).
func readHooks(path string) (map[string]manifest.Hook, []Warning, error) {
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

	type rawHook struct {
		event, matcher, command string
		timeoutMs               int
	}
	var raws []rawHook
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
				// Skip ainfra-owned hooks (e.g. the staleness-check that
				// 'ainfra install' injects). These are implementation
				// details, not user content — including them in the
				// adopted manifest would round-trip them into ainfra.yaml
				// and surface them in 'inspect' as if the user wrote them.
				if isAinfraOwnedHookCommand(command) {
					continue
				}
				timeoutSec, _ := hm["timeout"].(float64)
				raws = append(raws, rawHook{
					event:     event,
					matcher:   matcher,
					command:   command,
					timeoutMs: int(timeoutSec) * 1000, // settings.json stores seconds; manifest stores ms.
				})
			}
		}
	}

	// Two-pass id assignment: first try a short id ("<event>-<matcher>"),
	// fall back to a hash-suffixed id only when two hooks would collide.
	// Avoids cryptic suffixes like "pretooluse-bash-172d96a1" when there's
	// just one PreToolUse/Bash hook in the file.
	out := map[string]manifest.Hook{}
	shortCounts := map[string]int{}
	for _, r := range raws {
		shortCounts[shortHookID(r.event, r.matcher)]++
	}
	for _, r := range raws {
		short := shortHookID(r.event, r.matcher)
		id := short
		if shortCounts[short] > 1 {
			id = synthesizeHookID(r.event, r.matcher, r.command)
		}
		out[id] = manifest.Hook{
			Event:   r.event,
			Matcher: r.matcher,
			Command: r.command,
			Timeout: r.timeoutMs,
		}
	}

	if len(out) > 0 {
		warnings = append(warnings, Warning{
			Message: "adopt: hooks adopted from settings.json get auto-assigned IDs; rename in ainfra.yaml if you want different names",
		})
	}
	return out, warnings, nil
}

// isAinfraOwnedHookCommand reports whether a settings.json hook command is
// one ainfra itself manages (it injects the staleness-check on every install,
// and any future built-ins will live under the same prefix). These are not
// user content and must not be round-tripped through adopt.
func isAinfraOwnedHookCommand(command string) bool {
	c := strings.TrimSpace(command)
	return strings.HasPrefix(c, "ainfra _") || strings.HasPrefix(c, "ainfra staleness-check")
}

// shortHookID returns the readable form of a hook id: "<event>-<matcher>"
// with matcher omitted when empty. Used as the first-choice id, with the
// hash-suffixed form reserved for actual collisions.
func shortHookID(event, matcher string) string {
	parts := []string{strings.ToLower(event)}
	if matcher != "" {
		parts = append(parts, slug(matcher))
	}
	return strings.Join(parts, "-")
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

var _ = iofs.ErrNotExist // keep import in case future readers want it
