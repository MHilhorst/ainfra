package main

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"github.com/MHilhorst/ainfra/internal/cli"
	"github.com/MHilhorst/ainfra/internal/lockfile"
	"github.com/MHilhorst/ainfra/internal/secret"
	"github.com/MHilhorst/ainfra/internal/ui"
)

// execFn replaces the current process with the target command. Swapped in
// tests — syscall.Exec would replace the test binary itself.
var execFn = syscall.Exec

// newExecCommand runs a command with the manifest's secrets resolved into its
// process environment. It is the runtime half of secret delivery: `install`
// persists secrets for at-rest consumers (settings env block, credential
// files), `exec` injects them fresh at launch — so a rotated secret reaches
// the next launch without re-running install, and no credential values live
// in shell config. The `claude` launcher shim written by `ainfra install`
// calls this.
func newExecCommand() *cli.Command {
	return &cli.Command{
		Name:      "exec",
		Summary:   "Run a command with managed secrets in its process environment",
		UsageLine: "ainfra [--chdir <manifest-dir>] exec -- <command> [args...]",
		Example:   "ainfra --chdir ~/projects/claude-config exec -- claude",
		// Hidden: launcher-shim plumbing, not a front-page verb. `ainfra help
		// exec` still documents it.
		Hidden: true,
		Run:    runExec,
	}
}

func runExec(ctx cli.Context) int {
	errColor := ui.NewColorizer(ctx.Stderr, ctx.NoColor)
	if len(ctx.Args) == 0 {
		ui.RenderError(ctx.Stderr, errColor, fmt.Errorf("exec: no command given — usage: ainfra exec -- <command> [args...]"))
		return 2
	}
	warn := func(msg string) {
		fmt.Fprintln(ctx.Stderr, errColor.Yellow("ainfra exec: "+msg))
	}

	// Secret resolution is fail-open: a launch must never be blocked by an
	// unreachable backend (offline laptop, locked vault, deleted repo).
	// Whatever resolves is injected; the rest is a warning, and the settings
	// env block written by `ainfra install` remains the at-rest fallback.
	if !fileExists(filepath.Join(ctx.Dir, "ainfra.lock")) && !fileExists(filepath.Join(ctx.Dir, "ainfra.yaml")) {
		warn(fmt.Sprintf("no ainfra manifest in %s — launching without secret injection", ctx.Dir))
	}
	committed, err := lockfile.Read(filepath.Join(ctx.Dir, "ainfra.lock"))
	if err != nil {
		warn(fmt.Sprintf("unreadable ainfra.lock in %s — launching without secret injection", ctx.Dir))
		committed = &lockfile.Lock{}
	}
	personal, err := lockfile.Read(filepath.Join(ctx.Dir, "ainfra.personal.lock"))
	if err != nil {
		personal = &lockfile.Lock{}
	}
	resolved, failures := resolveSecretEnv(ctx.Dir, secret.DefaultRegistry(), committed, personal)
	for _, f := range failures {
		warn(strings.TrimSpace(f) + " — launching without it")
	}

	bin, err := lookPathSkippingShims(ctx.Args[0])
	if err != nil {
		ui.RenderError(ctx.Stderr, errColor, fmt.Errorf("exec: %v", err))
		return 127
	}
	if err := execFn(bin, ctx.Args, mergeEnv(os.Environ(), resolved)); err != nil {
		ui.RenderError(ctx.Stderr, errColor, fmt.Errorf("exec %s: %v", bin, err))
		return 126
	}
	return 0
}

// mergeEnv returns base with every resolved secret set. A freshly-resolved
// value replaces an existing entry of the same name, so a rotated secret
// reaches the child even when a stale value is still exported somewhere.
func mergeEnv(base []string, resolved map[string]string) []string {
	if len(resolved) == 0 {
		return base
	}
	out := make([]string, 0, len(base)+len(resolved))
	for _, kv := range base {
		if eq := strings.IndexByte(kv, '='); eq > 0 {
			if _, ok := resolved[kv[:eq]]; ok {
				continue
			}
		}
		out = append(out, kv)
	}
	for _, k := range slices.Sorted(maps.Keys(resolved)) {
		out = append(out, k+"="+resolved[k])
	}
	return out
}

// lookPathSkippingShims resolves name against PATH, skipping ainfra's own
// shim directory — the `claude` shim re-enters `ainfra exec`, which must find
// the real binary instead of the shim. A name containing a path separator
// bypasses PATH entirely, like the shell would.
func lookPathSkippingShims(name string) (string, error) {
	if strings.ContainsRune(name, '/') {
		return exec.LookPath(name)
	}
	shimDir := ""
	if home, err := os.UserHomeDir(); err == nil {
		shimDir = filepath.Join(home, ".config", "ainfra", "bin")
	}
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if dir == "" || (shimDir != "" && dir == shimDir) {
			continue
		}
		cand := filepath.Join(dir, name)
		info, err := os.Stat(cand)
		if err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
			continue
		}
		return cand, nil
	}
	return "", fmt.Errorf("%s: command not found on PATH (shim dir is skipped)", name)
}
