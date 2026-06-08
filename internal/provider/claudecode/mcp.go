// Package claudecode contains the Claude Code channel providers for the ainfra
// reconciler. Each type implements provider.Provider for one channel kind.
package claudecode

import (
	"encoding/json"
	"errors"
	iofs "io/fs"
	"path/filepath"

	"github.com/MHilhorst/ainfra/internal/lockfile"
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
// A missing file is treated as no resources. ContentHash is computed from the
// on-disk entry using MCPServerCanonicalMap so a hand-edit to any field of a
// server (version pin in args, env value, command path) surfaces as drift on
// the next plan or check.
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
	for key, entry := range servers {
		obj, _ := entry.(map[string]any)
		resources = append(resources, provider.Resource{
			ID:          key,
			Channel:     "mcpServers",
			ContentHash: lockfile.ContentHash(MCPServerCanonicalMap(obj)),
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
// Nil or missing optional fields are omitted. This is what gets written to
// .mcp.json. MCPServerCanonicalMap consumes the same shape from disk.
func buildMCPServerObject(payload map[string]any) map[string]any {
	obj := map[string]any{}

	if cmd, ok := payload["command"]; ok && cmd != nil && cmd != "" {
		obj["command"] = cmd
	}
	if args, ok := payload["args"]; ok && !isEmpty(args) {
		obj["args"] = args
	}
	if env, ok := payload["env"]; ok && !isEmpty(env) {
		obj["env"] = env
	}
	if transport, ok := payload["transport"]; ok && transport != nil && transport != "" {
		obj["type"] = transport
	}
	if url, ok := payload["url"]; ok && url != nil && url != "" {
		obj["url"] = url
	}
	if headers, ok := payload["headers"]; ok && !isEmpty(headers) {
		obj["headers"] = headers
	}

	return obj
}

// MCPServerCanonicalMap returns the canonical on-disk representation of one
// MCP server entry. It accepts either the on-disk shape (keys "command", "args",
// "env", "type", "url", "headers") or the resolver's intermediate shape (where
// "transport" is used in place of "type") and normalizes to the on-disk shape.
// Empty/nil values are dropped so json.Marshal output is stable.
//
// Both the resolver (when computing the lockfile's desired ContentHash) and the
// Observe path (when hashing what is actually on disk) must agree on this
// shape; that is the entire point of putting it here.
func MCPServerCanonicalMap(entry map[string]any) map[string]any {
	if entry == nil {
		return map[string]any{}
	}
	obj := map[string]any{}
	if v, ok := entry["command"].(string); ok && v != "" {
		obj["command"] = v
	}
	if v, ok := entry["args"]; ok && !isEmpty(v) {
		obj["args"] = v
	}
	if v, ok := entry["env"]; ok && !isEmpty(v) {
		obj["env"] = v
	}
	// Resolver-side passes "transport"; the on-disk file uses "type".
	if v, ok := entry["type"].(string); ok && v != "" {
		obj["type"] = v
	} else if v, ok := entry["transport"].(string); ok && v != "" {
		obj["type"] = v
	}
	if v, ok := entry["url"].(string); ok && v != "" {
		obj["url"] = v
	}
	if v, ok := entry["headers"]; ok && !isEmpty(v) {
		obj["headers"] = v
	}
	return obj
}

func isEmpty(v any) bool {
	if v == nil {
		return true
	}
	switch x := v.(type) {
	case string:
		return x == ""
	case []any:
		return len(x) == 0
	case []string:
		return len(x) == 0
	case map[string]any:
		return len(x) == 0
	case map[string]string:
		return len(x) == 0
	}
	return false
}
