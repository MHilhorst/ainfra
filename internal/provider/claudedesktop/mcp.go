// Package claudedesktop contains the Claude Desktop app channel providers.
// Claude Desktop reads a single JSON config file; ainfra reconciles only its
// mcpServers object. See docs/superpowers/specs/2026-05-22-subscriber-mode-design.md.
package claudedesktop

import (
	"encoding/json"
	"errors"
	iofs "io/fs"
	"path/filepath"
	"runtime"

	"github.com/MHilhorst/ainfra/internal/provider"
	"github.com/MHilhorst/ainfra/internal/provider/fsmerge"
)

// MCP reconciles entries in claude_desktop_config.json under "mcpServers".
type MCP struct{}

// Channel returns the channel name this provider manages.
func (MCP) Channel() string { return "mcpServers" }

// configPath returns the OS-specific Claude Desktop config file path.
func configPath(env provider.Env) string {
	switch runtime.GOOS {
	case "windows":
		return filepath.Join(env.Home, "AppData", "Roaming", "Claude", "claude_desktop_config.json")
	default: // darwin and anything else
		return filepath.Join(env.Home, "Library", "Application Support", "Claude", "claude_desktop_config.json")
	}
}

// Observe reads the config file and returns a Resource per mcpServers key.
// A missing file is treated as no resources.
func (MCP) Observe(env provider.Env) ([]provider.Resource, error) {
	raw, err := env.FS.ReadFile(configPath(env))
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
	servers, ok := doc["mcpServers"].(map[string]any)
	if !ok {
		return nil, nil
	}
	resources := make([]provider.Resource, 0, len(servers))
	for key := range servers {
		resources = append(resources, provider.Resource{ID: key, Channel: "mcpServers"})
	}
	return resources, nil
}

// Apply executes the channel plan against the config file. When env.DryRun is
// true the result is computed but the file is not written.
func (MCP) Apply(env provider.Env, plan provider.ChannelPlan) (provider.ApplyResult, error) {
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
			desired[c.ID] = buildServerObject(c.Resource.Payload)
		}
	}
	if len(ownedKeys) == 0 {
		return provider.ApplyResult{Channel: "mcpServers"}, nil
	}
	if !env.DryRun {
		if err := fsmerge.MergeJSONKeys(env.FS, configPath(env), "mcpServers", desired, ownedKeys); err != nil {
			return provider.ApplyResult{}, err
		}
	}
	return provider.ApplyResult{Channel: "mcpServers", Applied: applied}, nil
}

// buildServerObject constructs a Claude Desktop mcpServers entry from a
// resource payload. Optional fields absent from the payload are omitted.
func buildServerObject(payload map[string]any) map[string]any {
	obj := map[string]any{}
	if v, ok := payload["command"]; ok && v != nil && v != "" {
		obj["command"] = v
	}
	if v, ok := payload["args"]; ok && v != nil {
		obj["args"] = v
	}
	if v, ok := payload["env"]; ok && v != nil {
		obj["env"] = v
	}
	if v, ok := payload["transport"]; ok && v != nil && v != "" {
		obj["type"] = v
	}
	if v, ok := payload["url"]; ok && v != nil && v != "" {
		obj["url"] = v
	}
	if v, ok := payload["headers"]; ok && v != nil {
		obj["headers"] = v
	}
	return obj
}
