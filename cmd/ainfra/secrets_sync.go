package main

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/MHilhorst/ainfra/internal/lockfile"
	"github.com/MHilhorst/ainfra/internal/manifest"
	"github.com/MHilhorst/ainfra/internal/secret"
)

// syncResult reports what syncSecrets materialized.
type syncResult struct {
	EnvCount     int      // environment variables written to the settings file
	SettingsPath string   // the settings file written
	Files        []string // credential files written, by path
}

// syncSecrets resolves every secret referenced by the resolved locks and the
// manifest at dir and materializes it. It runs as the final step of
// `ainfra install`, which passes the in-memory resolved locks so a secret
// added to ainfra.yaml syncs even while the committed lockfile is stale.
//
// A secret is materialized by its destination:
//   - a single-value secret (lock)     -> one env var in the settings file
//   - envFile: true                    -> a .env blob expanded into many env vars
//   - path: <file>                     -> the resolved value written to that file
func syncSecrets(dir string, reg *secret.Registry, committed, personal *lockfile.Lock) (syncResult, error) {
	// The single-value secret set is the union of both locks.
	refs := map[string]lockfile.SecretRef{}
	maps.Copy(refs, committed.Secrets)
	maps.Copy(refs, personal.Secrets)

	resolved := map[string]string{}
	var failures []string
	for _, v := range slices.Sorted(maps.Keys(refs)) {
		sr := refs[v]
		val, err := reg.Resolve(expandUser(sr.Ref))
		if err != nil {
			failures = append(failures, "  "+err.Error())
			continue
		}
		resolved[sr.Var] = val
	}

	// envFile and path secrets are declared in the manifest, not the lockfile:
	// envFile expands one ref into many env vars; path writes one ref to a file.
	fileSet := map[string]bool{}
	if layers, lerr := manifest.LoadLayers(dir); lerr == nil {
		for _, ln := range []manifest.Layer{manifest.LayerTeam, manifest.LayerRepo, manifest.LayerPersonal} {
			m := layers[ln]
			if m == nil {
				continue
			}
			for _, id := range slices.Sorted(maps.Keys(m.Secrets)) {
				sec := m.Secrets[id]
				if !sec.EnvFile && sec.Path == "" {
					continue
				}
				blob, rerr := reg.Resolve(expandUser(sec.Ref))
				if rerr != nil {
					failures = append(failures, fmt.Sprintf("  secret %q: %v", id, rerr))
					continue
				}
				switch {
				case sec.EnvFile:
					maps.Copy(resolved, parseEnvBlob(blob))
				case sec.Path != "":
					dest := expandTilde(sec.Path)
					if werr := writeCredentialFile(dest, blob); werr != nil {
						failures = append(failures, fmt.Sprintf("  secret %q: %v", id, werr))
						continue
					}
					fileSet[dest] = true
				}
			}
		}
	}

	if len(failures) > 0 {
		return syncResult{}, fmt.Errorf("could not resolve secrets:\n%s", strings.Join(failures, "\n"))
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return syncResult{}, err
	}
	settingsPath := filepath.Join(home, ".claude", "settings.local.json")
	if err := writeSettingsEnv(settingsPath, resolved); err != nil {
		return syncResult{}, err
	}
	return syncResult{
		EnvCount:     len(resolved),
		SettingsPath: settingsPath,
		Files:        slices.Sorted(maps.Keys(fileSet)),
	}, nil
}

// expandTilde resolves a leading ~/ against the user's home directory.
func expandTilde(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(path, "~"), "/"))
		}
	}
	return path
}

// writeCredentialFile writes a resolved secret value to a file: the parent
// directory is created 0700 and the file 0600 — both hold a credential.
func writeCredentialFile(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o600)
}

// parseEnvBlob parses a .env-style blob (KEY=value lines) into a map. Blank
// lines and # comments are skipped, a leading `export ` is ignored, and a
// double-quoted value has its quotes removed and \n \t \" \\ escapes decoded —
// so a multi-line PEM key or JSON document can be stored on a single line.
func parseEnvBlob(s string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(s, "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		t = strings.TrimPrefix(t, "export ")
		eq := strings.IndexByte(t, '=')
		if eq <= 0 {
			continue
		}
		key := strings.TrimSpace(t[:eq])
		val := strings.TrimSpace(t[eq+1:])
		switch {
		case len(val) >= 2 && val[0] == '"' && val[len(val)-1] == '"':
			val = val[1 : len(val)-1]
			val = strings.NewReplacer(`\n`, "\n", `\t`, "\t", `\"`, `"`, `\\`, `\`).Replace(val)
		case len(val) >= 2 && val[0] == '\'' && val[len(val)-1] == '\'':
			val = val[1 : len(val)-1]
		}
		out[key] = val
	}
	return out
}

// writeSettingsEnv merges the resolved secrets into the "env" object of the
// Claude Code settings file, preserving every other key in the file and every
// env entry ainfra does not manage. The file is written 0600 — it holds
// credential values.
func writeSettingsEnv(path string, env map[string]string) error {
	doc := map[string]any{}
	if raw, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(raw, &doc); err != nil {
			return fmt.Errorf("parsing %s: %w", path, err)
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	envObj, _ := doc["env"].(map[string]any)
	if envObj == nil {
		envObj = map[string]any{}
	}
	for k, v := range env {
		envObj[k] = v
	}
	doc["env"] = envObj

	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, append(out, '\n'), 0o600); err != nil {
		return err
	}
	// WriteFile's mode only applies when creating the file. Claude Code may have
	// created settings.local.json at 0644 first, so chmod explicitly — it holds
	// credential values and must not be world-readable.
	return os.Chmod(path, 0o600)
}
