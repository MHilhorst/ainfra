package main

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"os/exec"
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
	ShimPath     string   // launcher shim that injects secrets at startup, "" when no secrets
	Files        []string // credential files written, by path
}

// resolveSecretEnv resolves every environment-variable secret referenced by
// the locks (single-value secrets) and the manifest at dir (envFile blobs).
// It is shared by `ainfra install`, which persists the result, and
// `ainfra exec`, which injects it into a child process at launch.
// Credential-file (path:) secrets are install-time artifacts and are not
// resolved here. Resolution failures are returned as messages, not an error —
// the callers decide whether a failure is fatal (install) or a warning (exec).
func resolveSecretEnv(dir string, reg *secret.Registry, committed, personal *lockfile.Lock) (map[string]string, []string) {
	// The single-value secret set is the union of both locks.
	refs := map[string]lockfile.SecretRef{}
	for _, l := range []*lockfile.Lock{committed, personal} {
		if l != nil {
			maps.Copy(refs, l.Secrets)
		}
	}

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

	// envFile secrets expand one ref into many env vars; they are declared in
	// the manifest, not the lockfile.
	if layers, lerr := manifest.LoadLayers(dir); lerr == nil {
		for _, ln := range []manifest.Layer{manifest.LayerTeam, manifest.LayerRepo, manifest.LayerPersonal} {
			m := layers[ln]
			if m == nil {
				continue
			}
			for _, id := range slices.Sorted(maps.Keys(m.Secrets)) {
				sec := m.Secrets[id]
				if !sec.EnvFile {
					continue
				}
				blob, rerr := reg.Resolve(expandUser(sec.Ref))
				if rerr != nil {
					failures = append(failures, fmt.Sprintf("  secret %q: %v", id, rerr))
					continue
				}
				maps.Copy(resolved, parseEnvBlob(blob))
			}
		}
	}
	return resolved, failures
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
//
// Process-environment delivery (what ${VAR} expansion in HTTP MCP server
// headers needs) is not persisted at all: the launcher shim written here
// re-resolves secrets through `ainfra exec` at every launch.
func syncSecrets(dir string, reg *secret.Registry, committed, personal *lockfile.Lock) (syncResult, error) {
	resolved, failures := resolveSecretEnv(dir, reg, committed, personal)

	// path secrets write the resolved value to a credential file.
	fileSet := map[string]bool{}
	if layers, lerr := manifest.LoadLayers(dir); lerr == nil {
		for _, ln := range []manifest.Layer{manifest.LayerTeam, manifest.LayerRepo, manifest.LayerPersonal} {
			m := layers[ln]
			if m == nil {
				continue
			}
			for _, id := range slices.Sorted(maps.Keys(m.Secrets)) {
				sec := m.Secrets[id]
				if sec.Path == "" || sec.EnvFile {
					continue
				}
				blob, rerr := reg.Resolve(expandUser(sec.Ref))
				if rerr != nil {
					failures = append(failures, fmt.Sprintf("  secret %q: %v", id, rerr))
					continue
				}
				dest := expandTilde(sec.Path)
				if werr := writeCredentialFile(dest, blob); werr != nil {
					failures = append(failures, fmt.Sprintf("  secret %q: %v", id, werr))
					continue
				}
				fileSet[dest] = true
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
	// so a launcher shim re-execs claude through `ainfra exec`, which resolves
	// secrets fresh into the process env at every launch. Skipped entirely
	// when the manifest declares no secrets — a secretless install must not
	// touch the user's shell config.
	shimPath := ""
	if len(resolved) > 0 {
		shimPath, err = writeLauncherShim(home, dir)
		if err != nil {
			return syncResult{}, err
		}
		if err := ensurePathLine(filepath.Join(home, ".zshenv"), filepath.Dir(shimPath)); err != nil {
			return syncResult{}, err
		}
	}
	// The pre-shim mechanism exported credential values into every shell via
	// an env.sh sourced from ~/.zshenv. Remove it on every install so upgraded
	// machines stop carrying secrets in shell config.
	if err := cleanupLegacyShellEnv(home); err != nil {
		return syncResult{}, err
	}

	return syncResult{
		EnvCount:     len(resolved),
		SettingsPath: settingsPath,
		ShimPath:     shimPath,
		Files:        slices.Sorted(maps.Keys(fileSet)),
	}, nil
}

// writeLauncherShim writes an executable `claude` wrapper that re-execs the
// real binary through `ainfra exec`, so every launch gets freshly-resolved
// secrets in its process environment. The manifest dir is baked in at install
// time because the shim runs from any working directory. The shim holds no
// secret values, so 0755 is fine.
//
// A second shim, `claude-app`, targets the real claude binary by absolute
// path (resolved at install time). It exists for GUI hosts that launch claude
// by a configured path instead of PATH lookup — e.g. cmux's "Claude Binary
// Path" setting — where the name-based shim either never runs or would
// resolve the host's own wrapper. Skipped when no native binary is found.
func writeLauncherShim(home, manifestDir string) (string, error) {
	binDir := filepath.Join(home, ".config", "ainfra", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return "", err
	}
	// The ainfra binary is referenced absolutely: GUI-spawned processes (the
	// exact audience of these shims) get a minimal PATH without /opt/homebrew
	// /bin, so a bare `ainfra` fails with "not found" there. LookPath gives
	// the stable brew symlink; the running executable is the fallback.
	ainfraBin := "ainfra"
	if p, err := exec.LookPath("ainfra"); err == nil {
		ainfraBin = p
	} else if p, err := os.Executable(); err == nil {
		ainfraBin = p
	}
	shim := filepath.Join(binDir, "claude")
	content := fmt.Sprintf(`#!/bin/sh
# Generated by ainfra. Do not edit by hand.
# Resolves managed secrets into the process environment, then launches claude.
exec %q --chdir %q exec -- claude "$@"
`, ainfraBin, manifestDir)
	if err := os.WriteFile(shim, []byte(content), 0o755); err != nil {
		return "", err
	}
	if real := findRealClaude(binDir); real != "" {
		app := filepath.Join(binDir, "claude-app")
		appContent := fmt.Sprintf(`#!/bin/sh
# Generated by ainfra. Do not edit by hand.
# Absolute-target variant of the claude shim, for GUI hosts (e.g. cmux's
# Claude Binary Path setting) that must not re-resolve claude via PATH.
exec %q --chdir %q exec -- %q "$@"
`, ainfraBin, manifestDir, real)
		if err := os.WriteFile(app, []byte(appContent), 0o755); err != nil {
			return "", err
		}
		if err := os.Chmod(app, 0o755); err != nil {
			return "", err
		}
	}
	// WriteFile's mode only applies on creation; re-assert on updates.
	return shim, os.Chmod(shim, 0o755)
}

// findRealClaude returns the absolute path of the first claude on PATH that
// is neither in the shim dir nor a wrapper script (anything starting with
// "#!") — third-party wrappers like cmux's must not be baked into claude-app,
// or the shim and the wrapper would launch each other forever. Empty when no
// native binary is found. A symlink (the standard ~/.local/bin/claude) is
// returned as the symlink path, so self-updates that repoint it keep working.
func findRealClaude(shimDir string) string {
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if dir == "" || dir == shimDir {
			continue
		}
		cand := filepath.Join(dir, "claude")
		info, err := os.Stat(cand)
		if err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
			continue
		}
		f, err := os.Open(cand)
		if err != nil {
			continue
		}
		var magic [2]byte
		n, _ := f.Read(magic[:])
		f.Close()
		if n == 2 && magic[0] == '#' && magic[1] == '!' {
			continue
		}
		return cand
	}
	return ""
}

// ensurePathLine makes ~/.zshenv put the ainfra shim dir first on PATH,
// creating the rc file when missing. The line carries no secret values.
// Idempotent: a file that already references the shim dir (in any form) is
// left untouched, so a user can move or rewrite the line without ainfra
// re-appending it.
func ensurePathLine(rcPath, binDir string) error {
	marker := binDir
	ref := fmt.Sprintf("%q", binDir)
	if home, err := os.UserHomeDir(); err == nil {
		if rel, rerr := filepath.Rel(home, binDir); rerr == nil && !strings.HasPrefix(rel, "..") {
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
	line := fmt.Sprintf("\n# Added by ainfra — puts the secret-injecting launcher shims on PATH.\nexport PATH=%s:\"$PATH\"\n", ref)
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

// cleanupLegacyShellEnv removes the pre-shim secret delivery: the env.sh file
// of export lines and the ~/.zshenv line that sourced it (written by ainfra
// <= 0.2.11). Both put credential values into the environment of every shell.
// Idempotent, and a no-op on machines that never had them. Other content in
// ~/.zshenv — including the shim PATH line — is preserved.
func cleanupLegacyShellEnv(home string) error {
	envPath := filepath.Join(home, ".config", "ainfra", "env.sh")
	if err := os.Remove(envPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	rcPath := filepath.Join(home, ".zshenv")
	raw, err := os.ReadFile(rcPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	lines := strings.Split(string(raw), "\n")
	kept := make([]string, 0, len(lines))
	removed := false
	for _, l := range lines {
		if strings.Contains(l, ".config/ainfra/env.sh") ||
			strings.Contains(l, "Added by ainfra — exports managed secrets into the shell environment.") {
			removed = true
			continue
		}
		kept = append(kept, l)
	}
	if !removed {
		return nil
	}
	return os.WriteFile(rcPath, []byte(strings.Join(kept, "\n")), 0o644)
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
