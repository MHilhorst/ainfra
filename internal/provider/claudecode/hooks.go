package claudecode

import (
	"fmt"
	"path/filepath"
	"sort"

	"github.com/MHilhorst/ainfra/internal/provider"
	"github.com/MHilhorst/ainfra/internal/provider/fsmerge"
)

// Hooks reconciles the "hooks" key of <root>/.claude/settings.json. Claude Code
// keys hooks by lifecycle event; each event holds an array of matcher groups,
// each group being {matcher?, hooks: [{type, command, timeout?}]}. ainfra owns
// the event keys it writes and leaves the rest of settings.json untouched.
type Hooks struct{}

// Channel returns the channel name this provider manages.
func (Hooks) Channel() string { return "hooks" }

func hooksPath(env provider.Env) string {
	return filepath.Join(env.Root, ".claude", "settings.json")
}

// Observe returns nil. Claude Code's hooks schema groups hooks by event, not by
// ainfra hook id, so the stored shape cannot be mapped back to individual
// managed hooks. Hooks are therefore reconciled wholesale on every apply — the
// same approach the CLITools channel takes.
func (Hooks) Observe(_ provider.Env) ([]provider.Resource, error) {
	return nil, nil
}

// Apply writes the managed hooks into .claude/settings.json in Claude Code's
// event-keyed schema. When env.DryRun is true the result is computed but no
// file is modified.
func (Hooks) Apply(env provider.Env, plan provider.ChannelPlan) (provider.ApplyResult, error) {
	changes := append([]provider.Change(nil), plan.Changes...)
	sort.Slice(changes, func(i, j int) bool { return changes[i].ID < changes[j].ID })

	byEvent := map[string][]any{}
	var applied []provider.Change

	for _, c := range changes {
		if c.Kind != provider.ChangeCreate && c.Kind != provider.ChangeUpdate {
			continue
		}
		event, _ := c.Resource.Payload["event"].(string)
		if event == "" {
			continue
		}
		byEvent[event] = append(byEvent[event], buildMatcherGroup(c.Resource.Payload))
		applied = append(applied, c)

		// Install the hook's bundled source script, if any, under
		// <root>/.ainfra/run/ so the hook's `command` can reference it.
		if !env.DryRun {
			if name, _ := c.Resource.Payload["scriptName"].(string); name != "" {
				content, _ := c.Resource.Payload["scriptContent"].(string)
				dst := filepath.Join(env.Root, ".ainfra", "run", name)
				if err := fsmerge.WriteOwnedFile(env.FS, dst, []byte(content)); err != nil {
					return provider.ApplyResult{}, fmt.Errorf("hooks: installing script %q: %w", name, err)
				}
			}
		}
	}

	if len(byEvent) == 0 {
		return provider.ApplyResult{Channel: "hooks"}, nil
	}

	desired := make(map[string]any, len(byEvent))
	ownedKeys := make([]string, 0, len(byEvent))
	for event, groups := range byEvent {
		desired[event] = groups
		ownedKeys = append(ownedKeys, event)
	}
	sort.Strings(ownedKeys)

	if !env.DryRun {
		if err := fsmerge.MergeJSONKeys(env.FS, hooksPath(env), "hooks", desired, ownedKeys); err != nil {
			return provider.ApplyResult{}, err
		}
	}

	return provider.ApplyResult{Channel: "hooks", Applied: applied}, nil
}

// buildMatcherGroup builds one Claude Code matcher group from a hook payload:
// {matcher?, hooks: [{type: "command", command, timeout?}]}. An empty matcher
// is omitted (valid for non-tool events like SessionStart).
func buildMatcherGroup(payload map[string]any) map[string]any {
	hook := map[string]any{"type": "command"}
	if command, ok := payload["command"].(string); ok && command != "" {
		hook["command"] = command
	}
	if secs := timeoutSeconds(payload["timeout"]); secs > 0 {
		hook["timeout"] = secs
	}
	group := map[string]any{"hooks": []any{hook}}
	if matcher, ok := payload["matcher"].(string); ok && matcher != "" {
		group["matcher"] = matcher
	}
	return group
}

// timeoutSeconds converts a manifest hook timeout — declared in milliseconds —
// to the whole seconds Claude Code's settings.json expects, rounding up so a
// sub-second timeout never collapses to zero.
func timeoutSeconds(v any) int {
	var ms int
	switch t := v.(type) {
	case int:
		ms = t
	case int64:
		ms = int(t)
	case float64:
		ms = int(t)
	}
	if ms <= 0 {
		return 0
	}
	return (ms + 999) / 1000
}
