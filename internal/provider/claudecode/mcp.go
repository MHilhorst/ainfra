// Package claudecode contains the Claude Code channel providers for the ainfra
// reconciler. Each type implements provider.Provider for one channel kind.
package claudecode

import (
	"encoding/json"
	"errors"
	iofs "io/fs"
	"path/filepath"

	"github.com/MHilhorst/ainfra/internal/provider"
	"github.com/MHilhorst/ainfra/internal/provider/fsmerge"
)

// MCP reconciles entries in <root>/.mcp.json under the "mcpServers" top-level key.
type MCP struct{}

// Channel returns the channel name this provider manages.
func (MCP) Channel() string { return "mcpServers" }

func mcpPath(env provider.Env) string {
	return filepath.Join(env.Root, ".mcp.json")
}

// Observe reads .mcp.json and returns a Resource for each key under mcpServers.
// A missing file is treated as no resources. ContentHash is left empty; the
// orchestrator backfills it from the ledger.
func (MCP) Observe(env provider.Env) ([]provider.Resource, error) {
	raw, err := env.FS.ReadFile(mcpPath(env))
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
		resources = append(resources, provider.Resource{
			ID:      key,
			Channel: "mcpServers",
		})
	}
	return resources, nil
}

// Apply executes the channel plan against .mcp.json. When env.DryRun is true,
// it computes the result but does not write the file.
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
			desired[c.ID] = buildMCPServerObject(c.Resource.Payload)
		}
		// ChangeDelete: contributes to ownedKeys but not desired, so the merge removes it.
	}

	if len(ownedKeys) == 0 {
		return provider.ApplyResult{Channel: "mcpServers"}, nil
	}

	if !env.DryRun {
		if err := fsmerge.MergeJSONKeys(env.FS, mcpPath(env), "mcpServers", desired, ownedKeys); err != nil {
			return provider.ApplyResult{}, err
		}
	}

	return provider.ApplyResult{
		Channel: "mcpServers",
		Applied: applied,
	}, nil
}

// buildMCPServerObject constructs the server entry map from a resource payload.
// Nil or missing optional fields are omitted.
func buildMCPServerObject(payload map[string]any) map[string]any {
	obj := map[string]any{}

	if cmd, ok := payload["command"]; ok && cmd != nil && cmd != "" {
		obj["command"] = cmd
	}
	if args, ok := payload["args"]; ok && args != nil {
		obj["args"] = args
	}
	if env, ok := payload["env"]; ok && env != nil {
		obj["env"] = env
	}
	if transport, ok := payload["transport"]; ok && transport != nil && transport != "" {
		obj["type"] = transport
	}
	if url, ok := payload["url"]; ok && url != nil && url != "" {
		obj["url"] = url
	}
	if headers, ok := payload["headers"]; ok && headers != nil {
		obj["headers"] = headers
	}

	return obj
}
