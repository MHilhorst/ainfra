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
	"github.com/MHilhorst/ainfra/internal/resolve"
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
		// Everything after `exec` belongs to the child command, including its
		// flags: `ainfra exec claude --no-color` passes --no-color to claude.
		// exec registers no flags of its own, so a token here is never a
		// dropped ainfra flag.
		SubParsesArgs: func([]string) bool { return true },
		Run:           runExec,
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
	// A shim baked against a since-deleted directory (typically a git worktree
	// the install ran from) would otherwise drop every secret on every launch,
	// machine-wide, until someone reinstalled. Recover by walking up to the
	// nearest enclosing manifest instead.
	dir := ctx.Dir
	if !hasManifest(dir) {
		if recovered, ok := nearestManifestDir(dir); ok {
			warn(fmt.Sprintf("no ainfra manifest in %s — using %s instead (stale launcher shim; run `ainfra install` there to repoint it)", dir, recovered))
			dir = recovered
		} else {
			warn(fmt.Sprintf("no ainfra manifest in %s — launching without secret injection", dir))
		}
	}
	committed, err := lockfile.Read(filepath.Join(dir, "ainfra.lock"))
	if err != nil {
		warn(fmt.Sprintf("unreadable ainfra.lock in %s — launching without secret injection", dir))
		committed = &lockfile.Lock{}
	}
	personal, err := lockfile.Read(filepath.Join(dir, "ainfra.personal.lock"))
	if err != nil {
		personal = &lockfile.Lock{}
	}
	// Identity-gated: a secret scoped to identities the caller is not in is
	// skipped rather than attempted. On a headless box that is the difference
	// between a clean launch and a per-human vault miss warned about on every
	// single run.
	rctx := resolve.NewContextFromEnv(ctx.Identity, dir, dir)
	resolved, failures := resolveSecretEnvFor(dir, secret.DefaultRegistry(), committed, personal, rctx)
	for _, f := range failures {
		warn(strings.TrimSpace(f) + " — launching without it")
	}

	bin, err := lookPathSkippingShims(ctx.Args[0])
	if err != nil {
		ui.RenderError(ctx.Stderr, errColor, fmt.Errorf("exec: %v", err))
		return 127
	}
	// The child's PATH loses the shim dir. Third-party claude wrappers (e.g.
	// cmux's) resolve the "real" binary by scanning PATH skipping only their
	// own dir — with the shim dir still present, the shim and such a wrapper
	// resolve each other in an infinite loop. Secrets are already injected,
	// so nested launches don't need the shim again.
	env := stripShimDirFromPath(mergeEnv(os.Environ(), resolved))
	if err := execFn(bin, ctx.Args, env); err != nil {
		ui.RenderError(ctx.Stderr, errColor, fmt.Errorf("exec %s: %v", bin, err))
		return 126
	}
	return 0
}

// hasManifest reports whether dir is a manifest dir — either file is enough,
// matching what runExec goes on to read.
func hasManifest(dir string) bool {
	return fileExists(filepath.Join(dir, "ainfra.lock")) || fileExists(filepath.Join(dir, "ainfra.yaml"))
}

// nearestManifestDir walks up from dir looking for an enclosing manifest,
// returning false at the filesystem root.
//
// dir itself usually does not exist here — that is the case worth recovering.
// A shim pinned to <repo>/.claude/worktrees/<name> resolves back to <repo>
// after that worktree is removed, so secrets keep flowing from the same
// manifest the dead worktree was a checkout of.
func nearestManifestDir(dir string) (string, bool) {
	for {
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
		if hasManifest(dir) {
			return dir, true
		}
	}
}

// stripShimDirFromPath removes ainfra's shim dir from the PATH entry of an
// environment slice. See runExec for why.
func stripShimDirFromPath(env []string) []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return env
	}
	shimDir := filepath.Join(home, ".config", "ainfra", "bin")
	for i, kv := range env {
		if !strings.HasPrefix(kv, "PATH=") {
			continue
		}
		var kept []string
		for _, d := range filepath.SplitList(kv[len("PATH="):]) {
			if d != shimDir {
				kept = append(kept, d)
			}
		}
		env[i] = "PATH=" + strings.Join(kept, string(os.PathListSeparator))
	}
	return env
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
