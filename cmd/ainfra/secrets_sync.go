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

// secretSchemesUsed returns the distinct ref schemes (e.g. "op", "env") every
// secret in the resolved locks and the manifest's envFile/path secrets resolves
// through. It is the input to the backend preflight: which credential backends
// must be ready for this install to materialize secrets.
func secretSchemesUsed(dir string, committed, personal *lockfile.Lock) []string {
	set := map[string]bool{}
	for _, l := range []*lockfile.Lock{committed, personal} {
		if l == nil {
			continue
		}
		for _, sr := range l.Secrets {
			if sr.Scheme != "" {
				set[sr.Scheme] = true
			}
		}
	}
	// envFile/path secrets live in the manifest, not the lockfile.
	if layers, err := manifest.LoadLayers(dir); err == nil {
		for _, m := range layers {
			if m == nil {
				continue
			}
			for _, sec := range m.Secrets {
				if !sec.EnvFile && sec.Path == "" {
					continue
				}
				if scheme, err := secret.SchemeOf(expandUser(sec.Ref)); err == nil {
					set[scheme] = true
				}
			}
		}
	}
	return slices.Sorted(maps.Keys(set))
}

// preflightSecretBackends verifies every credential backend a secret resolves
// through is ready (e.g. the 1Password CLI is installed and signed in) before
// apply writes any config, so an unusable backend fails fast instead of leaving
// a half-configured repo. Backends with no readiness probe are skipped.
func preflightSecretBackends(dir string, reg *secret.Registry, committed, personal *lockfile.Lock) []string {
	var failures []string
	for _, scheme := range secretSchemesUsed(dir, committed, personal) {
		if err := reg.CheckBackend(scheme); err != nil {
			failures = append(failures, err.Error())
		}
	}
	return failures
}

// syncResult reports what syncSecrets materialized.
type syncResult struct {
	EnvCount     int      // environment variables written to the settings file
	SettingsPath string   // the settings file written
	ShellEnvPath string   // shell export file for process-env delivery, "" when no secrets
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

	// The settings env block only reaches stdio MCP servers. Claude Code
	// expands ${VAR} in HTTP server headers from the real process environment,
	// so the same secrets are also written as shell exports and sourced from
	// ~/.zshenv. Skipped entirely when the manifest declares no secrets — a
	// secretless install must not touch the user's shell config.
	shellEnvPath := ""
	if len(resolved) > 0 {
		shellEnvPath = filepath.Join(home, ".config", "ainfra", "env.sh")
		if err := writeShellEnv(shellEnvPath, resolved); err != nil {
			return syncResult{}, err
		}
		if err := ensureShellSourceLine(filepath.Join(home, ".zshenv"), shellEnvPath); err != nil {
			return syncResult{}, err
		}
	}

	return syncResult{
		EnvCount:     len(resolved),
		SettingsPath: settingsPath,
		ShellEnvPath: shellEnvPath,
		Files:        slices.Sorted(maps.Keys(fileSet)),
	}, nil
}

// writeShellEnv writes the resolved secrets as `export KEY='value'` lines so
// a shell that sources the file puts every secret into the process
// environment. Values are single-quoted with embedded quotes escaped, so
// multi-line or special-character values survive. The file is written 0600 —
// it holds credential values.
func writeShellEnv(path string, env map[string]string) error {
	var b strings.Builder
	b.WriteString("# Generated by ainfra. Do not edit by hand.\n")
	b.WriteString("# Sourced from ~/.zshenv so ${VAR} references in MCP server headers resolve.\n")
	for _, k := range slices.Sorted(maps.Keys(env)) {
		fmt.Fprintf(&b, "export %s='%s'\n", k, strings.ReplaceAll(env[k], "'", `'\''`))
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		return err
	}
	// WriteFile's mode only applies on creation; tighten a pre-existing file.
	return os.Chmod(path, 0o600)
}

// ensureShellSourceLine makes ~/.zshenv source the ainfra env file, creating
// the rc file when missing. Idempotent: a file that already references the
// env file (in any form) is left untouched, so a user can move or rewrite the
// line without ainfra re-appending it.
func ensureShellSourceLine(rcPath, envPath string) error {
	marker := envPath
	ref := fmt.Sprintf("%q", envPath)
	if home, err := os.UserHomeDir(); err == nil {
		if rel, rerr := filepath.Rel(home, envPath); rerr == nil && !strings.HasPrefix(rel, "..") {
			marker = rel
			ref = fmt.Sprintf(`"$HOME/%s"`, rel)
		}
	}
	existing, err := os.ReadFile(rcPath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if strings.Contains(string(existing), marker) {
		return nil
	}
	line := fmt.Sprintf("\n# Added by ainfra — exports managed secrets into the shell environment.\n[ -f %s ] && . %s\n", ref, ref)
	if len(existing) > 0 && !strings.HasSuffix(string(existing), "\n") {
		line = "\n" + line
	}
	f, err := os.OpenFile(rcPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(line)
	return err
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
