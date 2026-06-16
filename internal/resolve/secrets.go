package resolve

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/MHilhorst/ainfra/internal/lockfile"
	"github.com/MHilhorst/ainfra/internal/manifest"
	"github.com/MHilhorst/ainfra/internal/secret"
	"gopkg.in/yaml.v3"
)

// normalizeSecret turns one secret: map value into a manifest.Secret. A string
// value is a reference to a top-level secrets: entry by id; a map value is an
// inline secret definition.
func normalizeSecret(v any, topLevel map[string]manifest.Secret) (manifest.Secret, error) {
	switch t := v.(type) {
	case string:
		s, ok := topLevel[t]
		if !ok {
			return manifest.Secret{}, fmt.Errorf("references unknown top-level secret %q", t)
		}
		return s, nil
	case map[string]any:
		raw, err := yaml.Marshal(t)
		if err != nil {
			return manifest.Secret{}, err
		}
		var s manifest.Secret
		if err := yaml.Unmarshal(raw, &s); err != nil {
			return manifest.Secret{}, err
		}
		return s, nil
	default:
		return manifest.Secret{}, fmt.Errorf("must be a string id or an inline map, got %T", v)
	}
}

// collectSecretRefs normalizes an entry's secret: map and returns, keyed by
// placeholder var, the lockfile SecretRefs for every ref-mode secret; a map
// from each secret name to the string that should replace ${secret.<name>}
// during interpolation (a literal value, or an ${AINFRA_SECRET_*} placeholder);
// and the normalized secret per name, so callers can tell how each binding is
// meant to reach the tool.
func collectSecretRefs(channel, owner string, layer manifest.Layer, raw map[string]any, topLevel map[string]manifest.Secret) (map[string]lockfile.SecretRef, map[string]string, map[string]manifest.Secret, error) {
	refs := map[string]lockfile.SecretRef{}
	vals := map[string]string{}
	norms := map[string]manifest.Secret{}
	for _, name := range slices.Sorted(maps.Keys(raw)) {
		sec, err := normalizeSecret(raw[name], topLevel)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("%s %q: secret %q: %w", channel, owner, name, err)
		}
		norms[name] = sec
		mode := sec.Mode
		if mode == "" {
			mode = "direct"
		}
		switch {
		case mode == "direct" && sec.Ref == "":
			vals[name] = sec.Value
		case mode == "direct" && sec.Ref != "":
			scheme, err := secret.SchemeOf(sec.Ref)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("%s %q: secret %q: %w", channel, owner, name, err)
			}
			scope := sec.Scope
			if scope == "" {
				scope = "shared"
			}
			// A secret may declare the env-var name it is exported under; this
			// lets `ainfra exec` match an MCP config that already expects a
			// specific name. Otherwise a generated AINFRA_SECRET_* name is used.
			v := sec.Env
			if v == "" {
				v = secret.PlaceholderVar(channel, owner, name)
			}
			refs[v] = lockfile.SecretRef{Var: v, Ref: sec.Ref, Scheme: scheme, Scope: scope, Layer: string(layer)}
			vals[name] = "${" + v + "}"
		default: // brokered: no per-dev value exists in this increment
			vals[name] = ""
		}
	}
	return refs, vals, norms, nil
}

// boundButUnused reports a secret binding that can never reach the tool: bound
// in an entry's secret: block but neither referenced (${secret.<name>}) nor
// given an explicit delivery target. Such a binding is a silent no-op, so it is
// rejected at resolve time rather than launching the server without credentials.
func boundButUnused(channel, owner, name string, sec manifest.Secret, used map[string]bool) error {
	if used[name] {
		return nil
	}
	// An explicit delivery target reaches the tool without an in-place
	// reference: Env exports it under a name the tool reads; Path / EnvFile
	// materialize it elsewhere.
	if sec.Env != "" || sec.Path != "" || sec.EnvFile {
		return nil
	}
	// Brokered secrets carry no per-dev value to wire; a binding is meaningful
	// for gateway routing even with no reference.
	if mode := sec.Mode; mode != "" && mode != "direct" {
		return nil
	}
	return fmt.Errorf("%s %q: secret %q is bound but never used — reference it as ${secret.%s} in env, args, headers, or url, "+
		"or give the secret an `env:` name so it is exported to the environment", channel, owner, name, name)
}

// substituteSecrets replaces secret tokens in srv's env, headers, and url with
// their final value: a literal, or an ${AINFRA_SECRET_*} placeholder. It
// recognises both the raw ${secret.<name>} form used by inline servers and the
// <secret:<owner>.<name>> interim form emitted by Instantiate for templated
// servers. It returns the lockfile SecretRefs for every ref-mode secret.
func substituteSecrets(srv *manifest.MCPServer, channel, owner string, layer manifest.Layer, raw map[string]any, topLevel map[string]manifest.Secret) (map[string]lockfile.SecretRef, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	refs, vals, norms, err := collectSecretRefs(channel, owner, layer, raw, topLevel)
	if err != nil {
		return nil, err
	}
	used := map[string]bool{}
	replace := func(s string) string {
		for name, val := range vals {
			tok := "${secret." + name + "}"
			interim := fmt.Sprintf("<secret:%s.%s>", owner, name)
			if strings.Contains(s, tok) || strings.Contains(s, interim) {
				used[name] = true
			}
			s = strings.ReplaceAll(s, tok, val)
			s = strings.ReplaceAll(s, interim, val)
		}
		return s
	}
	for k, v := range srv.Env {
		srv.Env[k] = replace(v)
	}
	for k, v := range srv.Headers {
		srv.Headers[k] = replace(v)
	}
	for i, a := range srv.Args {
		srv.Args[i] = replace(a)
	}
	srv.Command = replace(srv.Command)
	srv.URL = replace(srv.URL)

	for _, name := range slices.Sorted(maps.Keys(norms)) {
		if err := boundButUnused(channel, owner, name, norms[name], used); err != nil {
			return nil, err
		}
	}
	return refs, nil
}
