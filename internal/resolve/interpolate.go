// Package resolve turns a layered manifest into fully resolved state:
// templates instantiated, ${...} expanded, layers merged, ports allocated.
package resolve

import (
	"fmt"
	"regexp"
)

// Scope holds the four interpolation namespaces (spec §4.4).
type Scope struct {
	Params   map[string]any
	Instance map[string]any
	Resolved map[string]any
	Secret   map[string]any
}

var refPattern = regexp.MustCompile(`\$\{([a-zA-Z]+)\.([a-zA-Z0-9_]+)\}`)

// Interpolate expands every ${namespace.key} in s against scope.
func Interpolate(s string, scope Scope) (string, error) {
	var bad error
	out := refPattern.ReplaceAllStringFunc(s, func(m string) string {
		g := refPattern.FindStringSubmatch(m)
		ns, key := g[1], g[2]
		table, ok := map[string]map[string]any{
			"params": scope.Params, "instance": scope.Instance,
			"resolved": scope.Resolved, "secret": scope.Secret,
		}[ns]
		if !ok {
			if bad == nil {
				bad = fmt.Errorf("unknown namespace %q in %q", ns, m)
			}
			return m
		}
		v, ok := table[key]
		if !ok {
			if bad == nil {
				bad = fmt.Errorf("unknown reference %q", m)
			}
			return m
		}
		return fmt.Sprintf("%v", v)
	})
	if bad != nil {
		return "", bad
	}
	return out, nil
}

// InterpolateMap applies Interpolate to every string value in m, recursively.
func InterpolateMap(m map[string]any, scope Scope) (map[string]any, error) {
	out := make(map[string]any, len(m))
	for k, v := range m {
		nv, err := interpolateValue(v, scope)
		if err != nil {
			return nil, err
		}
		out[k] = nv
	}
	return out, nil
}

func interpolateValue(v any, scope Scope) (any, error) {
	switch t := v.(type) {
	case string:
		return Interpolate(t, scope)
	case map[string]any:
		return InterpolateMap(t, scope)
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			nv, err := interpolateValue(e, scope)
			if err != nil {
				return nil, err
			}
			out[i] = nv
		}
		return out, nil
	default:
		return v, nil
	}
}
