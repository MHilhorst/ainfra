package resolve

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/MHilhorst/ainfra/internal/manifest"
	"github.com/MHilhorst/ainfra/internal/provider"
)

// varPlaceholder matches {{KEY}} where KEY is a valid identifier.
var varPlaceholder = regexp.MustCompile(`\{\{([A-Za-z_][A-Za-z0-9_]*)\}\}`)

// collectVars merges vars across layers; first-seen-wins (team > repo > personal).
func collectVars(layers map[manifest.Layer]*manifest.Manifest) map[string]manifest.Var {
	all := map[string]manifest.Var{}
	for _, layerName := range []manifest.Layer{manifest.LayerTeam, manifest.LayerRepo, manifest.LayerPersonal} {
		m, ok := layers[layerName]
		if !ok {
			continue
		}
		for name, v := range m.Vars {
			if _, exists := all[name]; !exists {
				all[name] = v
			}
		}
	}
	return all
}

// resolveVars evaluates each Var spec into a concrete string value.
// For from: value, returns the literal Value.
// For from: env, reads the named environment variable.
// For from: command, runs the command via runner and returns trimmed stdout.
func resolveVars(specs map[string]manifest.Var, runner provider.CommandRunner) (map[string]string, error) {
	out := make(map[string]string, len(specs))
	for name, v := range specs {
		switch v.From {
		case "value":
			out[name] = v.Value
		case "env":
			out[name] = os.Getenv(v.Env)
		case "command":
			raw, err := runner.Run("sh", "-c", v.Command)
			if err != nil {
				return nil, fmt.Errorf("var %q: command %q failed: %w", name, v.Command, err)
			}
			out[name] = strings.TrimRight(string(raw), " \t\n\r")
		default:
			return nil, fmt.Errorf("var %q: unknown from: %q", name, v.From)
		}
	}
	return out, nil
}

// substituteVars replaces {{KEY}} placeholders in content with values from vars.
// An undefined KEY returns an error naming the variable.
// An unclosed {{ that does not match the pattern is left unchanged.
func substituteVars(content string, vars map[string]string) (string, error) {
	var subErr error
	result := varPlaceholder.ReplaceAllStringFunc(content, func(match string) string {
		if subErr != nil {
			return match
		}
		// Extract key from match {{KEY}}
		sub := varPlaceholder.FindStringSubmatch(match)
		if len(sub) < 2 {
			return match
		}
		key := sub[1]
		val, ok := vars[key]
		if !ok {
			subErr = fmt.Errorf("references undefined variable %q", key)
			return match
		}
		return val
	})
	if subErr != nil {
		return "", subErr
	}
	return result, nil
}
