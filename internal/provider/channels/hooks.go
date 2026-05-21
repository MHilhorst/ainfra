package channels

import (
	"encoding/json"
	"errors"
	iofs "io/fs"
	"path/filepath"

	"github.com/MHilhorst/ainfra/internal/provider"
	"github.com/MHilhorst/ainfra/internal/provider/fsmerge"
)

// Hooks reconciles entries in <root>/.claude/settings.json under the "hooks"
// top-level key. ainfra owns one nested object per managed hook id.
type Hooks struct{}

// Channel returns the channel name this provider manages.
func (Hooks) Channel() string { return "hooks" }

func hooksPath(env provider.Env) string {
	return filepath.Join(env.Root, ".claude", "settings.json")
}

// Observe reads .claude/settings.json and returns a Resource for each key under
// hooks. A missing file is treated as no resources. ContentHash is left empty;
// the orchestrator backfills it from the ledger.
func (Hooks) Observe(env provider.Env) ([]provider.Resource, error) {
	raw, err := env.FS.ReadFile(hooksPath(env))
	if errors.Is(err, iofs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}

	hooks, ok := doc["hooks"].(map[string]any)
	if !ok {
		return nil, nil
	}

	resources := make([]provider.Resource, 0, len(hooks))
	for key := range hooks {
		resources = append(resources, provider.Resource{
			ID:      key,
			Channel: "hooks",
		})
	}
	return resources, nil
}

// Apply executes the channel plan against .claude/settings.json. When
// env.DryRun is true, it computes the result but does not write the file.
func (Hooks) Apply(env provider.Env, plan provider.ChannelPlan) (provider.ApplyResult, error) {
	desired := map[string]any{}
	ownedKeys := make([]string, 0, len(plan.Changes))
	var applied []provider.Change

	for _, c := range plan.Changes {
		if c.Kind == provider.ChangeNoop {
			continue
		}
		ownedKeys = append(ownedKeys, c.ID)
		applied = append(applied, c)

		if c.Kind == provider.ChangeCreate || c.Kind == provider.ChangeUpdate {
			desired[c.ID] = buildHookObject(c.Resource.Payload)
		}
		// ChangeDelete: contributes to ownedKeys but not desired, so the merge removes it.
	}

	if len(ownedKeys) == 0 {
		return provider.ApplyResult{Channel: "hooks"}, nil
	}

	if !env.DryRun {
		if err := fsmerge.MergeJSONKeys(env.FS, hooksPath(env), "hooks", desired, ownedKeys); err != nil {
			return provider.ApplyResult{}, err
		}
	}

	return provider.ApplyResult{
		Channel: "hooks",
		Applied: applied,
	}, nil
}

// buildHookObject constructs the hook entry map from a resource payload.
// Empty matcher and zero timeout are omitted.
func buildHookObject(payload map[string]any) map[string]any {
	obj := map[string]any{}

	if event, ok := payload["event"]; ok && event != nil {
		obj["event"] = event
	}
	if matcher, ok := payload["matcher"]; ok && matcher != nil && matcher != "" {
		obj["matcher"] = matcher
	}
	if command, ok := payload["command"]; ok && command != nil {
		obj["command"] = command
	}
	if timeout, ok := payload["timeout"]; ok && timeout != nil {
		switch v := timeout.(type) {
		case int:
			if v != 0 {
				obj["timeout"] = v
			}
		case float64:
			if v != 0 {
				obj["timeout"] = v
			}
		case int64:
			if v != 0 {
				obj["timeout"] = v
			}
		}
	}

	return obj
}
