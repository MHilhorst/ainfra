package main

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"

	"github.com/MHilhorst/ainfra/internal/cli"
	"github.com/MHilhorst/ainfra/internal/lockfile"
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

	lockPath := filepath.Join(ctx.Dir, "ainfra.lock")
	if !fileExists(lockPath) {
		ui.RenderError(ctx.Stderr, errColor, fmt.Errorf("ainfra.lock not found — run `ainfra lock` first"))
		return 1
	}
	committed, err := lockfile.Read(lockPath)
	if err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}
	personal, err := lockfile.Read(filepath.Join(ctx.Dir, "ainfra.personal.lock"))
	if err != nil {
		personal = &lockfile.Lock{}
	}

	// The secret set is the union of both lockfiles.
	refs := map[string]lockfile.SecretRef{}
	maps.Copy(refs, committed.Secrets)
	maps.Copy(refs, personal.Secrets)

	// Resolve every ref, collecting all failures before aborting.
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
	if len(failures) > 0 {
		fmt.Fprintln(ctx.Stderr, "Could not resolve secrets:")
		for _, f := range failures {
			fmt.Fprintln(ctx.Stderr, f)
		}
		return 1
	}

	home, err := os.UserHomeDir()
	if err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}
	settingsPath := filepath.Join(home, ".claude", "settings.local.json")
	if err := writeSettingsEnv(settingsPath, resolved); err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}

	fmt.Fprintf(ctx.Stdout, "Wrote %d secrets to %s\n", len(resolved), settingsPath)
	return 0
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
