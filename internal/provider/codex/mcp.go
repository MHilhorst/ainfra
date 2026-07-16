// Package codex contains the Codex channel providers for the ainfra
// reconciler. Each type implements provider.Provider for one channel kind,
// rendering into the config files the Codex CLI reads.
package codex

import (
	"errors"
	"fmt"
	iofs "io/fs"
	"path/filepath"
	"strings"

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
			desired[c.ID] = buildCodexServerTable(env.Root, c.Resource.Payload, c.Resource.Requires)
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
// payload. Codex supports command-launched stdio servers and streamable HTTP
// servers. Nil or missing optional fields are omitted.
func buildCodexServerTable(root string, payload map[string]any, requires []string) map[string]any {
	table := map[string]any{}
	if url, ok := payload["url"]; ok && url != nil && url != "" {
		table["url"] = url
		if tokenEnv := bearerTokenEnvVar(payload["headers"]); tokenEnv != "" {
			table["bearer_token_env_var"] = tokenEnv
		}
		httpHeaders, envHTTPHeaders := codexHTTPHeaders(payload["headers"])
		if len(httpHeaders) > 0 {
			table["http_headers"] = httpHeaders
		}
		if len(envHTTPHeaders) > 0 {
			table["env_http_headers"] = envHTTPHeaders
		}
		return table
	}
	if cmd, ok := payload["command"]; ok && cmd != nil && cmd != "" {
		table["command"] = cmd
	}
	if args, ok := payload["args"]; ok && args != nil {
		table["args"] = args
	}
	if serviceID := firstRequiredService(requires); serviceID != "" {
		if wrappedCommand, wrappedArgs := wrapWithServiceStart(root, serviceID, table["command"], table["args"]); wrappedCommand != "" {
			table["command"] = wrappedCommand
			table["args"] = wrappedArgs
		}
	}
	if env, ok := payload["env"]; ok && env != nil {
		table["env"] = env
	}
	return table
}

func bearerTokenEnvVar(headers any) string {
	headerMap, ok := headers.(map[string]string)
	if !ok {
		return ""
	}
	auth := strings.TrimSpace(headerMap["Authorization"])
	if !strings.HasPrefix(auth, "Bearer ${") || !strings.HasSuffix(auth, "}") {
		return ""
	}
	return strings.TrimSuffix(strings.TrimPrefix(auth, "Bearer ${"), "}")
}

func codexHTTPHeaders(headers any) (map[string]string, map[string]string) {
	headerMap, ok := headers.(map[string]string)
	if !ok {
		return nil, nil
	}
	httpHeaders := map[string]string{}
	envHTTPHeaders := map[string]string{}
	for key, value := range headerMap {
		trimmed := strings.TrimSpace(value)
		if key == "Authorization" && bearerTokenEnvVar(headers) != "" && strings.HasPrefix(trimmed, "Bearer ${") {
			continue
		}
		if envVar := envPlaceholder(trimmed); envVar != "" {
			envHTTPHeaders[key] = envVar
			continue
		}
		httpHeaders[key] = value
	}
	return httpHeaders, envHTTPHeaders
}

func envPlaceholder(value string) string {
	if strings.HasPrefix(value, "${") && strings.HasSuffix(value, "}") {
		return strings.TrimSuffix(strings.TrimPrefix(value, "${"), "}")
	}
	return ""
}

func firstRequiredService(requires []string) string {
	for _, req := range requires {
		if strings.HasPrefix(req, "svc:") {
			return strings.TrimPrefix(req, "svc:")
		}
	}
	return ""
}

func wrapWithServiceStart(root, serviceID string, command any, args any) (string, []any) {
	cmd, ok := command.(string)
	if !ok || cmd == "" {
		return "", nil
	}
	startPath := filepath.Join(root, ".ainfra", "services", serviceID, "start.sh")
	line := fmt.Sprintf("sh %s && exec %s", shellQuote(startPath), shellCommand(cmd, args))
	return "sh", []any{"-c", line}
}

func shellCommand(command string, args any) string {
	parts := []string{shellQuote(command)}
	switch values := args.(type) {
	case []string:
		for _, arg := range values {
			parts = append(parts, shellQuote(arg))
		}
	case []any:
		for _, arg := range values {
			parts = append(parts, shellQuote(fmt.Sprint(arg)))
		}
	}
	return strings.Join(parts, " ")
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
