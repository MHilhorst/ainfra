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
	if note != "" {
		t.Errorf("clean redirect should be silent, got note: %q", note)
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
			// the only thing that differs.
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

// The self-heal half: a shim already baked against a since-deleted worktree
// must recover the enclosing repo rather than dropping every secret.
func TestExecRecoversFromDeletedWorktreeDir(t *testing.T) {
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
	dead := filepath.Join(repo, ".claude", "worktrees", "deleted")

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
}

// Recovery must never reach sideways into an unrelated project. An ancestor
// holding a manifest is not enough — a shared parent dir (~/clients, a scratch
// dir, anything another process can write) would otherwise hand its
// credentials to whatever the stale shim launches.
func TestExecRefusesUnrelatedAncestorManifest(t *testing.T) {
	for _, tc := range []struct {
		name     string
		repoRoot bool
	}{
		{"ancestor is not a repo root", false},
		{"ancestor is a repo root", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			// The manifest sits in a shared parent, two levels above the dead
			// path, with an unrelated project in between.
			shared := filepath.Join(root, "shared")
			dead := filepath.Join(shared, "someproject", "worktrees", "gone")
			home := filepath.Join(root, "home")
			mkdirs(t, shared, home)
			t.Setenv("HOME", home)
			t.Setenv("TEAM_ENV_BLOB", "EXCALIDRAW_API_KEY=not-yours\n")

			writeSecretFixture(t, shared)
			if tc.repoRoot {
				gitRepo(t, shared)
			}
			if code := run([]string{"--chdir", shared, "lock"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
				t.Fatal("lock failed")
			}

			call := captureExec(t)
			var out, errOut bytes.Buffer
			if code := run([]string{"--chdir", dead, "exec", "--", "sh", "-c", "true"}, &out, &errOut); code != 0 {
				t.Fatalf("exec: code=%d err=%q", code, errOut.String())
			}
			got, ok := envValue(call.env, "EXCALIDRAW_API_KEY")
			if tc.repoRoot {
				// A repo root enclosing the dead path is the legitimate
				// recovery case this feature exists for.
				if !ok || got != "not-yours" {
					t.Errorf("expected recovery from the enclosing repo, got %q (present=%v)", got, ok)
				}
				return
			}
			if ok {
				t.Errorf("leaked an unrelated dir's secret: EXCALIDRAW_API_KEY=%q", got)
			}
			if !strings.Contains(errOut.String(), "without secret injection") {
				t.Errorf("expected a plain fail-open warning, got: %q", errOut.String())
			}
		})
	}
}

// The home directory is the one ancestor nearly every path shares, so a
// manifest sitting in it must never be adopted by recovery.
func TestExecRefusesHomeDirManifest(t *testing.T) {
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

	dead := filepath.Join(home, "projects", "gone", "worktrees", "x")

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

// filepath.Dir is lexical, so a relative dir would bottom out at "." and stop
// at the process cwd instead of reaching the real ancestors above it.
func TestNearestManifestDirHandlesRelativePaths(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	dead := filepath.Join(repo, ".claude", "worktrees", "gone")
	mkdirs(t, repo)
	writeSecretFixture(t, repo)
	gitRepo(t, repo)

	// cwd is inside the repo, and the input names the dead path relatively.
	t.Chdir(repo)
	got, ok := nearestManifestDir(filepath.Join(".claude", "worktrees", "gone"))
	if !ok {
		t.Fatalf("relative path %q did not recover the enclosing repo", dead)
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
