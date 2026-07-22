package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitRepo turns dir into a git repo with one commit, so linked worktrees can
// be added to it. Skips the test when git is unavailable.
func gitRepo(t *testing.T, dir string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "test"},
		{"add", "-A"},
		{"commit", "-qm", "init"},
	} {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

// gitAddWorktree creates a linked worktree of dir at path on a new branch.
func gitAddWorktree(t *testing.T, dir, path, branch string) {
	t.Helper()
	cmd := exec.Command("git", "-C", dir, "worktree", "add", "-q", "-b", branch, path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git worktree add: %v\n%s", err, out)
	}
}

// mkdirs creates every dir, failing the test on error.
func mkdirs(t *testing.T, dirs ...string) {
	t.Helper()
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

// shimText returns the generated claude shim's contents.
func shimText(t *testing.T, home string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(home, ".config", "ainfra", "bin", "claude"))
	if err != nil {
		t.Fatalf("shim not written: %v", err)
	}
	return string(raw)
}

// bakedDir asserts the shim's --chdir argument is want.
func assertBaked(t *testing.T, shim, want string) {
	t.Helper()
	if !strings.Contains(shim, `--chdir "`+want+`" exec`) {
		t.Errorf("shim --chdir is not %q, got:\n%s", want, shim)
	}
}

// The shim is per-machine but a worktree is per-task. When the two checkouts
// agree on every manifest input, installing from a worktree must bake the main
// worktree's path, or deleting that worktree silently drops secret injection
// from every future claude launch.
func TestWriteLauncherShimRedirectsOutOfWorktree(t *testing.T) {
	root := t.TempDir()
	repo, home := filepath.Join(root, "repo"), filepath.Join(root, "home")
	mkdirs(t, repo, home)
	writeSecretFixture(t, repo)
	gitRepo(t, repo)

	wt := filepath.Join(repo, ".claude", "worktrees", "throwaway")
	gitAddWorktree(t, repo, wt, "task")

	_, note, err := writeLauncherShim(home, wt)
	if err != nil {
		t.Fatal(err)
	}
	// EvalSymlinks: on macOS t.TempDir() lives under /var, a symlink to
	// /private/var, and git reports the resolved form.
	wantDir, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	assertBaked(t, shimText(t, home), wantDir)
	// A redirect is never silent: the shim now resolves from a different dir
	// than this install did, and only this line makes that visible.
	if !strings.Contains(note, wantDir) {
		t.Errorf("redirect was not reported to the user, note = %q", note)
	}
}

// Installing from the main worktree, or from a plain non-git directory, must
// leave the baked path exactly as given — the redirect targets linked
// worktrees only.
func TestWriteLauncherShimKeepsDurableDirs(t *testing.T) {
	for _, tc := range []struct {
		name string
		git  bool
	}{
		{"main worktree", true},
		{"not a git repo", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			repo, home := filepath.Join(root, "repo"), filepath.Join(root, "home")
			mkdirs(t, repo, home)
			writeSecretFixture(t, repo)
			if tc.git {
				gitRepo(t, repo)
			}

			if _, _, err := writeLauncherShim(home, repo); err != nil {
				t.Fatal(err)
			}
			assertBaked(t, shimText(t, home), repo)
		})
	}
}

// A worktree whose main checkout has no manifest at all must not be
// redirected: a stale-but-present path beats a confidently wrong one.
func TestWriteLauncherShimKeepsWorktreeWhenMainHasNoManifest(t *testing.T) {
	root := t.TempDir()
	repo, home := filepath.Join(root, "repo"), filepath.Join(root, "home")
	mkdirs(t, repo, home)
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRepo(t, repo)

	wt := filepath.Join(root, "wt")
	gitAddWorktree(t, repo, wt, "task")
	writeSecretFixture(t, wt)

	if _, _, err := writeLauncherShim(home, wt); err != nil {
		t.Fatal(err)
	}
	assertBaked(t, shimText(t, home), wt)
}

// The secret-boundary case: a linked worktree usually sits on its own branch.
// When its manifest inputs differ from main's, redirecting would make every
// future launch resolve a DIFFERENT branch's credentials. Each input file is
// checked separately because exec reads all of them, and comparing only
// ainfra.yaml would miss a diverged lock.
func TestWriteLauncherShimKeepsWorktreeWhenManifestsDiverge(t *testing.T) {
	for _, name := range manifestInputs {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			repo, home := filepath.Join(root, "repo"), filepath.Join(root, "home")
			mkdirs(t, repo, home)
			writeSecretFixture(t, repo)
			// Both dirs must hold every input, so that this subtest's file is
			// the only thing that differs — differing CONTENT, never presence,
			// which is a separate case with its own tests.
			for _, n := range manifestInputs {
				if err := os.WriteFile(filepath.Join(repo, n), []byte("shared: main\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			gitRepo(t, repo)

			wt := filepath.Join(repo, ".claude", "worktrees", "task")
			gitAddWorktree(t, repo, wt, "task")
			for _, n := range manifestInputs {
				if err := os.WriteFile(filepath.Join(wt, n), []byte("shared: main\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			// Only this input diverges — e.g. the branch retargeted a secret.
			if err := os.WriteFile(filepath.Join(wt, name), []byte("shared: BRANCH\n"), 0o644); err != nil {
				t.Fatal(err)
			}

			_, note, err := writeLauncherShim(home, wt)
			if err != nil {
				t.Fatal(err)
			}
			shim := shimText(t, home)
			if strings.Contains(shim, `--chdir "`+repo+`" exec`) {
				t.Errorf("shim redirected to main despite a diverged %s, got:\n%s", name, shim)
			}
			assertBaked(t, shim, wt)
			if !strings.Contains(note, name) {
				t.Errorf("divergence was not reported to the user, note = %q", note)
			}
		})
	}
}

// manifestInputs drives the divergence check, so a test that merely iterates
// it cannot notice an entry going missing. Pin the set literally: every file
// secret resolution reads from the baked dir must be compared before the shim
// is allowed to point somewhere else. ainfra.personal.yaml is the one that was
// forgotten once — it carries envFile/path secrets that never reach any lock.
func TestManifestInputsCoversEverySecretSource(t *testing.T) {
	want := map[string]bool{
		"ainfra.yaml":          true,
		"ainfra.lock":          true,
		"ainfra.personal.yaml": true,
		"ainfra.personal.lock": true,
	}
	got := map[string]bool{}
	for _, n := range manifestInputs {
		got[n] = true
	}
	for n := range want {
		if !got[n] {
			t.Errorf("manifestInputs is missing %s — a redirect could swap it silently", n)
		}
	}
	for n := range got {
		if !want[n] {
			t.Errorf("manifestInputs has an unexpected entry %s; update this test deliberately", n)
		}
	}
}

// A file the destination TRACKS is one git would have put in the worktree, so
// its absence there is a real difference and must refuse — even though it is
// one of the personal files that are untracked in most repos. Which files a
// repo ignores is the repo's business, so the check asks git rather than
// assuming from the filename.
func TestWriteLauncherShimKeepsWorktreeWhenDestinationTracksTheFile(t *testing.T) {
	root := t.TempDir()
	repo, home := filepath.Join(root, "repo"), filepath.Join(root, "home")
	mkdirs(t, repo, home)
	writeSecretFixture(t, repo)
	// Committed, so git tracks it — the opposite of the usual gitignored case.
	if err := os.WriteFile(filepath.Join(repo, "ainfra.personal.yaml"), []byte("version: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRepo(t, repo)

	wt := filepath.Join(repo, ".claude", "worktrees", "task")
	gitAddWorktree(t, repo, wt, "task")
	// The branch removed the tracked personal manifest.
	if err := os.Remove(filepath.Join(wt, "ainfra.personal.yaml")); err != nil {
		t.Fatal(err)
	}

	_, note, err := writeLauncherShim(home, wt)
	if err != nil {
		t.Fatal(err)
	}
	assertBaked(t, shimText(t, home), wt)
	if !strings.Contains(note, "ainfra.personal.yaml") {
		t.Errorf("a tracked file missing from the worktree must refuse, note = %q", note)
	}
}

// Unreadable is not absent. A destination file that exists but cannot be read
// must never be waved through by the untracked-file excuse — its bytes were
// never compared, so the redirect would be taken on no evidence.
func TestFirstDifferingInputRefusesUnreadableDestinationFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads regardless of mode bits")
	}
	root := t.TempDir()
	from, to := filepath.Join(root, "wt"), filepath.Join(root, "repo")
	mkdirs(t, from, to)
	for _, d := range []string{from, to} {
		if err := os.WriteFile(filepath.Join(d, "ainfra.yaml"), []byte("version: 1\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Present only at the destination, and unreadable. Without the read-error
	// check this is exactly the shape the untracked excuse lets through.
	secret := filepath.Join(to, "ainfra.personal.yaml")
	if err := os.WriteFile(secret, []byte("version: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(secret, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(secret, 0o644) })

	name, ok := firstDifferingInput(from, to)
	if ok {
		t.Error("an unreadable destination file was treated as no divergence")
	}
	if name != "ainfra.personal.yaml" {
		t.Errorf("reported %q, want ainfra.personal.yaml", name)
	}
}

// The case that made the first cut inert. ainfra.personal.yaml is gitignored,
// so no linked worktree ever has one while the main checkout does — true for
// all 68 worktrees on the machine this was found on. Refusing to redirect
// there meant every real worktree install kept pinning the shim to a throwaway
// dir, which is the entire bug.
func TestWriteLauncherShimRedirectsWhenOnlyDestinationHasPersonalManifest(t *testing.T) {
	for _, name := range []string{"ainfra.personal.yaml", "ainfra.personal.lock"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			repo, home := filepath.Join(root, "repo"), filepath.Join(root, "home")
			mkdirs(t, repo, home)
			writeSecretFixture(t, repo)
			gitRepo(t, repo)

			wt := filepath.Join(repo, ".claude", "worktrees", "task")
			gitAddWorktree(t, repo, wt, "task")
			// Written after the worktree exists, so only main has it —
			// exactly what gitignoring produces.
			if err := os.WriteFile(filepath.Join(repo, name), []byte("version: 1\n"), 0o644); err != nil {
				t.Fatal(err)
			}

			_, note, err := writeLauncherShim(home, wt)
			if err != nil {
				t.Fatal(err)
			}
			wantDir, err := filepath.EvalSymlinks(repo)
			if err != nil {
				t.Fatal(err)
			}
			assertBaked(t, shimText(t, home), wantDir)
			if strings.Contains(note, "differ") {
				t.Errorf("a destination-only untracked file is not divergence, got note: %q", note)
			}
			if !strings.Contains(note, wantDir) {
				t.Errorf("redirect was not reported, note = %q", note)
			}
		})
	}
}

// The opposite direction still refuses: redirecting away from a worktree that
// has its own personal manifest would drop those secrets entirely.
func TestWriteLauncherShimKeepsWorktreeWhenOnlySourceHasPersonalManifest(t *testing.T) {
	root := t.TempDir()
	repo, home := filepath.Join(root, "repo"), filepath.Join(root, "home")
	mkdirs(t, repo, home)
	writeSecretFixture(t, repo)
	gitRepo(t, repo)

	wt := filepath.Join(repo, ".claude", "worktrees", "task")
	gitAddWorktree(t, repo, wt, "task")
	if err := os.WriteFile(filepath.Join(wt, "ainfra.personal.yaml"), []byte("version: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, note, err := writeLauncherShim(home, wt)
	if err != nil {
		t.Fatal(err)
	}
	assertBaked(t, shimText(t, home), wt)
	if !strings.Contains(note, "ainfra.personal.yaml") {
		t.Errorf("dropping the worktree's own personal manifest was not reported, note = %q", note)
	}
}

// A TRACKED file missing on one side is a real difference in shared config —
// the branch removed it deliberately — and must never be excused the way a
// gitignored one is.
func TestWriteLauncherShimKeepsWorktreeWhenTrackedInputAsymmetric(t *testing.T) {
	root := t.TempDir()
	repo, home := filepath.Join(root, "repo"), filepath.Join(root, "home")
	mkdirs(t, repo, home)
	writeSecretFixture(t, repo)
	if err := os.WriteFile(filepath.Join(repo, "ainfra.lock"), []byte("version: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRepo(t, repo)

	wt := filepath.Join(repo, ".claude", "worktrees", "task")
	gitAddWorktree(t, repo, wt, "task")
	// The branch dropped the tracked lock.
	if err := os.Remove(filepath.Join(wt, "ainfra.lock")); err != nil {
		t.Fatal(err)
	}

	_, note, err := writeLauncherShim(home, wt)
	if err != nil {
		t.Fatal(err)
	}
	assertBaked(t, shimText(t, home), wt)
	if !strings.Contains(note, "ainfra.lock") {
		t.Errorf("asymmetric tracked file was not reported, note = %q", note)
	}
}

// A diverged ainfra.personal.yaml must block the redirect, named explicitly
// rather than via manifestInputs, so this survives the list being edited.
func TestWriteLauncherShimKeepsWorktreeWhenPersonalManifestDiverges(t *testing.T) {
	root := t.TempDir()
	repo, home := filepath.Join(root, "repo"), filepath.Join(root, "home")
	mkdirs(t, repo, home)
	writeSecretFixture(t, repo)
	if err := os.WriteFile(filepath.Join(repo, "ainfra.personal.yaml"), []byte("version: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRepo(t, repo)

	wt := filepath.Join(repo, ".claude", "worktrees", "task")
	gitAddWorktree(t, repo, wt, "task")
	// Personal manifests are gitignored, so the worktree's copy is its own.
	if err := os.WriteFile(filepath.Join(wt, "ainfra.personal.yaml"), []byte("version: 1\nsecrets: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, note, err := writeLauncherShim(home, wt)
	if err != nil {
		t.Fatal(err)
	}
	assertBaked(t, shimText(t, home), wt)
	if !strings.Contains(note, "ainfra.personal.yaml") {
		t.Errorf("diverged personal manifest was not reported, note = %q", note)
	}
}

// The self-heal half: a shim already baked against a since-deleted worktree
// must recover the repo that worktree belonged to rather than dropping every
// secret. Both container layouts in worktreeContainers count.
func TestExecRecoversFromDeletedWorktreeDir(t *testing.T) {
	for _, container := range worktreeContainers {
		t.Run(filepath.Join(container...), func(t *testing.T) {
			root := t.TempDir()
			repo, home := filepath.Join(root, "repo"), filepath.Join(root, "home")
			mkdirs(t, repo, home)
			t.Setenv("HOME", home)
			t.Setenv("TEAM_ENV_BLOB", "EXCALIDRAW_API_KEY=recovered-key\n")

			writeSecretFixture(t, repo)
			gitRepo(t, repo)
			if code := run([]string{"--chdir", repo, "lock"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
				t.Fatal("lock failed")
			}

			// The path the shim was baked against, now gone.
			dead := filepath.Join(append(append([]string{repo}, container...), "deleted")...)

			call := captureExec(t)
			var out, errOut bytes.Buffer
			if code := run([]string{"--chdir", dead, "exec", "--", "sh", "-c", "true"}, &out, &errOut); code != 0 {
				t.Fatalf("exec: code=%d err=%q", code, errOut.String())
			}
			if got, ok := envValue(call.env, "EXCALIDRAW_API_KEY"); !ok || got != "recovered-key" {
				t.Errorf("child EXCALIDRAW_API_KEY = %q (present=%v), want recovered-key", got, ok)
			}
			if !strings.Contains(errOut.String(), "stale launcher shim") {
				t.Errorf("expected a stale-shim warning naming the fix, got: %q", errOut.String())
			}
			if strings.Contains(errOut.String(), "without secret injection") {
				t.Errorf("recovery still claimed secrets were dropped: %q", errOut.String())
			}
		})
	}
}

// Recovery must never reach sideways into a project the dead path cannot be
// shown to have belonged to. Merely sitting under a repo that has a manifest
// is not proof — a monorepo, a shared checkout, a mistyped --chdir, or any
// parent dir carrying an ainfra.yaml would otherwise hand its credentials to
// whatever the stale shim launches. Only the worktree layouts in
// worktreeContainers qualify.
func TestExecRefusesUnprovableRecovery(t *testing.T) {
	for _, tc := range []struct {
		name string
		// dead is relative to the repo root holding the manifest.
		dead []string
	}{
		{"not a worktree path at all", []string{"src", "pkg", "gone"}},
		{"worktrees dir but wrong parent", []string{"someproject", "worktrees", "gone"}},
		{"nested too deep under the container", []string{".claude", "worktrees", "a", "b"}},
		{"container with no name segment", []string{".claude", "worktrees"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			repo, home := filepath.Join(root, "repo"), filepath.Join(root, "home")
			mkdirs(t, repo, home)
			t.Setenv("HOME", home)
			t.Setenv("TEAM_ENV_BLOB", "EXCALIDRAW_API_KEY=not-yours\n")

			writeSecretFixture(t, repo)
			gitRepo(t, repo)
			if code := run([]string{"--chdir", repo, "lock"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
				t.Fatal("lock failed")
			}
			dead := filepath.Join(append([]string{repo}, tc.dead...)...)

			call := captureExec(t)
			var out, errOut bytes.Buffer
			if code := run([]string{"--chdir", dead, "exec", "--", "sh", "-c", "true"}, &out, &errOut); code != 0 {
				t.Fatalf("exec: code=%d err=%q", code, errOut.String())
			}
			if got, ok := envValue(call.env, "EXCALIDRAW_API_KEY"); ok {
				t.Errorf("injected secrets for an unprovable path: EXCALIDRAW_API_KEY=%q", got)
			}
			if !strings.Contains(errOut.String(), "without secret injection") {
				t.Errorf("expected a plain fail-open warning, got: %q", errOut.String())
			}
		})
	}
}

// filepath.Dir is lexical, so a relative dir would bottom out at "." and stop
// at the process cwd instead of resolving against the real filesystem.
func TestRecoveredWorktreeRepoHandlesRelativePaths(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	mkdirs(t, repo)
	writeSecretFixture(t, repo)
	gitRepo(t, repo)

	// cwd is inside the repo, and the input names the dead path relatively.
	t.Chdir(repo)
	got, ok := recoveredWorktreeRepo(filepath.Join(".claude", "worktrees", "gone"))
	if !ok {
		t.Fatal("relative worktree path did not recover the enclosing repo")
	}
	wantDir, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	gotResolved, err := filepath.EvalSymlinks(got)
	if err != nil {
		t.Fatal(err)
	}
	if gotResolved != wantDir {
		t.Errorf("recovered %q, want %q", gotResolved, wantDir)
	}
}

// The home directory is the one ancestor nearly every path shares. Even in a
// valid-looking worktree layout it must never be adopted.
func TestExecRefusesHomeDirRepo(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	mkdirs(t, home)
	t.Setenv("HOME", home)
	t.Setenv("TEAM_ENV_BLOB", "EXCALIDRAW_API_KEY=home-key\n")

	writeSecretFixture(t, home)
	gitRepo(t, home)
	if code := run([]string{"--chdir", home, "lock"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal("lock failed")
	}
	dead := filepath.Join(home, ".claude", "worktrees", "x")

	call := captureExec(t)
	var out, errOut bytes.Buffer
	if code := run([]string{"--chdir", dead, "exec", "--", "sh", "-c", "true"}, &out, &errOut); code != 0 {
		t.Fatalf("exec: code=%d err=%q", code, errOut.String())
	}
	if got, ok := envValue(call.env, "EXCALIDRAW_API_KEY"); ok {
		t.Errorf("adopted the home directory's manifest: EXCALIDRAW_API_KEY=%q", got)
	}
	if !strings.Contains(errOut.String(), "without secret injection") {
		t.Errorf("expected a plain fail-open warning, got: %q", errOut.String())
	}
}
