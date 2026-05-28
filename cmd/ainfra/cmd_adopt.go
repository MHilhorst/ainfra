package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/MHilhorst/ainfra/internal/adopt"
	"github.com/MHilhorst/ainfra/internal/cli"
	"github.com/MHilhorst/ainfra/internal/diag"
	"github.com/MHilhorst/ainfra/internal/manifest"
	"github.com/MHilhorst/ainfra/internal/ui"
)

// newAdoptCommand scans an existing repo's .mcp.json, .claude/, and CLAUDE.md
// and emits a draft ainfra.yaml. It is the brownfield onramp: a one-shot import
// for teams who already have a Claude Code setup committed to git.
//
// With --scope=user, it instead scans the user's ~/.claude/ tree and emits the
// global personal manifest at $XDG_CONFIG_HOME/ainfra/personal.yaml. This is
// the migration path for developers with an existing ~/.claude/ setup who
// want to start managing their cross-repo personal layer through ainfra.
func newAdoptCommand() *cli.Command {
	var force, merge bool
	var scope string
	return &cli.Command{
		Name:      "adopt",
		Summary:   "Generate ainfra.yaml (or personal.yaml) from existing Claude Code config",
		UsageLine: "ainfra adopt [--scope=repo|user] [--force | --merge]",
		Example:   "ainfra adopt --scope=user",
		SetFlags: func(fs *flag.FlagSet) {
			fs.BoolVar(&force, "force", false, "overwrite an existing manifest")
			fs.BoolVar(&merge, "merge", false, "add new entries to an existing manifest without overwriting existing keys")
			fs.StringVar(&scope, "scope", "repo", "which manifest to emit: 'repo' (./ainfra.yaml) or 'user' ($XDG_CONFIG_HOME/ainfra/personal.yaml)")
		},
		Run: func(ctx cli.Context) int { return runAdopt(ctx, scope, force, merge) },
	}
}

func runAdopt(ctx cli.Context, scope string, force, merge bool) int {
	errColor := ui.NewColorizer(ctx.Stderr, ctx.NoColor)

	if scope == "" {
		scope = "repo"
	}
	if scope != "repo" && scope != "user" {
		ui.RenderError(ctx.Stderr, errColor, &diag.Diagnostic{
			Summary: "adopt: invalid --scope " + scope,
			Hint:    "Use --scope=repo (default) or --scope=user.",
		})
		return 1
	}

	var (
		path   string
		layout adopt.Layout
	)
	if scope == "user" {
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			ui.RenderError(ctx.Stderr, errColor, &diag.Diagnostic{
				Summary: "adopt --scope=user: cannot resolve home directory",
				Hint:    "Set HOME (or XDG_CONFIG_HOME) and re-run.",
			})
			return 1
		}
		path = manifest.GlobalPersonalPath()
		if path == "" {
			ui.RenderError(ctx.Stderr, errColor, &diag.Diagnostic{
				Summary: "adopt --scope=user: cannot resolve personal manifest path",
				Hint:    "Set XDG_CONFIG_HOME or HOME and re-run.",
			})
			return 1
		}
		layout = adopt.UserLayout(home)
	} else {
		path = filepath.Join(ctx.Dir, "ainfra.yaml")
		layout = adopt.RepoLayout(ctx.Dir)
	}

	if force && merge {
		ui.RenderError(ctx.Stderr, errColor, &diag.Diagnostic{
			Summary: "adopt: --force and --merge are mutually exclusive",
			Hint:    "Pick one: --force overwrites; --merge adds only new entries.",
		})
		return 1
	}

	existingPresent := false
	if _, err := os.Stat(path); err == nil {
		existingPresent = true
	}
	if existingPresent && !force && !merge {
		ui.RenderError(ctx.Stderr, errColor, &diag.Diagnostic{
			Summary: "adopt: " + filepath.Base(path) + " exists",
			Hint:    "Use --merge to add new entries or --force to overwrite.",
		})
		return 1
	}

	scanned, warnings, err := adopt.ScanLayout(layout)
	if err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}

	final := scanned
	if merge && existingPresent {
		existing, err := loadExistingForMerge(ctx.Dir, scope)
		if err != nil {
			ui.RenderError(ctx.Stderr, errColor, err)
			return 1
		}
		if existing == nil {
			ui.RenderError(ctx.Stderr, errColor, fmt.Errorf("adopt: --merge: could not load existing %s", filepath.Base(path)))
			return 1
		}
		merged, mws := mergeAdopt(*existing, scanned)
		final = merged
		warnings = append(warnings, mws...)
	}

	out, err := adopt.Emit(final)
	if err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}

	c := ui.NewColorizer(ctx.Stdout, ctx.NoColor)
	errC := ui.NewColorizer(ctx.Stderr, ctx.NoColor)

	fmt.Fprintln(ctx.Stdout, "ainfra: wrote "+path)
	strippedCount := printAdoptWarnings(ctx.Stderr, errC, warnings)

	base := filepath.Base(path)
	switch {
	case strippedCount > 0:
		ui.Next(ctx.Stdout, c, fmt.Sprintf("open %s, replace %d TODO secret ref(s) under 'secrets:', then run 'ainfra validate'.", base, strippedCount))
	case scope == "user":
		ui.Next(ctx.Stdout, c, "review "+base+"; entries declared here install to ~/.claude/. For team-shared tooling prefer a team manifest via extends:.")
	default:
		ui.Next(ctx.Stdout, c, "review ainfra.yaml, then run 'ainfra validate', 'ainfra lock', and 'ainfra plan'.")
	}
	return 0
}

// printAdoptWarnings groups adopt warnings by Kind and writes a structured
// summary to w. Returns the number of stripped-credential warnings so the
// caller can adapt the "Next:" hint.
//
// The intent is to give a developer who has never seen this CLI before a clear
// answer to three questions per section: what happened, why it matters, and
// what they have to do next.
func printAdoptWarnings(w io.Writer, c ui.Colorizer, warnings []adopt.Warning) int {
	if len(warnings) == 0 {
		return 0
	}
	var stripped, merged, review []adopt.Warning
	for _, x := range warnings {
		switch x.Kind {
		case adopt.WarnStripped:
			stripped = append(stripped, x)
		case adopt.WarnMergeAdd:
			merged = append(merged, x)
		default:
			review = append(review, x)
		}
	}

	if len(stripped) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, c.Bold(c.Yellow(fmt.Sprintf("Secrets stripped (%d)", len(stripped)))))
		fmt.Fprintln(w, c.Dim("  Your config had plaintext credentials sitting inside it. We rewrote"))
		fmt.Fprintln(w, c.Dim("  ainfra.yaml so they live as ${secrets.<name>} references instead."))
		fmt.Fprintln(w, c.Dim("  Before committing, point each one at where the value actually lives"))
		fmt.Fprintln(w, c.Dim("  (1Password op://, env var, file://, ...). The literals are NOT in the"))
		fmt.Fprintln(w, c.Dim("  manifest — they were stripped literal credential values, removed at scan time."))
		fmt.Fprintln(w)
		width := 0
		for _, x := range stripped {
			if len(x.Target) > width {
				width = len(x.Target)
			}
		}
		for _, x := range stripped {
			pad := strings.Repeat(" ", width-len(x.Target))
			fmt.Fprintf(w, "  %s %s%s  %s %s\n",
				c.Yellow("!"),
				c.Bold(x.Target),
				pad,
				c.Dim("<- was"),
				c.Dim(x.Origin),
			)
		}
	}

	if len(merged) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, c.Bold(fmt.Sprintf("Merged new entries (%d)", len(merged))))
		fmt.Fprintln(w, c.Dim("  These keys weren't in your existing manifest, so adopt added them."))
		fmt.Fprintln(w, c.Dim("  Nothing to fix — listed for awareness."))
		fmt.Fprintln(w)
		for _, x := range merged {
			fmt.Fprintf(w, "  %s %s\n", c.Green("+"), x.Message)
		}
	}

	if len(review) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, c.Bold(fmt.Sprintf("Review manually (%d)", len(review))))
		fmt.Fprintln(w, c.Dim("  Adopt couldn't fully ingest these. Skim them once and decide if you"))
		fmt.Fprintln(w, c.Dim("  need to add anything to ainfra.yaml by hand."))
		fmt.Fprintln(w)
		for _, x := range review {
			fmt.Fprintf(w, "  %s %s\n", c.Dim("-"), strings.TrimPrefix(x.Message, "adopt: "))
		}
	}
	return len(stripped)
}

// loadExistingForMerge returns the manifest that --merge should fold the scan
// result into. For repo scope this is the repo's ainfra.yaml; for user scope
// it is the global personal manifest at $XDG_CONFIG_HOME/ainfra/personal.yaml.
func loadExistingForMerge(dir, scope string) (*manifest.Manifest, error) {
	if scope == "user" {
		return manifest.LoadGlobalPersonal()
	}
	layers, err := manifest.LoadLayers(dir)
	if err != nil {
		return nil, err
	}
	return layers[manifest.LayerRepo], nil
}

// mergeAdopt overlays scanned onto existing: existing keys win, new keys from
// scanned land. Returns one warning per newly-added entry per channel.
func mergeAdopt(existing, scanned manifest.Manifest) (manifest.Manifest, []adopt.Warning) {
	out := existing
	var warnings []adopt.Warning

	out.MCPServers, warnings = addNewMCP(out.MCPServers, scanned.MCPServers, "mcpServers", warnings)
	out.Secrets, warnings = addNewSecrets(out.Secrets, scanned.Secrets, "secrets", warnings)
	out.Hooks, warnings = addNewHooks(out.Hooks, scanned.Hooks, "hooks", warnings)
	out.Commands, warnings = addNewCommands(out.Commands, scanned.Commands, "commands", warnings)
	out.Rules, warnings = addNewRules(out.Rules, scanned.Rules, "rules", warnings)

	return out, warnings
}

func addNewMCP(dst, src map[string]manifest.MCPServer, channel string, ws []adopt.Warning) (map[string]manifest.MCPServer, []adopt.Warning) {
	if dst == nil {
		dst = map[string]manifest.MCPServer{}
	}
	for k, v := range src {
		if _, ok := dst[k]; ok {
			continue
		}
		dst[k] = v
		ws = append(ws, adopt.Warning{Kind: adopt.WarnMergeAdd, Target: channel + "." + k, Message: fmt.Sprintf("adding %s.%s", channel, k)})
	}
	if len(dst) == 0 {
		return nil, ws
	}
	return dst, ws
}

func addNewSecrets(dst, src map[string]manifest.Secret, channel string, ws []adopt.Warning) (map[string]manifest.Secret, []adopt.Warning) {
	if dst == nil {
		dst = map[string]manifest.Secret{}
	}
	for k, v := range src {
		if _, ok := dst[k]; ok {
			continue
		}
		dst[k] = v
		ws = append(ws, adopt.Warning{Kind: adopt.WarnMergeAdd, Target: channel + "." + k, Message: fmt.Sprintf("adding %s.%s", channel, k)})
	}
	if len(dst) == 0 {
		return nil, ws
	}
	return dst, ws
}

func addNewHooks(dst, src map[string]manifest.Hook, channel string, ws []adopt.Warning) (map[string]manifest.Hook, []adopt.Warning) {
	if dst == nil {
		dst = map[string]manifest.Hook{}
	}
	for k, v := range src {
		if _, ok := dst[k]; ok {
			continue
		}
		dst[k] = v
		ws = append(ws, adopt.Warning{Kind: adopt.WarnMergeAdd, Target: channel + "." + k, Message: fmt.Sprintf("adding %s.%s", channel, k)})
	}
	if len(dst) == 0 {
		return nil, ws
	}
	return dst, ws
}

func addNewCommands(dst, src map[string]manifest.Command, channel string, ws []adopt.Warning) (map[string]manifest.Command, []adopt.Warning) {
	if dst == nil {
		dst = map[string]manifest.Command{}
	}
	for k, v := range src {
		if _, ok := dst[k]; ok {
			continue
		}
		dst[k] = v
		ws = append(ws, adopt.Warning{Kind: adopt.WarnMergeAdd, Target: channel + "." + k, Message: fmt.Sprintf("adding %s.%s", channel, k)})
	}
	if len(dst) == 0 {
		return nil, ws
	}
	return dst, ws
}

func addNewRules(dst, src map[string]manifest.Rule, channel string, ws []adopt.Warning) (map[string]manifest.Rule, []adopt.Warning) {
	if dst == nil {
		dst = map[string]manifest.Rule{}
	}
	for k, v := range src {
		if _, ok := dst[k]; ok {
			continue
		}
		dst[k] = v
		ws = append(ws, adopt.Warning{Kind: adopt.WarnMergeAdd, Target: channel + "." + k, Message: fmt.Sprintf("adding %s.%s", channel, k)})
	}
	if len(dst) == 0 {
		return nil, ws
	}
	return dst, ws
}
