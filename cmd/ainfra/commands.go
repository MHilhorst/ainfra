package main

import (
	"flag"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/MHilhorst/ainfra/internal/cli"
	"github.com/MHilhorst/ainfra/internal/lockfile"
	"github.com/MHilhorst/ainfra/internal/manifest"
	"github.com/MHilhorst/ainfra/internal/provider"
	"github.com/MHilhorst/ainfra/internal/provider/precond"
	"github.com/MHilhorst/ainfra/internal/resolve"
	"github.com/MHilhorst/ainfra/internal/secret"
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
			Marketplaces:       merge(committed.Entries.Marketplaces, personal.Entries.Marketplaces),
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

// warnIfAinfraVersionMismatch reads the repo layer's optional
// `ainfraVersion:` field and warns to stderr if the running binary's
// version does not match. Missing field is silent — only opt-in repos
// surface a warning. Exact-string match in v1; semver ranges deferred.
func warnIfAinfraVersionMismatch(ctx cli.Context, dir string) {
	layers, err := manifest.LoadLayers(dir)
	if err != nil {
		return
	}
	repo := layers[manifest.LayerRepo]
	if repo == nil || repo.AinfraVersion == "" {
		return
	}
	if repo.AinfraVersion == version.Version {
		return
	}
	if os.Getenv("AINFRA_QUIET") != "" {
		return
	}
	c := ui.NewColorizer(ctx.Stderr, ctx.NoColor)
	fmt.Fprintln(ctx.Stderr, c.Yellow(
		fmt.Sprintf("warning: this repo expects ainfra %s; you are running %s. "+
			"Different ainfra versions can produce different lockfiles. "+
			"See https://github.com/MHilhorst/ainfra/releases to upgrade.",
			repo.AinfraVersion, version.Version)))
}

func newPlanCommand() *cli.Command {
	return &cli.Command{
		Name:          "plan",
		Summary:       "Show the diff between desired and observed state",
		UsageLine:     "ainfra plan",
		Example:       "ainfra plan",
		Hidden:        true,
		DeprecatedFor: "install --dry-run",
		Run:           runPlan,
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

// renderApplySummary prints the one-line apply tally and, for any failed or
// skipped resource, a reason line.
func renderApplySummary(w io.Writer, results []provider.ApplyResult) {
	var applied, skipped, failed, warned int
	for _, r := range results {
		applied += len(r.Applied)
		skipped += len(r.Skipped)
		failed += len(r.Failed)
		warned += len(r.Warnings)
	}
	fmt.Fprintf(w, "applied %d, skipped %d, failed %d\n", applied, skipped, failed)
	for _, r := range results {
		for _, f := range r.Failed {
			fmt.Fprintf(w, "  failed:  %s %s — %v\n", r.Channel, f.Change.ID, f.Err)
		}
		for _, s := range r.Skipped {
			fmt.Fprintf(w, "  skipped: %s %s — %s\n", r.Channel, s.Change.ID, s.Reason)
		}
		for _, wn := range r.Warnings {
			fmt.Fprintf(w, "  warning: %s %s — %s\n", r.Channel, wn.Change.ID, wn.Reason)
		}
	}
}

func newApplyCommand() *cli.Command {
	var yes, dryRun, noInstall, strict bool
	var from string
	return &cli.Command{
		Name:          "apply",
		Summary:       "Reconcile the environment to match the manifest or a published artifact",
		UsageLine:     "ainfra apply [--yes] [--dry-run] [--strict] [--no-install] [--from <url-or-dir>]",
		Example:       "ainfra apply --from https://downloads.example.com/ainfra/sales --yes",
		Hidden:        true,
		DeprecatedFor: "install",
		SetFlags: func(fs *flag.FlagSet) {
			fs.BoolVar(&yes, "yes", false, "skip confirmation prompt")
			fs.BoolVar(&dryRun, "dry-run", false, "preview the apply without writing anything")
			fs.BoolVar(&strict, "strict", false, "exit non-zero when --dry-run finds drift (CI shape)")
			fs.BoolVar(&noInstall, "no-install", false, "reconcile config files but skip CLI-tool installs")
			fs.StringVar(&from, "from", "", "reconcile against a published artifact instead of a repo")
		},
		Run: func(ctx cli.Context) int {
			if from != "" {
				return runApplyFrom(ctx, from, yes)
			}
			return runApply(ctx, yes, dryRun, noInstall, strict)
		},
	}
}

// newInstallCommand is the renamed front-page reconcile verb. Same body as
// apply, plus the --strict flag that makes --dry-run exit non-zero on any drift
// (CI shape, replacing the standalone `check` verb).
func newInstallCommand() *cli.Command {
	var yes, dryRun, noInstall, strict, printSchema bool
	var from string
	return &cli.Command{
		Name:      "install",
		Summary:   "Reconcile the environment to match the manifest (install/update everything)",
		UsageLine: "ainfra install [--yes] [--dry-run] [--strict] [--no-install] [--from <url-or-dir>] [--print-schema]",
		Example:   "ainfra install --yes",
		SetFlags: func(fs *flag.FlagSet) {
			fs.BoolVar(&yes, "yes", false, "skip confirmation prompt")
			fs.BoolVar(&dryRun, "dry-run", false, "preview without writing (replaces 'ainfra plan')")
			fs.BoolVar(&strict, "strict", false, "with --dry-run, exit non-zero on any drift (CI shape; replaces 'ainfra check')")
			fs.BoolVar(&noInstall, "no-install", false, "reconcile config files but skip CLI-tool installs")
			fs.StringVar(&from, "from", "", "reconcile against a published artifact instead of a repo")
			fs.BoolVar(&printSchema, "print-schema", false, "print the JSON Schema for ainfra.yaml and exit (replaces 'ainfra schema')")
		},
		Run: func(ctx cli.Context) int {
			if printSchema {
				return runPrintSchema(ctx)
			}
			if from != "" {
				return runApplyFrom(ctx, from, yes)
			}
			return runApply(ctx, yes, dryRun, noInstall, strict)
		},
	}
}

func runApplyFrom(ctx cli.Context, from string, yes bool) int {
	errColor := ui.NewColorizer(ctx.Stderr, ctx.NoColor)

	dir, cleanup, err := artifactSource(from)
	if err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}
	defer cleanup()

	providers, rendered, lock, err := loadArtifact(dir)
	if err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}
	env, home, err := subscriberEnv()
	if err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}
	orch := provider.NewOrchestrator(home, env, providers)
	plans, err := orch.PlanAllRendered(rendered)
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
		fmt.Fprintln(ctx.Stdout, "Nothing to do.")
		return 0
	}

	c := ui.NewColorizer(ctx.Stdout, ctx.NoColor)
	ui.RenderPlan(ctx.Stdout, c, plans)

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

	results, err := orch.ApplyAllRendered(rendered, lock)
	renderApplySummary(ctx.Stdout, results)
	if err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}
	appendApplyHistory(home, "apply --from", "", lock.ManifestHash, results, ctx.Stderr)
	fmt.Fprintln(ctx.Stdout, "Apply complete.")
	return 0
}

func runApply(ctx cli.Context, yes, dryRun, noInstall, strict bool) int {
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
	warnIfAinfraVersionMismatch(ctx, dir)

	// Render resources with Payload so providers can write file content.
	rctx := resolve.NewContextFromEnv(ctx.Identity, dir, dir)
	rendered, err := resolve.RenderResourcesFor(dir, provider.ExecRunner{}, rctx)
	if err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}

	providers, err := providersForDir(dir)
	if err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}
	env := buildEnv(dir)
	env.DryRun = dryRun
	env.NoInstall = noInstall
	orch := provider.NewOrchestrator(dir, env, providers)
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
	if failures := checkPreconditions(dir, env); len(failures) > 0 {
		fmt.Fprintln(ctx.Stderr, "Preconditions failed:")
		for _, f := range failures {
			fmt.Fprintf(ctx.Stderr, "  %s: %s\n", f.ID, f.Remediation)
		}
		return 1
	}

	// Confirm unless --yes or --dry-run (a dry run changes nothing).
	if !yes && !dryRun {
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

	results, err := orch.ApplyAllRendered(rendered, merged)
	if !dryRun {
		renderApplySummary(ctx.Stdout, results)
	}
	if err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}

	if dryRun {
		fmt.Fprintln(ctx.Stdout, "Dry run complete — no changes were applied.")
		if strict {
			return 1
		}
		return 0
	}

	// Record apply history for Govern groundwork. Failures here are reported
	// but never fail the apply — history is observational.
	layers, lerr := manifest.LoadLayers(dir)
	agentID := ""
	if lerr == nil {
		agentID, _, _ = manifest.ResolveAgent(layers)
	}
	appendApplyHistory(dir, "apply", agentID, merged.ManifestHash, results, ctx.Stderr)

	// Final step: resolve the manifest's secrets and write them into the
	// Claude Code settings env block, so a normally-launched Claude has them.
	// This makes `ainfra apply` a complete setup — config and credentials.
	res, serr := syncSecrets(dir, secret.DefaultRegistry())
	if serr != nil {
		ui.RenderError(ctx.Stderr, errColor, serr)
		return 1
	}
	fmt.Fprintf(ctx.Stdout, "Synced %d secrets to %s\n", res.EnvCount, res.SettingsPath)
	for _, f := range res.Files {
		fmt.Fprintf(ctx.Stdout, "Synced credential file %s\n", f)
	}
	fmt.Fprintln(ctx.Stdout, "Apply complete.")
	return 0
}

func newCheckCommand() *cli.Command {
	var from string
	return &cli.Command{
		Name:          "check",
		Summary:       "Verify the environment matches the lockfile or a published artifact; report drift",
		UsageLine:     "ainfra check [--from <url-or-dir>]",
		Example:       "ainfra check --from https://downloads.example.com/ainfra/sales",
		Hidden:        true,
		DeprecatedFor: "install --dry-run --strict",
		SetFlags: func(fs *flag.FlagSet) {
			fs.StringVar(&from, "from", "", "check against a published artifact instead of a repo")
		},
		Run: func(ctx cli.Context) int {
			if from != "" {
				return runCheckFrom(ctx, from)
			}
			return runCheck(ctx)
		},
	}
}

func runCheckFrom(ctx cli.Context, from string) int {
	errColor := ui.NewColorizer(ctx.Stderr, ctx.NoColor)

	dir, cleanup, err := artifactSource(from)
	if err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}
	defer cleanup()

	providers, rendered, _, err := loadArtifact(dir)
	if err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}
	env, home, err := subscriberEnv()
	if err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}
	orch := provider.NewOrchestrator(home, env, providers)
	plans, err := orch.PlanAllRendered(rendered)
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

	secretFailures := checkSecrets(committed, personal, secret.DefaultRegistry())
	precondFailures := checkPreconditions(dir, buildEnv(dir))

	if allEmpty && len(secretFailures) == 0 && len(precondFailures) == 0 {
		fmt.Fprintln(ctx.Stdout, "No drift.")
		return 0
	}

	c := ui.NewColorizer(ctx.Stdout, ctx.NoColor)
	if !allEmpty {
		ui.RenderPlan(ctx.Stdout, c, plans)
	}
	if len(precondFailures) > 0 {
		fmt.Fprintln(ctx.Stderr, "Preconditions failed:")
		for _, f := range precondFailures {
			fmt.Fprintf(ctx.Stderr, "  %s: %s\n", f.ID, f.Remediation)
		}
	}
	if len(secretFailures) > 0 {
		fmt.Fprintln(ctx.Stderr, "Unresolvable secrets:")
		for _, f := range secretFailures {
			fmt.Fprintf(ctx.Stderr, "  %s\n", f)
		}
	}
	return 1
}

// checkSecrets verifies every secret reference in both lockfiles is resolvable.
// It returns one message per unresolvable ref; the messages never contain a
// secret value.
func checkSecrets(committed, personal *lockfile.Lock, reg *secret.Registry) []string {
	refs := map[string]lockfile.SecretRef{}
	maps.Copy(refs, committed.Secrets)
	maps.Copy(refs, personal.Secrets)

	var failures []string
	for _, v := range slices.Sorted(maps.Keys(refs)) {
		if err := reg.Check(refs[v].Ref); err != nil {
			failures = append(failures, err.Error())
		}
	}
	return failures
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
			ps = append(ps, toPrecondition(id, m.Preconditions[id]))
		}
	}
	return precond.CheckAll(env, ps)
}

// toPrecondition converts a manifest precondition into a precond.Precondition.
// It reads check.type: "dns-resolves" (evaluated against check.host) — anything
// else falls back to a "shell" command (the default kind).
func toPrecondition(id string, p manifest.Precondition) precond.Precondition {
	pc := precond.Precondition{ID: id, Remediation: p.Remediation}
	switch t, _ := p.Check["type"].(string); t {
	case "dns-resolves":
		pc.Kind = "dns-resolves"
		pc.Host, _ = p.Check["host"].(string)
	default:
		if s, ok := p.Check["shell"].(string); ok {
			pc.Command = strings.TrimSpace(s)
		}
	}
	return pc
}
