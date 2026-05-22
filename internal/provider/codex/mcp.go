// Package codex contains the Codex channel providers for the ainfra
// reconciler. Each type implements provider.Provider for one channel kind,
// rendering into the config files the Codex CLI reads.
package codex

import (
	"errors"
	iofs "io/fs"
	"path/filepath"

	"github.com/BurntSushi/toml"
	"github.com/MHilhorst/ainfra/internal/provider"
	"github.com/MHilhorst/ainfra/internal/provider/fsmerge"
)

// MCP reconciles MCP servers into ~/.codex/config.toml under the
// [mcp_servers.<id>] tables.
type MCP struct{}

// Channel returns the channel name this provider manages.
func (MCP) Channel() string { return "mcpServers" }

func configPath(env provider.Env) string {
	return filepath.Join(env.Home, ".codex", "config.toml")
}

// Observe reads config.toml and returns a Resource for each key under
// [mcp_servers]. A missing file is treated as no resources. ContentHash is
// left empty; the orchestrator backfills it from the ledger.
func (MCP) Observe(env provider.Env) ([]provider.Resource, error) {
	raw, err := env.FS.ReadFile(configPath(env))
	if errors.Is(err, iofs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	doc := map[string]any{}
	if len(raw) > 0 {
		if err := toml.Unmarshal(raw, &doc); err != nil {
			return nil, err
		}
	}

	servers, ok := doc["mcp_servers"].(map[string]any)
	if !ok {
		return nil, nil
	}

	resources := make([]provider.Resource, 0, len(servers))
	for key := range servers {
		resources = append(resources, provider.Resource{ID: key, Channel: "mcpServers"})
	}
	return resources, nil
}

// Apply executes the channel plan against config.toml. When env.DryRun is
// true, the result is computed but the file is not written.
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
			desired[c.ID] = buildCodexServerTable(c.Resource.Payload)
		}
		// ChangeDelete: in ownedKeys, not in desired — the merge removes it.
	}

	if len(ownedKeys) == 0 {
		return provider.ApplyResult{Channel: "mcpServers"}, nil
	}

	if !env.DryRun {
		if err := fsmerge.MergeTOMLTables(env.FS, configPath(env), "mcp_servers", desired, ownedKeys); err != nil {
			return provider.ApplyResult{}, err
		}
	}

	return provider.ApplyResult{Channel: "mcpServers", Applied: applied}, nil
}

// buildCodexServerTable constructs the [mcp_servers.<id>] table from a resource
// payload. Codex MCP servers are command-launched; the payload's transport
// field is not written. Nil or missing optional fields are omitted.
func buildCodexServerTable(payload map[string]any) map[string]any {
	table := map[string]any{}
	if cmd, ok := payload["command"]; ok && cmd != nil && cmd != "" {
		table["command"] = cmd
	}
	if args, ok := payload["args"]; ok && args != nil {
		table["args"] = args
	}
	if env, ok := payload["env"]; ok && env != nil {
		table["env"] = env
	}
	return table
}
