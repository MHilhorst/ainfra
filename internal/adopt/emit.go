package adopt

import (
	"bytes"
	"fmt"
	"sort"

	"github.com/MHilhorst/ainfra/internal/manifest"
	"gopkg.in/yaml.v3"
)

// Emit serializes m to ainfra.yaml's canonical key order and returns the
// resulting bytes (always terminated with a trailing newline).
//
// Each entry is rendered as a map of its non-zero fields only — the manifest
// struct types intentionally lack `omitempty` tags for compatibility with
// other code paths, so adopt does the omission itself. This keeps the output
// readable and gives a stable round-trip: a manifest emitted by Emit, parsed
// via manifest.Load, and re-emitted by Emit is byte-identical.
func Emit(m manifest.Manifest) ([]byte, error) {
	root := &yaml.Node{Kind: yaml.MappingNode}

	addScalar := func(key string, value any) error {
		v := &yaml.Node{}
		if err := v.Encode(value); err != nil {
			return fmt.Errorf("adopt: encode %s: %w", key, err)
		}
		root.Content = append(root.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: key, Tag: "!!str"},
			v,
		)
		return nil
	}
	addMap := func(key string, value any) error {
		v := &yaml.Node{}
		if err := v.Encode(value); err != nil {
			return fmt.Errorf("adopt: encode %s: %w", key, err)
		}
		root.Content = append(root.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: key, Tag: "!!str"},
			v,
		)
		return nil
	}

	if err := addScalar("version", m.Version); err != nil {
		return nil, err
	}
	if m.AinfraVersion != "" {
		if err := addScalar("ainfraVersion", m.AinfraVersion); err != nil {
			return nil, err
		}
	}
	if m.Agent != "" {
		if err := addScalar("agent", m.Agent); err != nil {
			return nil, err
		}
	}
	if len(m.CLITools) > 0 {
		if err := addMap("cliTools", m.CLITools); err != nil {
			return nil, err
		}
	}
	if len(m.Secrets) > 0 {
		if err := addMap("secrets", emitSecrets(m.Secrets)); err != nil {
			return nil, err
		}
	}
	if len(m.MCPServers) > 0 {
		if err := addMap("mcpServers", emitMCPServers(m.MCPServers)); err != nil {
			return nil, err
		}
	}
	if len(m.Hooks) > 0 {
		if err := addMap("hooks", emitHooks(m.Hooks)); err != nil {
			return nil, err
		}
	}
	if len(m.Skills) > 0 {
		if err := addMap("skills", m.Skills); err != nil {
			return nil, err
		}
	}
	if len(m.Commands) > 0 {
		if err := addMap("commands", emitCommands(m.Commands)); err != nil {
			return nil, err
		}
	}
	if len(m.Rules) > 0 {
		if err := addMap("rules", emitRules(m.Rules)); err != nil {
			return nil, err
		}
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(root); err != nil {
		return nil, fmt.Errorf("adopt: encode manifest: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("adopt: close encoder: %w", err)
	}
	out := buf.Bytes()
	if len(out) == 0 || out[len(out)-1] != '\n' {
		out = append(out, '\n')
	}
	return out, nil
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func emitSecrets(in map[string]manifest.Secret) map[string]map[string]any {
	out := map[string]map[string]any{}
	for _, k := range sortedKeys(in) {
		s := in[k]
		entry := map[string]any{}
		if s.Mode != "" {
			entry["mode"] = s.Mode
		}
		if s.Value != "" {
			entry["value"] = s.Value
		}
		if s.Ref != "" {
			entry["ref"] = s.Ref
		}
		if s.Gateway != "" {
			entry["gateway"] = s.Gateway
		}
		if s.Scope != "" {
			entry["scope"] = s.Scope
		}
		if s.Env != "" {
			entry["env"] = s.Env
		}
		if s.EnvFile {
			entry["envFile"] = true
		}
		if s.Path != "" {
			entry["path"] = s.Path
		}
		out[k] = entry
	}
	return out
}

func emitMCPServers(in map[string]manifest.MCPServer) map[string]map[string]any {
	out := map[string]map[string]any{}
	for _, k := range sortedKeys(in) {
		s := in[k]
		entry := map[string]any{}
		if s.Template != "" {
			entry["template"] = s.Template
		}
		if len(s.Params) > 0 {
			entry["params"] = s.Params
		}
		if len(s.Secret) > 0 {
			entry["secret"] = s.Secret
		}
		if s.Transport != "" {
			entry["transport"] = s.Transport
		}
		if s.URL != "" {
			entry["url"] = s.URL
		}
		if s.Command != "" {
			entry["command"] = s.Command
		}
		if len(s.Args) > 0 {
			entry["args"] = s.Args
		}
		if s.Version != "" {
			entry["version"] = s.Version
		}
		if len(s.Env) > 0 {
			entry["env"] = s.Env
		}
		if len(s.Headers) > 0 {
			entry["headers"] = s.Headers
		}
		out[k] = entry
	}
	return out
}

func emitHooks(in map[string]manifest.Hook) map[string]map[string]any {
	out := map[string]map[string]any{}
	for _, k := range sortedKeys(in) {
		h := in[k]
		entry := map[string]any{}
		if h.Event != "" {
			entry["event"] = h.Event
		}
		if h.Matcher != "" {
			entry["matcher"] = h.Matcher
		}
		if h.Command != "" {
			entry["command"] = h.Command
		}
		if h.Source != "" {
			entry["source"] = h.Source
		}
		if h.Timeout != 0 {
			entry["timeout"] = h.Timeout
		}
		out[k] = entry
	}
	return out
}

func emitCommands(in map[string]manifest.Command) map[string]map[string]any {
	out := map[string]map[string]any{}
	for _, k := range sortedKeys(in) {
		c := in[k]
		entry := map[string]any{}
		if c.Source != "" {
			entry["source"] = c.Source
		}
		if c.Description != "" {
			entry["description"] = c.Description
		}
		if c.Version != "" {
			entry["version"] = c.Version
		}
		out[k] = entry
	}
	return out
}

func emitRules(in map[string]manifest.Rule) map[string]map[string]any {
	out := map[string]map[string]any{}
	for _, k := range sortedKeys(in) {
		r := in[k]
		entry := map[string]any{}
		if r.Target != "" {
			entry["target"] = r.Target
		}
		if r.Source != "" {
			entry["source"] = r.Source
		}
		if r.Template {
			entry["template"] = true
		}
		if r.Version != "" {
			entry["version"] = r.Version
		}
		out[k] = entry
	}
	return out
}
