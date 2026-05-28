package main

import (
	"fmt"
	"os"

	"github.com/MHilhorst/ainfra/internal/cli"
	"github.com/MHilhorst/ainfra/internal/lockfile"
	"github.com/MHilhorst/ainfra/internal/manifest"
	"github.com/MHilhorst/ainfra/internal/provider"
	"github.com/MHilhorst/ainfra/internal/resolve"
	"github.com/MHilhorst/ainfra/internal/ui"
)

// injectStalenessHook adds the built-in SessionStart staleness hook into the
// rendered hooks channel and the in-memory lock that drives the install pass.
// The hook is infra plumbing — it never appears in ainfra.yaml or the
// persisted lockfile; only `install` knows about it, and the applied ledger
// records it like any other hook so subsequent plans show no drift.
//
// Returns true when the hook was injected (so callers can branch on it),
// false when there is no repo manifest or the manifest opts out via
// `stalenessWarning: false`.
func injectStalenessHook(dir string, rendered map[string][]provider.Resource, lock *lockfile.Lock) bool {
	if _, err := os.Stat(dir + "/ainfra.yaml"); err != nil {
		return false
	}
	layers, err := manifest.LoadLayers(dir)
	if err != nil {
		return false
	}
	repo := layers[manifest.LayerRepo]
	if repo == nil {
		return false
	}
	if repo.StalenessWarning != nil && !*repo.StalenessWarning {
		return false
	}
	payload := map[string]any{
		"event":   "SessionStart",
		"command": manifest.StalenessHookCommand,
	}
	hash := lockfile.ContentHash(payload)

	if rendered != nil {
		found := false
		for _, r := range rendered["hooks"] {
			if r.ID == manifest.StalenessHookID {
				found = true
				break
			}
		}
		if !found {
			rendered["hooks"] = append(rendered["hooks"], provider.Resource{
				ID:          manifest.StalenessHookID,
				Channel:     "hooks",
				Layer:       string(manifest.LayerRepo),
				ContentHash: hash,
				Payload:     payload,
			})
		}
	}
	if lock != nil {
		if lock.Entries.Hooks == nil {
			lock.Entries.Hooks = map[string]lockfile.Entry{}
		}
		if _, exists := lock.Entries.Hooks[manifest.StalenessHookID]; !exists {
			lock.Entries.Hooks[manifest.StalenessHookID] = lockfile.Entry{
				Layer:       string(manifest.LayerRepo),
				ContentHash: hash,
			}
		}
	}
	return true
}

// newStalenessCheckCommand is the SessionStart hook ainfra installs by default
// in every managed repo. It compares the current manifest hash to the hash
// recorded in the applied ledger and prints one stderr line when they differ.
// Hidden because users never invoke it themselves; Claude Code does at every
// session start. Exit code is always 0 — the hook must never block Claude.
func newStalenessCheckCommand() *cli.Command {
	return &cli.Command{
		Name:      "_staleness-check",
		Summary:   "Internal: SessionStart hook reporting manifest staleness",
		UsageLine: "ainfra _staleness-check",
		Hidden:    true,
		Run:       runStalenessCheck,
	}
}

func runStalenessCheck(ctx cli.Context) int {
	dir := ctx.Dir
	current, err := resolve.CurrentManifestHash(dir)
	if err != nil {
		// No manifest, or unreadable — silently skip. The hook fires on every
		// session, including in directories without ainfra.yaml; that's not
		// an error condition.
		return 0
	}
	if current == "" {
		return 0
	}
	applied, err := provider.ReadApplied(dir)
	if err != nil || applied == nil || applied.ManifestHash == "" {
		// Never applied — staleness is undefined; stay silent.
		return 0
	}
	if applied.ManifestHash == current {
		return 0
	}
	c := ui.NewColorizer(ctx.Stderr, ctx.NoColor)
	fmt.Fprintln(ctx.Stderr, c.Yellow(
		"ainfra: this repo's manifest has changed since the last install — run `ainfra install` to refresh"))
	return 0
}
