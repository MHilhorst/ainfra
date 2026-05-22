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
// placeholder var, the lockfile SecretRefs for every ref-mode secret, plus a
// map from each secret name to the string that should replace ${secret.<name>}
// during interpolation (a literal value, or an ${AINFRA_SECRET_*} placeholder).
func collectSecretRefs(channel, owner string, layer manifest.Layer, raw map[string]any, topLevel map[string]manifest.Secret) (map[string]lockfile.SecretRef, map[string]string, error) {
	refs := map[string]lockfile.SecretRef{}
	vals := map[string]string{}
	for _, name := range slices.Sorted(maps.Keys(raw)) {
		sec, err := normalizeSecret(raw[name], topLevel)
		if err != nil {
			return nil, nil, fmt.Errorf("%s %q: secret %q: %w", channel, owner, name, err)
		}
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
				return nil, nil, fmt.Errorf("%s %q: secret %q: %w", channel, owner, name, err)
			}
			scope := sec.Scope
			if scope == "" {
				scope = "shared"
			}
			v := secret.PlaceholderVar(channel, owner, name)
			refs[v] = lockfile.SecretRef{Var: v, Ref: sec.Ref, Scheme: scheme, Scope: scope, Layer: string(layer)}
			vals[name] = "${" + v + "}"
		default: // brokered: no per-dev value exists in this increment
			vals[name] = ""
		}
	}
	return refs, vals, nil
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
	refs, vals, err := collectSecretRefs(channel, owner, layer, raw, topLevel)
	if err != nil {
		return nil, err
	}
	replace := func(s string) string {
		for name, val := range vals {
			s = strings.ReplaceAll(s, "${secret."+name+"}", val)
			s = strings.ReplaceAll(s, fmt.Sprintf("<secret:%s.%s>", owner, name), val)
		}
		return s
	}
	for k, v := range srv.Env {
		srv.Env[k] = replace(v)
	}
	for k, v := range srv.Headers {
		srv.Headers[k] = replace(v)
	}
	srv.URL = replace(srv.URL)
	return refs, nil
}
