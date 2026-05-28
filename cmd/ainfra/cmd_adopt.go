package main

import (
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

// runAdopt powers `ainfra init --adopt`: scans the current repo's .claude/
// tree and emits a draft ainfra.yaml. It is a function rather than a separate
// command because the `adopt` verb was folded into `init`.
func runAdopt(ctx cli.Context, force, merge bool) int {
	errColor := ui.NewColorizer(ctx.Stderr, ctx.NoColor)

	path := filepath.Join(ctx.Dir, "ainfra.yaml")
	layout := adopt.RepoLayout(ctx.Dir)

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
		existing, err := loadExistingForMerge(ctx.Dir)
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
	if strippedCount > 0 {
		ui.Next(ctx.Stdout, c, fmt.Sprintf("open %s, replace %d TODO secret ref(s) under 'secrets:', then run 'ainfra validate'.", base, strippedCount))
	} else {
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

// loadExistingForMerge returns the repo manifest that --merge should fold the
// scan result into.
func loadExistingForMerge(dir string) (*manifest.Manifest, error) {
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
