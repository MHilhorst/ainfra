package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

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

// renderOffered prints the undeclared entries prune reported but did not
// remove, and how to keep them.
//
// Prune never deletes on first sight, so this report is the user's chance to
// declare something before a later run removes it. Undeclared usually means
// "never got around to declaring it" rather than "unwanted", which is why the
// list leads with how to keep things rather than how to delete them.
func renderOffered(w io.Writer, c ui.Colorizer, repoOffers, userOffers []provider.Change) {
	if len(repoOffers) == 0 && len(userOffers) == 0 {
		return
	}
	fmt.Fprintln(w, c.Yellow("Not declared in ainfra (nothing removed yet):"))

	// The two scopes need different advice, and getting it wrong is dangerous.
	// Config in ~/.claude/ applies in every repo, so declaring it in one repo's
	// ainfra.personal.yaml would leave it undeclared everywhere else — and a
	// --prune run in another repo would report and then remove it. --global is
	// the only correct home for those.
	if len(userOffers) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, c.Bold("  In ~/.claude/ (applies in every repo):"))
		renderOfferGroup(w, userOffers)
		fmt.Fprintln(w)
		// Flags before positionals: Go's flag package stops parsing at the
		// first positional, so `add command ship --global` silently ignores
		// --global and writes the team's ainfra.yaml instead.
		fmt.Fprintln(w, "  To keep one, declare it in your global personal manifest:")
		fmt.Fprintln(w, "    ainfra add --global <channel> <id> <source>")
	}
	if len(repoOffers) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, c.Bold("  In this repo:"))
		renderOfferGroup(w, repoOffers)
		fmt.Fprintln(w)
		fmt.Fprintln(w, "  To keep one, declare it in this repo's manifest:")
		fmt.Fprintln(w, "    ainfra add <channel> <id> <source>              # shared with the team, via ainfra.yaml")
		fmt.Fprintln(w, "    ainfra add --personal <channel> <id> <source>   # just you, just this repo")
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, "Anything still undeclared will be removed by the next 'ainfra install --prune'.")
	fmt.Fprintln(w, "Backups are written to .ainfra/pruned-<timestamp>/ when it does.")
	fmt.Fprintln(w)
}

// renderOfferGroup prints one scope's offers, grouped by channel.
func renderOfferGroup(w io.Writer, offers []provider.Change) {
	byCh := map[string][]string{}
	for _, ch := range offers {
		byCh[ch.Resource.Channel] = append(byCh[ch.Resource.Channel], ch.ID)
	}
	channels := make([]string, 0, len(byCh))
	for ch := range byCh {
		channels = append(channels, ch)
	}
	sort.Strings(channels)
	for _, ch := range channels {
		ids := byCh[ch]
		sort.Strings(ids)
		fmt.Fprintf(w, "    %-10s %s\n", ch, strings.Join(ids, ", "))
	}
}

// pruneScopeNotice states what --prune cannot clear. Without it a user could
// reasonably read a clean prune as "my machine now matches the manifest", which
// is not what the feature does.
const pruneScopeNotice = "--prune covers mcpServers (in this repo), skills, and commands. " +
	"It does not touch hooks, rules, personal MCP servers in ~/.claude.json, CLI tools, background services, plugins, or marketplaces."

// hasApplySummaryContent reports whether the user-scope summary block would
// say anything beyond "Applied 0 entries". Used to skip an entirely-empty
// User-scope section that just adds noise to the output.
func hasApplySummaryContent(results []provider.ApplyResult) bool {
	for _, r := range results {
		if len(r.Applied) > 0 || len(r.Skipped) > 0 || len(r.Failed) > 0 || len(r.Warnings) > 0 {
			return true
		}
	}
	return false
}

// pluralS returns "s" when n != 1, "" otherwise. Used to avoid "Applied 1
// changes" / "Wrote 1 secrets" awkwardness in user-facing output.
func pluralS(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
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

func filterLockToRendered(l *lockfile.Lock, rendered map[string][]provider.Resource) *lockfile.Lock {
	if l == nil {
		return l
	}
	return &lockfile.Lock{
		Version:      l.Version,
		GeneratedAt:  l.GeneratedAt,
		ManifestHash: l.ManifestHash,
		Secrets:      l.Secrets,
		Entries: lockfile.Entries{
			MCPServers:         filterEntries(l.Entries.MCPServers, rendered["mcpServers"]),
			BackgroundServices: filterEntries(l.Entries.BackgroundServices, rendered["backgroundServices"]),
			Hooks:              filterEntries(l.Entries.Hooks, rendered["hooks"]),
			Commands:           filterEntries(l.Entries.Commands, rendered["commands"]),
			CLITools:           filterEntries(l.Entries.CLITools, rendered["cliTools"]),
			Skills:             filterEntries(l.Entries.Skills, rendered["skills"]),
			Marketplaces:       filterEntries(l.Entries.Marketplaces, rendered["marketplaces"]),
			Plugins:            filterEntries(l.Entries.Plugins, rendered["plugins"]),
			Rules:              filterEntries(l.Entries.Rules, rendered["rules"]),
			Tools:              filterEntries(l.Entries.Tools, rendered["tools"]),
		},
	}
}

func filterEntries(entries map[string]lockfile.Entry, resources []provider.Resource) map[string]lockfile.Entry {
	out := map[string]lockfile.Entry{}
	for _, r := range resources {
		if e, ok := entries[r.ID]; ok {
			out[r.ID] = e
		}
	}
	return out
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
		fmt.Fprintln(ctx.Stderr, c.Yellow("warning: ainfra.yaml has changed since ainfra.lock was generated — the lockfile is stale. A maintainer should refresh it with `ainfra update` and commit the result."))
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

// renderApplySummary prints the apply tally and, for any failed or skipped
// resource, a reason line. Zero-everywhere is rendered as a single "nothing
// to apply" line so empty channels don't clutter the output.
func renderApplySummary(w io.Writer, results []provider.ApplyResult) {
	var applied, skipped, failed, warned int
	for _, r := range results {
		applied += len(r.Applied)
		skipped += len(r.Skipped)
		failed += len(r.Failed)
		warned += len(r.Warnings)
	}
	switch {
	case applied == 0 && skipped == 0 && failed == 0:
		fmt.Fprintln(w, "Nothing to apply.")
	case skipped == 0 && failed == 0:
		fmt.Fprintf(w, "Applied %d change%s.\n", applied, pluralS(applied))
	default:
		fmt.Fprintf(w, "Applied %d change%s, skipped %d, failed %d.\n", applied, pluralS(applied), skipped, failed)
	}
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
	var yes, dryRun, noInstall, strict, printSchema, prune bool
	var from, agentOverride string
	return &cli.Command{
		Name:      "install",
		Summary:   "Install/update everything in ainfra.yaml (writes config files, installs CLI tools)",
		UsageLine: "ainfra install [--agent <agent>] [--yes] [--dry-run] [--strict] [--no-install] [--prune] [--from <url-or-dir>] [--print-schema]",
		Example:   "ainfra install --yes",
		SetFlags: func(fs *flag.FlagSet) {
			fs.StringVar(&agentOverride, "agent", "", "target agent for this install (claude-code, codex, claude-desktop)")
			fs.BoolVar(&yes, "yes", false, "skip confirmation prompt")
			fs.BoolVar(&prune, "prune", false, "remove config not declared in ainfra; the first run only reports what it would remove")
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
				if prune {
					// Fail rather than silently ignore: a user who passes
					// --prune and sees a clean exit would reasonably believe
					// their undeclared config had been reported.
					ui.RenderError(ctx.Stderr, ui.NewColorizer(ctx.Stderr, ctx.NoColor),
						errors.New("--prune is not supported with --from: an artifact install has no manifest to declare what to keep"))
					return 1
				}
				return runApplyFrom(ctx, from, yes)
			}
			return runApply(ctx, yes, dryRun, noInstall, strict, prune, agentOverride)
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

func runApply(ctx cli.Context, yes, dryRun, noInstall, strict, prune bool, agentOverride string) int {
	dir := ctx.Dir
	errColor := ui.NewColorizer(ctx.Stderr, ctx.NoColor)

	layers, lerr := manifest.LoadLayers(dir)
	effectiveAgent := ""
	if lerr == nil {
		effectiveAgent, _, _ = manifest.ResolveAgentWithOverride(layers, agentOverride)
	}

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

	// Render resources with Payload so providers can write file content. The
	// render resolves ainfra.yaml in memory and never rewrites the lockfiles
	// — those only change via `ainfra lock`/`update`/`add`. The resolved
	// locks carry the secret refs for the manifest as it is now, so secrets
	// added after the last lock refresh still sync below.
	rctx := resolve.NewContextFromEnv(ctx.Identity, dir, dir)
	rctx.Agent = agentOverride
	rendered, resolvedCommitted, resolvedPersonal, err := resolve.RenderResourcesAndLocksFor(dir, provider.ExecRunner{}, rctx)
	if err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}

	// Auto-emit the SessionStart staleness hook when a repo manifest exists
	// and hasn't opted out. The hook is infra plumbing, not a manifest entry —
	// it stays out of `ainfra.yaml`, the persisted lockfile, and `ainfra list`.
	// The orchestrator and applied ledger treat it like any other hook.
	injectStalenessHook(dir, rendered, merged, agentOverride)

	providers, err := providersForDir(dir, agentOverride)
	if err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}
	env := buildEnv(dir, effectiveAgent)
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
	if prune {
		orch.EnablePrune(time.Now)
	}
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
	priorUser, _ := provider.ReadAppliedUserForAgent(effectiveAgent)
	userLedgerNonEmpty := priorUser != nil && hasAnyEntry(priorUser)
	if herr != nil && prune {
		// Refuse rather than prune repo scope alone: a partial prune the user
		// did not ask for is worse than none.
		ui.RenderError(ctx.Stderr, errColor,
			fmt.Errorf("--prune needs your home directory to reconcile user-scope config: %w", herr))
		return 1
	}
	// --prune always constructs the user-scope orchestrator. Otherwise it is
	// built only when personal entries or a non-empty user ledger exist, which
	// is exactly backwards for prune: a user with nothing declared has the most
	// undeclared config in ~/.claude, and would silently get no user-scope pass
	// at all.
	if herr == nil && (anyResources(userRendered) || userLedgerNonEmpty || prune) {
		userEnv := env
		userEnv.Root = home
		userEnv.UserScope = true
		userOrch = provider.NewOrchestratorScoped(home, provider.ScopeUser, userEnv, providers)
		if prune {
			userOrch.EnablePrune(time.Now)
		}
		userPlans, err = userOrch.PlanAllRendered(userRendered)
		if err != nil {
			ui.RenderError(ctx.Stderr, errColor, err)
			return 1
		}
	}

	// Report undeclared config before the "nothing to do" check below. Newly
	// offered entries are deliberately absent from the plan — prune never
	// deletes on first sight — so an empty plan is exactly the case where the
	// user most needs to see this list.
	var offered []provider.Change
	if prune {
		repoOffers := orch.NewlyOffered()
		var userOffers []provider.Change
		if userOrch != nil {
			userOffers = userOrch.NewlyOffered()
		}
		offered = append(append([]provider.Change{}, repoOffers...), userOffers...)

		c := ui.NewColorizer(ctx.Stdout, ctx.NoColor)
		if orch.OfferedLedgerCorrupt() || (userOrch != nil && userOrch.OfferedLedgerCorrupt()) {
			fmt.Fprintln(ctx.Stderr, ui.NewColorizer(ctx.Stderr, ctx.NoColor).Yellow(
				"warning: the offered ledger was unreadable, so every undeclared entry has been reported again and nothing was removed."))
		}
		renderOffered(ctx.Stdout, c, repoOffers, userOffers)
		fmt.Fprintln(ctx.Stdout, c.Dim(pruneScopeNotice))
		fmt.Fprintln(ctx.Stdout)
	}

	// Check if there is anything to do.
	allEmpty := plansEmpty(plans) && plansEmpty(userPlans)
	if allEmpty {
		if len(offered) == 0 {
			fmt.Fprintln(ctx.Stdout, "Nothing to do — your environment already matches ainfra.yaml.")
		}
		// Record the offers even though there is nothing to apply. Without
		// this they would be re-offered on every run and never arm, so prune
		// could never remove anything.
		if err := orch.RecordOffered(); err != nil {
			ui.RenderError(ctx.Stderr, errColor, err)
			return 1
		}
		if userOrch != nil {
			if err := userOrch.RecordOffered(); err != nil {
				ui.RenderError(ctx.Stderr, errColor, err)
				return 1
			}
		}
		if dryRun {
			return 0
		}
		// A secret value can rotate in its backend without any config drift,
		// so a no-op install still re-materializes secrets. A backend that is
		// not ready (offline laptop, vault signed out) degrades to a warning —
		// it must not fail an otherwise clean install.
		if failures := preflightSecretBackends(dir, secret.DefaultRegistry(), resolvedCommitted, resolvedPersonal); len(failures) > 0 {
			c := ui.NewColorizer(ctx.Stderr, ctx.NoColor)
			fmt.Fprintln(ctx.Stderr, c.Yellow("warning: secrets were not refreshed — backend not ready:"))
			for _, f := range failures {
				fmt.Fprintf(ctx.Stderr, "  %s\n", f)
			}
			return 0
		}
		res, serr := syncSecrets(dir, secret.DefaultRegistry(), resolvedCommitted, resolvedPersonal)
		if serr != nil {
			ui.RenderError(ctx.Stderr, errColor, serr)
			return 1
		}
		renderSyncResult(ctx.Stdout, res)
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

	// Verify the credential backends the manifest's secrets resolve through
	// (e.g. the 1Password CLI) are ready before writing anything. Skipped on a
	// dry run, which never resolves secrets — so `install --dry-run --strict`
	// stays usable in CI where no vault is signed in.
	if !dryRun {
		if failures := preflightSecretBackends(dir, secret.DefaultRegistry(), resolvedCommitted, resolvedPersonal); len(failures) > 0 {
			fmt.Fprintln(ctx.Stderr, "Secret backend not ready — fix this before applying:")
			for _, f := range failures {
				fmt.Fprintf(ctx.Stderr, "  %s\n", f)
			}
			return 1
		}
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
	repoLock = filterLockToRendered(repoLock, repoRendered)
	userLock = filterLockToRendered(userLock, userRendered)

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
		if !dryRun && hasApplySummaryContent(userResults) {
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
	appendApplyHistory(dir, "apply", effectiveAgent, merged.ManifestHash, results, ctx.Stderr)

	// Final step: resolve the manifest's secrets and write them into the
	// Claude Code settings env block, so a normally-launched Claude has them.
	// This makes `ainfra apply` a complete setup — config and credentials.
	res, serr := syncSecrets(dir, secret.DefaultRegistry(), resolvedCommitted, resolvedPersonal)
	if serr != nil {
		ui.RenderError(ctx.Stderr, errColor, serr)
		return 1
	}
	renderSyncResult(ctx.Stdout, res)
	fmt.Fprintln(ctx.Stdout, "Done — your environment now matches ainfra.yaml.")
	return 0
}

// renderSyncResult reports what syncSecrets wrote.
func renderSyncResult(w io.Writer, res syncResult) {
	if res.EnvCount > 0 {
		fmt.Fprintf(w, "Wrote %d secret%s into %s.\n", res.EnvCount, pluralS(res.EnvCount), res.SettingsPath)
		if res.ShimPath != "" {
			fmt.Fprintf(w, "Launcher shim %s injects them fresh at every launch.\n", res.ShimPath)
		}
	}
	for _, f := range res.Files {
		fmt.Fprintf(w, "Wrote credential file %s\n", f)
	}
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
