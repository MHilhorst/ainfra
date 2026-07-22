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
	// A shim baked against a since-deleted git worktree would otherwise drop
	// every secret on every launch, machine-wide, until someone reinstalled.
	// Recover the repo that worktree belonged to — and only that, never a
	// manifest merely found nearby.
	dir := ctx.Dir
	if !hasManifest(dir) {
		if recovered, ok := recoveredWorktreeRepo(dir); ok {
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

// worktreeContainers are the directory layouts that hold linked worktrees:
// <repo>/.claude/worktrees/<name> and <repo>/.worktrees/<name>. Both are in
// active use by the repos this shim serves.
var worktreeContainers = [][]string{
	{".claude", "worktrees"},
	{".worktrees"},
}

// recoveredWorktreeRepo maps a dead shim path back to the repo it was a
// worktree of, returning false unless that relationship is provable from the
// path's own shape.
//
// This exists for one specific accident: a shim baked against
// <repo>/.claude/worktrees/<name> keeps pointing there after the worktree is
// removed. Recovering <repo> is safe because the dead path demonstrably
// belonged to it.
//
// It is deliberately NOT a search. An earlier cut walked up to the nearest
// ancestor holding a manifest, which meant a stale or mistyped --chdir
// anywhere under a monorepo, a shared checkout, or any parent dir carrying an
// ainfra.yaml would resolve THAT project's credentials into the child. Secret
// injection has to be earned: dir must sit exactly one segment below a known
// worktree container, and the repo above it must hold a manifest, be a git
// checkout root, and not be the home directory. Anything else returns false
// and the caller launches with no secrets, exactly as it did before recovery
// existed.
func recoveredWorktreeRepo(dir string) (string, bool) {
	// filepath.Dir is lexical, so a relative dir would bottom out at "."
	// (the process cwd) rather than at its real ancestors.
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", false
	}
	// Drop <name>, then the container segments, innermost first.
	repo := filepath.Dir(abs)
	for _, container := range worktreeContainers {
		candidate := repo
		matched := true
		for i := len(container) - 1; i >= 0; i-- {
			if filepath.Base(candidate) != container[i] {
				matched = false
				break
			}
			candidate = filepath.Dir(candidate)
		}
		if !matched {
			continue
		}
		if isRecoverableRepo(candidate) {
			return candidate, true
		}
	}
	return "", false
}

// isRecoverableRepo reports whether dir is a git checkout root holding a
// manifest, and is not the home directory — the one ancestor nearly every
// path shares, and so never a defensible source of secrets by inference.
func isRecoverableRepo(dir string) bool {
	if home, err := os.UserHomeDir(); err == nil && home != "" && dir == home {
		return false
	}
	if !hasManifest(dir) {
		return false
	}
	// Both spellings count: a .git directory in a normal clone, and a .git
	// file in a worktree or a --separate-git-dir clone.
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
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
