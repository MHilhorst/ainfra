package main

import (
	"flag"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/MHilhorst/ainfra/internal/cli"
	"github.com/MHilhorst/ainfra/internal/lockfile"
	"github.com/MHilhorst/ainfra/internal/manifest"
	"github.com/MHilhorst/ainfra/internal/provider"
	"github.com/MHilhorst/ainfra/internal/provider/precond"
	"github.com/MHilhorst/ainfra/internal/resolve"
	"github.com/MHilhorst/ainfra/internal/ui"
	"github.com/MHilhorst/ainfra/internal/version"
)

// newVersionCommand prints the build version, optionally as JSON.
func newVersionCommand() *cli.Command {
	var asJSON bool
	return &cli.Command{
		Name:      "version",
		Summary:   "Print the ainfra version",
		UsageLine: "ainfra version [--json]",
		Example:   "ainfra version --json",
		SetFlags:  func(fs *flag.FlagSet) { fs.BoolVar(&asJSON, "json", false, "print as JSON") },
		Run: func(ctx cli.Context) int {
			if asJSON {
				fmt.Fprintf(ctx.Stdout, "{\"version\":%q}\n", version.Version)
			} else {
				fmt.Fprintf(ctx.Stdout, "ainfra %s\n", version.Version)
			}
			return 0
		},
	}
}

// mergeLocks returns a new lock that is the union of committed and personal
// entries. Personal entries take precedence over committed entries when both
// define the same key in the same channel.
func mergeLocks(committed, personal *lockfile.Lock) *lockfile.Lock {
	merge := func(a, b map[string]lockfile.Entry) map[string]lockfile.Entry {
		out := make(map[string]lockfile.Entry, len(a)+len(b))
		for k, v := range a {
			out[k] = v
		}
		for k, v := range b {
			out[k] = v
		}
		return out
	}
	return &lockfile.Lock{
		Version: committed.Version,
		Entries: lockfile.Entries{
			MCPServers:         merge(committed.Entries.MCPServers, personal.Entries.MCPServers),
			BackgroundServices: merge(committed.Entries.BackgroundServices, personal.Entries.BackgroundServices),
			Hooks:              merge(committed.Entries.Hooks, personal.Entries.Hooks),
			Commands:           merge(committed.Entries.Commands, personal.Entries.Commands),
			CLITools:           merge(committed.Entries.CLITools, personal.Entries.CLITools),
			Skills:             merge(committed.Entries.Skills, personal.Entries.Skills),
			Plugins:            merge(committed.Entries.Plugins, personal.Entries.Plugins),
			Rules:              merge(committed.Entries.Rules, personal.Entries.Rules),
			Tools:              merge(committed.Entries.Tools, personal.Entries.Tools),
		},
	}
}

func fileExists(path string) bool {
	fs := provider.OSFilesystem{}
	_, err := fs.ReadFile(path)
	return err == nil
}

// warnIfStale prints a warning when the manifest has changed since the last
// lock run, indicating the lock may be out of date.
func warnIfStale(ctx cli.Context, dir string, committed *lockfile.Lock) {
	if committed.ManifestHash == "" {
		return
	}
	current, err := resolve.CurrentManifestHash(dir)
	if err != nil {
		return
	}
	if current != committed.ManifestHash {
		c := ui.NewColorizer(ctx.Stderr, ctx.NoColor)
		fmt.Fprintln(ctx.Stderr, c.Yellow("warning: manifest has changed since last lock run — run `ainfra lock` to update"))
	}
}

func newPlanCommand() *cli.Command {
	return &cli.Command{
		Name:      "plan",
		Summary:   "Show the diff between desired and observed state",
		UsageLine: "ainfra plan",
		Example:   "ainfra plan",
		Run:       runPlan,
	}
}

func runPlan(ctx cli.Context) int {
	dir := ctx.Dir
	errColor := ui.NewColorizer(ctx.Stderr, ctx.NoColor)

	lockPath := filepath.Join(dir, "ainfra.lock")
	if !fileExists(lockPath) {
		ui.RenderError(ctx.Stderr, errColor, fmt.Errorf("ainfra.lock not found — run `ainfra lock` first"))
		return 1
	}

	committed, err := lockfile.Read(lockPath)
	if err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}
	personal, err := lockfile.Read(filepath.Join(dir, "ainfra.personal.lock"))
	if err != nil {
		personal = &lockfile.Lock{}
	}
	merged := mergeLocks(committed, personal)
	warnIfStale(ctx, dir, committed)

	providers, err := providersForDir(dir)
	if err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}
	orch := provider.NewOrchestrator(dir, buildEnv(dir), providers)
	plans, err := orch.PlanAll(merged)
	if err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}

	c := ui.NewColorizer(ctx.Stdout, ctx.NoColor)
	ui.RenderPlan(ctx.Stdout, c, plans)
	return 0
}

func newApplyCommand() *cli.Command {
	var yes bool
	return &cli.Command{
		Name:      "apply",
		Summary:   "Reconcile the environment to match the manifest",
		UsageLine: "ainfra apply [--yes]",
		Example:   "ainfra apply --yes",
		SetFlags:  func(fs *flag.FlagSet) { fs.BoolVar(&yes, "yes", false, "skip confirmation prompt") },
		Run: func(ctx cli.Context) int {
			return runApply(ctx, yes)
		},
	}
}

func runApply(ctx cli.Context, yes bool) int {
	dir := ctx.Dir
	errColor := ui.NewColorizer(ctx.Stderr, ctx.NoColor)

	lockPath := filepath.Join(dir, "ainfra.lock")
	if !fileExists(lockPath) {
		ui.RenderError(ctx.Stderr, errColor, fmt.Errorf("ainfra.lock not found — run `ainfra lock` first"))
		return 1
	}

	committed, err := lockfile.Read(lockPath)
	if err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}
	personal, err := lockfile.Read(filepath.Join(dir, "ainfra.personal.lock"))
	if err != nil {
		personal = &lockfile.Lock{}
	}
	merged := mergeLocks(committed, personal)
	warnIfStale(ctx, dir, committed)

	// Render resources with Payload so providers can write file content.
	rendered, err := resolve.RenderResources(dir)
	if err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}

	providers, err := providersForDir(dir)
	if err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}
	orch := provider.NewOrchestrator(dir, buildEnv(dir), providers)
	plans, err := orch.PlanAllRendered(rendered)
	if err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}

	// Check if there is anything to do.
	allEmpty := true
	for _, p := range plans {
		if !p.Empty() {
			allEmpty = false
			break
		}
	}
	if allEmpty {
		fmt.Fprintln(ctx.Stdout, "Nothing to do.")
		return 0
	}

	c := ui.NewColorizer(ctx.Stdout, ctx.NoColor)
	ui.RenderPlan(ctx.Stdout, c, plans)

	// Check preconditions before applying.
	if failures := checkPreconditions(dir, buildEnv(dir)); len(failures) > 0 {
		fmt.Fprintln(ctx.Stderr, "Preconditions failed:")
		for _, f := range failures {
			fmt.Fprintf(ctx.Stderr, "  %s: %s\n", f.ID, f.Remediation)
		}
		return 1
	}

	// Confirm unless --yes.
	if !yes {
		ok, err := ui.Confirm(ctx.Stdin, ctx.Stdout, "Do you want to apply these changes? (yes/no): ")
		if err != nil {
			ui.RenderError(ctx.Stderr, errColor, err)
			return 1
		}
		if !ok {
			fmt.Fprintln(ctx.Stdout, "Aborted.")
			return 0
		}
	}

	if err := orch.ApplyAllRendered(rendered, merged); err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}

	fmt.Fprintln(ctx.Stdout, "Apply complete.")
	return 0
}

func newCheckCommand() *cli.Command {
	return &cli.Command{
		Name:      "check",
		Summary:   "Verify the environment matches the lockfile; report drift",
		UsageLine: "ainfra check",
		Example:   "ainfra check",
		Run:       runCheck,
	}
}

func runCheck(ctx cli.Context) int {
	dir := ctx.Dir
	errColor := ui.NewColorizer(ctx.Stderr, ctx.NoColor)

	lockPath := filepath.Join(dir, "ainfra.lock")
	if !fileExists(lockPath) {
		ui.RenderError(ctx.Stderr, errColor, fmt.Errorf("ainfra.lock not found — run `ainfra lock` first"))
		return 1
	}

	committed, err := lockfile.Read(lockPath)
	if err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}
	personal, err := lockfile.Read(filepath.Join(dir, "ainfra.personal.lock"))
	if err != nil {
		personal = &lockfile.Lock{}
	}
	merged := mergeLocks(committed, personal)

	providers, err := providersForDir(dir)
	if err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}
	orch := provider.NewOrchestrator(dir, buildEnv(dir), providers)
	plans, err := orch.PlanAll(merged)
	if err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}

	allEmpty := true
	for _, p := range plans {
		if !p.Empty() {
			allEmpty = false
			break
		}
	}
	if allEmpty {
		fmt.Fprintln(ctx.Stdout, "No drift.")
		return 0
	}

	c := ui.NewColorizer(ctx.Stdout, ctx.NoColor)
	ui.RenderPlan(ctx.Stdout, c, plans)
	return 1
}

// checkPreconditions loads the manifest layers and runs all declared
// preconditions. Returns any failures.
func checkPreconditions(dir string, env provider.Env) []precond.Failure {
	layers, err := manifest.LoadLayers(dir)
	if err != nil {
		return nil
	}
	var ps []precond.Precondition
	seen := map[string]bool{}
	for _, layerName := range []manifest.Layer{manifest.LayerTeam, manifest.LayerRepo, manifest.LayerPersonal} {
		m, ok := layers[layerName]
		if !ok {
			continue
		}
		ids := make([]string, 0, len(m.Preconditions))
		for id := range m.Preconditions {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			if seen[id] {
				continue
			}
			seen[id] = true
			p := m.Preconditions[id]
			cmd := shellCommand(p.Check)
			ps = append(ps, precond.Precondition{
				ID:          id,
				Command:     cmd,
				Remediation: p.Remediation,
			})
		}
	}
	return precond.CheckAll(env, ps)
}

// shellCommand extracts the shell command from a manifest precondition check
// map. The check map may have a "shell" key whose value is the command string.
func shellCommand(check map[string]any) string {
	if check == nil {
		return ""
	}
	if v, ok := check["shell"]; ok {
		if s, ok := v.(string); ok {
			return strings.TrimSpace(s)
		}
	}
	return ""
}
