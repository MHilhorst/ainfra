package channels

import (
	"encoding/json"
	"errors"
	iofs "io/fs"
	"path/filepath"

	"github.com/MHilhorst/ainfra/internal/provider"
	"github.com/MHilhorst/ainfra/internal/provider/fsmerge"
)

// Tools reconciles the permissions and disabledTools keys in
// <root>/.claude/settings.json. ainfra manages two top-level keys:
// "permissions" (containing "allow" and "deny" arrays) and "disabledTools"
// (an array of built-in tool names). Both keys are treated as a single logical
// resource with ID "tools".
type Tools struct{}

// Channel returns the channel name this provider manages.
func (Tools) Channel() string { return "tools" }

func toolsPath(env provider.Env) string {
	return filepath.Join(env.Root, ".claude", "settings.json")
}

// Observe reads .claude/settings.json and returns a single Resource with ID
// "tools" if either of the managed keys ("permissions" or "disabledTools") is
// present. A missing file is treated as no resources.
func (Tools) Observe(env provider.Env) ([]provider.Resource, error) {
	raw, err := env.FS.ReadFile(toolsPath(env))
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

	_, hasPerms := doc["permissions"]
	_, hasDisabled := doc["disabledTools"]
	if !hasPerms && !hasDisabled {
		return nil, nil
	}

	return []provider.Resource{
		{ID: "tools", Channel: "tools"},
	}, nil
}

// Apply executes the channel plan against .claude/settings.json. When
// env.DryRun is true, it computes the result but does not write the file.
// It merges "permissions" and "disabledTools" as two separate managed top-level
// keys, each owned entirely by ainfra.
func (Tools) Apply(env provider.Env, plan provider.ChannelPlan) (provider.ApplyResult, error) {
	var applied []provider.Change
	hasChange := false

	var desiredPerms map[string]any
	var desiredDisabled []any
	isDelete := false

	for _, c := range plan.Changes {
		if c.Kind == provider.ChangeNoop {
			continue
		}
		hasChange = true
		applied = append(applied, c)

		if c.Kind == provider.ChangeCreate || c.Kind == provider.ChangeUpdate {
			if allow, ok := c.Resource.Payload["allow"]; ok {
				if desiredPerms == nil {
					desiredPerms = map[string]any{}
				}
				desiredPerms["allow"] = allow
			}
			if deny, ok := c.Resource.Payload["deny"]; ok {
				if desiredPerms == nil {
					desiredPerms = map[string]any{}
				}
				desiredPerms["deny"] = deny
			}
			if disabled, ok := c.Resource.Payload["disabled"]; ok {
				if d, ok := disabled.([]any); ok {
					desiredDisabled = d
				}
			}
		} else if c.Kind == provider.ChangeDelete {
			isDelete = true
		}
	}

	if !hasChange {
		return provider.ApplyResult{Channel: "tools"}, nil
	}

	if !env.DryRun {
		path := toolsPath(env)

		if isDelete {
			// Remove both managed keys by passing empty desired and their names as ownedKeys.
			if err := fsmerge.MergeJSONKeys(env.FS, path, "permissions", map[string]any{}, []string{"allow", "deny"}); err != nil {
				return provider.ApplyResult{}, err
			}
			if err := fsmerge.MergeJSONKeys(env.FS, path, "disabledTools", map[string]any{}, []string{"__managed__"}); err != nil {
				return provider.ApplyResult{}, err
			}
			// For disabledTools, MergeJSONKeys treats it as an object key, so we need
			// to remove the top-level key differently. Use a direct approach.
			if err := removeTopLevelKey(env.FS, path, "disabledTools"); err != nil {
				return provider.ApplyResult{}, err
			}
			// Also clean up empty permissions object.
			if err := removeTopLevelKeyIfEmpty(env.FS, path, "permissions"); err != nil {
				return provider.ApplyResult{}, err
			}
		} else {
			// Merge permissions object.
			if desiredPerms != nil {
				if err := fsmerge.MergeJSONKeys(env.FS, path, "permissions", desiredPerms, []string{"allow", "deny"}); err != nil {
					return provider.ApplyResult{}, err
				}
			}
			// Write disabledTools as a top-level array key.
			if desiredDisabled != nil {
				if err := writeTopLevelArray(env.FS, path, "disabledTools", desiredDisabled); err != nil {
					return provider.ApplyResult{}, err
				}
			}
		}
	}

	return provider.ApplyResult{
		Channel: "tools",
		Applied: applied,
	}, nil
}

// removeTopLevelKey removes a top-level key from the JSON file at path.
func removeTopLevelKey(fs provider.Filesystem, path, key string) error {
	raw, err := fs.ReadFile(path)
	if errors.Is(err, iofs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}

	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return err
	}

	delete(doc, key)

	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return fs.WriteFile(path, out, 0o644)
}

// removeTopLevelKeyIfEmpty removes a top-level key if its value is an empty map.
func removeTopLevelKeyIfEmpty(fs provider.Filesystem, path, key string) error {
	raw, err := fs.ReadFile(path)
	if errors.Is(err, iofs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}

	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return err
	}

	if m, ok := doc[key].(map[string]any); ok && len(m) == 0 {
		delete(doc, key)
		out, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			return err
		}
		return fs.WriteFile(path, out, 0o644)
	}
	return nil
}

// writeTopLevelArray sets a top-level array key in the JSON file at path.
func writeTopLevelArray(fs provider.Filesystem, path, key string, values []any) error {
	raw, err := fs.ReadFile(path)
	if errors.Is(err, iofs.ErrNotExist) {
		raw = []byte("{}")
	} else if err != nil {
		return err
	}

	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		doc = map[string]any{}
	}

	doc[key] = values

	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}

	if err := fs.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return fs.WriteFile(path, out, 0o644)
}
