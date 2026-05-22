package main

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"github.com/MHilhorst/ainfra/internal/cli"
	"github.com/MHilhorst/ainfra/internal/lockfile"
	"github.com/MHilhorst/ainfra/internal/secret"
	"github.com/MHilhorst/ainfra/internal/ui"
)

// execFunc replaces the current process with another. syscall.Exec in
// production; a recording stub in tests.
type execFunc func(argv0 string, argv []string, envv []string) error

// newExecCommand returns the `ainfra exec` command: it resolves every secret
// reference in the lockfile and runs a command with the values in its
// environment. No secret value is written to disk.
func newExecCommand() *cli.Command {
	return &cli.Command{
		Name:      "exec",
		Summary:   "Resolve secrets and run a command with them in its environment",
		UsageLine: "ainfra exec [-- <command> [args...]]",
		Example:   "ainfra exec -- claude",
		Run: func(ctx cli.Context) int {
			return runExecWith(ctx, secret.DefaultRegistry(), syscall.Exec)
		},
	}
}

// runExecWith is the testable core of `ainfra exec`.
func runExecWith(ctx cli.Context, reg *secret.Registry, execFn execFunc) int {
	errColor := ui.NewColorizer(ctx.Stderr, ctx.NoColor)

	argv := ctx.Args
	if len(argv) == 0 {
		argv = []string{"claude"}
	}

	lockPath := filepath.Join(ctx.Dir, "ainfra.lock")
	if !fileExists(lockPath) {
		ui.RenderError(ctx.Stderr, errColor, fmt.Errorf("ainfra.lock not found — run `ainfra lock` first"))
		return 1
	}
	committed, err := lockfile.Read(lockPath)
	if err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}
	personal, err := lockfile.Read(filepath.Join(ctx.Dir, "ainfra.personal.lock"))
	if err != nil {
		personal = &lockfile.Lock{}
	}

	// The secret set is the union of both lockfiles.
	refs := map[string]lockfile.SecretRef{}
	maps.Copy(refs, committed.Secrets)
	maps.Copy(refs, personal.Secrets)

	// Resolve every ref, collecting all failures before aborting.
	resolved := map[string]string{}
	var failures []string
	for _, v := range slices.Sorted(maps.Keys(refs)) {
		sr := refs[v]
		val, err := reg.Resolve(expandUser(sr.Ref))
		if err != nil {
			failures = append(failures, "  "+err.Error())
			continue
		}
		resolved[sr.Var] = val
	}
	if len(failures) > 0 {
		fmt.Fprintln(ctx.Stderr, "Could not resolve secrets:")
		for _, f := range failures {
			fmt.Fprintln(ctx.Stderr, f)
		}
		return 1
	}

	// Build the child environment: the current env plus one var per secret.
	envv := os.Environ()
	for _, v := range slices.Sorted(maps.Keys(resolved)) {
		envv = append(envv, v+"="+resolved[v])
	}

	bin, err := exec.LookPath(argv[0])
	if err != nil {
		ui.RenderError(ctx.Stderr, errColor, fmt.Errorf("command not found: %s", argv[0]))
		return 1
	}
	if err := execFn(bin, argv, envv); err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}
	return 0 // unreachable on success: syscall.Exec replaces this process
}

// expandUser substitutes ${user} in a personal-scope ref with the OS username.
func expandUser(ref string) string {
	if !strings.Contains(ref, "${user}") {
		return ref
	}
	name := os.Getenv("USER")
	if u, err := user.Current(); err == nil && u.Username != "" {
		name = u.Username
	}
	return strings.ReplaceAll(ref, "${user}", name)
}
