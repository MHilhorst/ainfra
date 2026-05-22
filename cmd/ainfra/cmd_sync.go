package main

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/MHilhorst/ainfra/internal/cli"
	"github.com/MHilhorst/ainfra/internal/lockfile"
	"github.com/MHilhorst/ainfra/internal/manifest"
	"github.com/MHilhorst/ainfra/internal/secret"
	"github.com/MHilhorst/ainfra/internal/ui"
)

// newSyncCommand returns the `ainfra sync` command: it resolves every secret
// reference in the lockfile and writes the values into the Claude Code
// settings env block, so a normally-launched Claude Code (terminal, IDE, or
// app) has them — no `ainfra exec` wrapper required.
func newSyncCommand() *cli.Command {
	return &cli.Command{
		Name:      "sync",
		Summary:   "Resolve secrets and write them to the Claude Code settings env block",
		UsageLine: "ainfra sync",
		Example:   "ainfra sync",
		Run: func(ctx cli.Context) int {
			return runSyncWith(ctx, secret.DefaultRegistry())
		},
	}
}

// runSyncWith is the testable core of `ainfra sync`.
func runSyncWith(ctx cli.Context, reg *secret.Registry) int {
	errColor := ui.NewColorizer(ctx.Stderr, ctx.NoColor)
	count, path, err := syncSecrets(ctx.Dir, reg)
	if err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}
	fmt.Fprintf(ctx.Stdout, "Wrote %d secrets to %s\n", count, path)
	return 0
}

// syncSecrets resolves every secret referenced by the lockfiles at dir and
// writes the values into the Claude Code settings env block. It returns the
// number written and the settings path. Shared by `ainfra sync` and the final
// step of `ainfra apply`.
//
// Two secret shapes are resolved: ordinary single-value secrets (one ref ->
// one env var) and envFile secrets (one ref -> a .env blob that expands into
// many env vars).
func syncSecrets(dir string, reg *secret.Registry) (int, string, error) {
	lockPath := filepath.Join(dir, "ainfra.lock")
	if !fileExists(lockPath) {
		return 0, "", fmt.Errorf("ainfra.lock not found — run `ainfra lock` first")
	}
	committed, err := lockfile.Read(lockPath)
	if err != nil {
		return 0, "", err
	}
	personal, err := lockfile.Read(filepath.Join(dir, "ainfra.personal.lock"))
	if err != nil {
		personal = &lockfile.Lock{}
	}

	// The single-value secret set is the union of both lockfiles.
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

	// envFile secrets: a single reference whose resolved value is a .env blob.
	// Each line expands into its own variable, so one 1Password item can stand
	// in for a whole environment.
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
					failures = append(failures, fmt.Sprintf("  env-file secret %q: %v", id, rerr))
					continue
				}
				maps.Copy(resolved, parseEnvBlob(blob))
			}
		}
	}

	if len(failures) > 0 {
		return 0, "", fmt.Errorf("could not resolve secrets:\n%s", strings.Join(failures, "\n"))
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return 0, "", err
	}
	settingsPath := filepath.Join(home, ".claude", "settings.local.json")
	if err := writeSettingsEnv(settingsPath, resolved); err != nil {
		return 0, "", err
	}
	return len(resolved), settingsPath, nil
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
	return os.WriteFile(path, append(out, '\n'), 0o600)
}
