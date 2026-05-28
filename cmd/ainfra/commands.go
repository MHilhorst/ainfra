package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/MHilhorst/ainfra/internal/cli"
	"github.com/MHilhorst/ainfra/internal/lockfile"
	"github.com/MHilhorst/ainfra/internal/manifest"
	"github.com/MHilhorst/ainfra/internal/provider"
	"github.com/MHilhorst/ainfra/internal/provider/precond"
	"github.com/MHilhorst/ainfra/internal/resolve"
	"github.com/MHilhorst/ainfra/internal/schema"
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
		Version:      committed.Version,
		ManifestHash: committed.ManifestHash,
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
// partitionLockByLayer splits a merged lock into a repo-scope subset and a
// user-scope subset. An entry routes to user-scope when its Layer is
// "personal". mcpServers is forced into repo-scope regardless of layer —
// matches the partition rule used for rendered resources so the ledger and
// the apply pass agree on which scope owns each entry.
func partitionLockByLayer(l *lockfile.Lock) (repo, user *lockfile.Lock) {
	repo = &lockfile.Lock{Version: l.Version, GeneratedAt: l.GeneratedAt, ManifestHash: l.ManifestHash}
	user = &lockfile.Lock{Version: l.Version, GeneratedAt: l.GeneratedAt, ManifestHash: l.ManifestHash}

	// initialise all the entry maps so downstream nil-map writes don't panic.
	for _, lock := range []*lockfile.Lock{repo, user} {
		lock.Entries = lockfile.Entries{
			MCPServers:         map[string]lockfile.Entry{},
			BackgroundServices: map[string]lockfile.Entry{},
			Hooks:              map[string]lockfile.Entry{},
			Commands:           map[string]lockfile.Entry{},
			CLITools:           map[string]lockfile.Entry{},
			Skills:             map[string]lockfile.Entry{},
			Marketplaces:       map[string]lockfile.Entry{},
			Plugins:            map[string]lockfile.Entry{},
			Rules:              map[string]lockfile.Entry{},
			Tools:              map[string]lockfile.Entry{},
		}
	}

	route := func(channel string, src, dstRepo, dstUser map[string]lockfile.Entry) {
		for id, e := range src {
			if e.Layer == "personal" && channel != "mcpServers" {
				dstUser[id] = e
			} else {
				dstRepo[id] = e
			}
		}
	}
	route("mcpServers", l.Entries.MCPServers, repo.Entries.MCPServers, user.Entries.MCPServers)
	route("backgroundServices", l.Entries.BackgroundServices, repo.Entries.BackgroundServices, user.Entries.BackgroundServices)
	route("hooks", l.Entries.Hooks, repo.Entries.Hooks, user.Entries.Hooks)
	route("commands", l.Entries.Commands, repo.Entries.Commands, user.Entries.Commands)
	route("cliTools", l.Entries.CLITools, repo.Entries.CLITools, user.Entries.CLITools)
	route("skills", l.Entries.Skills, repo.Entries.Skills, user.Entries.Skills)
	route("marketplaces", l.Entries.Marketplaces, repo.Entries.Marketplaces, user.Entries.Marketplaces)
	route("plugins", l.Entries.Plugins, repo.Entries.Plugins, user.Entries.Plugins)
	route("rules", l.Entries.Rules, repo.Entries.Rules, user.Entries.Rules)
	route("tools", l.Entries.Tools, repo.Entries.Tools, user.Entries.Tools)
	return repo, user
}

// hasAnyEntry reports whether any channel in the lockfile carries at least
// one entry. Used to decide whether the user-scope cleanup pass should run.
func hasAnyEntry(l *lockfile.Lock) bool {
	if l == nil {
		return false
	}
	e := l.Entries
	return len(e.MCPServers)+len(e.BackgroundServices)+len(e.Hooks)+
		len(e.Commands)+len(e.CLITools)+len(e.Skills)+
		len(e.Marketplaces)+len(e.Plugins)+len(e.Rules)+len(e.Tools) > 0
}

// anyResources reports whether a rendered map has at least one resource.
func anyResources(rendered map[string][]provider.Resource) bool {
	for _, rs := range rendered {
		if len(rs) > 0 {
			return true
		}
	}
	return false
}

// plansEmpty reports whether every channel plan in the map is empty (or the
// map is nil).
func plansEmpty(plans map[string]provider.ChannelPlan) bool {
	for _, p := range plans {
		if !p.Empty() {
			return false
		}
	}
	return true
}

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
		fmt.Fprintln(ctx.Stderr, c.Yellow("warning: ainfra.yaml has changed since the last `ainfra lock` — the lockfile is stale. Run `ainfra lock` to refresh it."))
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
	fmt.Fprintf(w, "Applied %d entries, skipped %d, failed %d.\n", applied, skipped, failed)
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

// newInstallCommand is the front-page reconcile verb. It plans, applies, and
// syncs secrets in one pass; --dry-run + --strict gives the CI drift shape.
func newInstallCommand() *cli.Command {
	var yes, dryRun, noInstall, strict, printSchema bool
	var from string
	return &cli.Command{
		Name:      "install",
		Summary:   "Install/update everything in ainfra.yaml (writes config files, installs CLI tools)",
		UsageLine: "ainfra install [--yes] [--dry-run] [--strict] [--no-install] [--from <url-or-dir>] [--print-schema]",
		Example:   "ainfra install --yes",
		SetFlags: func(fs *flag.FlagSet) {
			fs.BoolVar(&yes, "yes", false, "skip confirmation prompt")
			fs.BoolVar(&dryRun, "dry-run", false, "preview without writing (replaces 'ainfra plan')")
			fs.BoolVar(&strict, "strict", false, "with --dry-run, exit non-zero on any drift (CI shape; replaces 'ainfra check')")
			fs.BoolVar(&noInstall, "no-install", false, "write config files but skip running CLI-tool installers")
			fs.StringVar(&from, "from", "", "install from a published artifact (URL or dir) instead of this repo")
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
		fmt.Fprintln(ctx.Stdout, "Nothing to do — the artifact is already applied.")
		return 0
	}

	c := ui.NewColorizer(ctx.Stdout, ctx.NoColor)
	ui.RenderPlan(ctx.Stdout, c, plans)

	if !yes {
		ok, err := ui.Confirm(ctx.Stdin, ctx.Stdout, "Apply these changes? (yes/no): ")
		if err != nil {
			ui.RenderError(ctx.Stderr, errColor, err)
			return 1
		}
		if !ok {
			fmt.Fprintln(ctx.Stdout, "Aborted — no changes were made.")
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
	fmt.Fprintln(ctx.Stdout, "Apply complete — your environment matches the artifact.")
	return 0
}

func runApply(ctx cli.Context, yes, dryRun, noInstall, strict bool) int {
	dir := ctx.Dir
	errColor := ui.NewColorizer(ctx.Stderr, ctx.NoColor)

	lockPath := filepath.Join(dir, "ainfra.lock")
	if !fileExists(lockPath) {
		ui.RenderError(ctx.Stderr, errColor, fmt.Errorf("no ainfra.lock yet — run `ainfra lock` to resolve ainfra.yaml into a lockfile, then re-run this command"))
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

	// Auto-emit the SessionStart staleness hook when a repo manifest exists
	// and hasn't opted out. The hook is infra plumbing, not a manifest entry —
	// it stays out of `ainfra.yaml`, the persisted lockfile, and `ainfra list`.
	// The orchestrator and applied ledger treat it like any other hook.
	injectStalenessHook(dir, rendered, merged)

	providers, err := providersForDir(dir)
	if err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}
	env := buildEnv(dir)
	env.DryRun = dryRun
	env.NoInstall = noInstall

	// Partition resources by scope. Personal-layer entries (except MCP servers,
	// see provider.PartitionByScope) materialize to ~/.claude/ instead of the
	// repo's .claude/, so the user's global personal config follows them
	// across repos. Repo and team entries continue to land in repo paths.
	repoRendered, userRendered := provider.PartitionByScope(rendered)

	// Warn (once) about MCP entries from the personal layer — Claude Code
	// reads user-level MCP servers from ~/.claude.json, which is a different
	// file with a different format than the project's .mcp.json. Until that
	// channel learns the user-scope path, personal-layer MCP entries fall
	// back to repo-scope behavior.
	if provider.HasUserScopeMCP(rendered) {
		c := ui.NewColorizer(ctx.Stderr, ctx.NoColor)
		fmt.Fprintln(ctx.Stderr, c.Yellow(
			"warning: personal-layer mcpServers entries are written to the repo's .mcp.json today. "+
				"User-scope MCP (~/.claude.json) is a follow-up; the server will only be visible in this repo."))
	}

	orch := provider.NewOrchestratorScoped(dir, provider.ScopeRepo, env, providers)
	plans, err := orch.PlanAllRendered(repoRendered)
	if err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}

	// User-scope plan. Created whenever either the rendered set has
	// personal-layer entries OR the user-scope applied ledger does — the
	// latter handles the cleanup case where an entry has been removed from
	// the personal manifest and needs to be deleted from ~/.claude/.
	var userOrch *provider.Orchestrator
	var userPlans map[string]provider.ChannelPlan
	home, herr := os.UserHomeDir()
	priorUser, _ := provider.ReadAppliedUser()
	userLedgerNonEmpty := priorUser != nil && hasAnyEntry(priorUser)
	if herr == nil && (anyResources(userRendered) || userLedgerNonEmpty) {
		userEnv := env
		userEnv.Root = home
		userOrch = provider.NewOrchestratorScoped(home, provider.ScopeUser, userEnv, providers)
		userPlans, err = userOrch.PlanAllRendered(userRendered)
		if err != nil {
			ui.RenderError(ctx.Stderr, errColor, err)
			return 1
		}
	}

	// Check if there is anything to do.
	allEmpty := plansEmpty(plans) && plansEmpty(userPlans)
	if allEmpty {
		fmt.Fprintln(ctx.Stdout, "Nothing to do — your environment already matches ainfra.yaml.")
		return 0
	}

	c := ui.NewColorizer(ctx.Stdout, ctx.NoColor)
	ui.RenderPlan(ctx.Stdout, c, plans)
	if userPlans != nil {
		fmt.Fprintln(ctx.Stdout, c.Bold("User-scope (~/.claude/, applies across all your repos):"))
		ui.RenderPlan(ctx.Stdout, c, userPlans)
	}

	// Check preconditions before applying.
	if failures := checkPreconditions(dir, env); len(failures) > 0 {
		fmt.Fprintln(ctx.Stderr, "Setup checks failed — fix these before applying:")
		for _, f := range failures {
			fmt.Fprintf(ctx.Stderr, "  %s: %s\n", f.ID, f.Remediation)
		}
		return 1
	}

	// Confirm unless --yes or --dry-run (a dry run changes nothing).
	if !yes && !dryRun {
		ok, err := ui.Confirm(ctx.Stdin, ctx.Stdout, "Apply these changes? (yes/no): ")
		if err != nil {
			ui.RenderError(ctx.Stderr, errColor, err)
			return 1
		}
		if !ok {
			fmt.Fprintln(ctx.Stdout, "Aborted — no changes were made.")
			return 0
		}
	}

	repoLock, userLock := partitionLockByLayer(merged)

	results, err := orch.ApplyAllRendered(repoRendered, repoLock)
	if !dryRun {
		renderApplySummary(ctx.Stdout, results)
	}
	if err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}

	// User-scope pass: same flow against ~/.claude/ when personal-layer
	// entries exist. Failures here don't block the repo apply — they
	// surface in the summary.
	if userOrch != nil {
		userResults, uerr := userOrch.ApplyAllRendered(userRendered, userLock)
		if !dryRun {
			fmt.Fprintln(ctx.Stdout, "User-scope (~/.claude/, applies across all your repos):")
			renderApplySummary(ctx.Stdout, userResults)
		}
		if uerr != nil {
			ui.RenderError(ctx.Stderr, errColor, uerr)
			// Don't return — repo apply succeeded; user-scope failure is reported.
		}
	}

	if dryRun {
		fmt.Fprintln(ctx.Stdout, "Dry run complete — nothing was written. Re-run without --dry-run to apply.")
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
	fmt.Fprintf(ctx.Stdout, "Wrote %d secret(s) into Claude Code settings (%s).\n", res.EnvCount, res.SettingsPath)
	for _, f := range res.Files {
		fmt.Fprintf(ctx.Stdout, "Wrote credential file %s\n", f)
	}
	fmt.Fprintln(ctx.Stdout, "Apply complete — your environment now matches ainfra.yaml.")
	return 0
}

// runPrintSchema dumps the manifest's JSON Schema; wired in via `install --print-schema`.
func runPrintSchema(ctx cli.Context) int {
	out, err := json.MarshalIndent(schema.Generate(), "", "  ")
	if err != nil {
		ui.RenderError(ctx.Stderr, ui.NewColorizer(ctx.Stderr, ctx.NoColor), err)
		return 1
	}
	fmt.Fprintln(ctx.Stdout, string(out))
	return 0
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
