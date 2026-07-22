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

// The shim is per-machine but a worktree is per-task. Installing from a
// worktree must bake the main worktree's path in, or deleting that worktree
// silently drops secret injection from every future claude launch.
func TestWriteLauncherShimRedirectsOutOfWorktree(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	home := filepath.Join(root, "home")
	for _, d := range []string{repo, home} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeSecretFixture(t, repo)
	gitRepo(t, repo)

	wt := filepath.Join(repo, ".claude", "worktrees", "throwaway")
	gitAddWorktree(t, repo, wt, "task")

	shim, err := writeLauncherShim(home, wt)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(shim)
	if err != nil {
		t.Fatal(err)
	}
	// EvalSymlinks: on macOS t.TempDir() lives under /var, a symlink to
	// /private/var, and git reports the resolved form.
	wantDir, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), wantDir+`" exec`) {
		t.Errorf("shim does not target the main worktree %q, got:\n%s", wantDir, raw)
	}
	if strings.Contains(string(raw), "throwaway") {
		t.Errorf("shim is pinned to the linked worktree, got:\n%s", raw)
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
			repo := filepath.Join(root, "repo")
			home := filepath.Join(root, "home")
			for _, d := range []string{repo, home} {
				if err := os.MkdirAll(d, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			writeSecretFixture(t, repo)
			if tc.git {
				gitRepo(t, repo)
			}

			shim, err := writeLauncherShim(home, repo)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(shim)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(raw), repo+`" exec`) {
				t.Errorf("shim path was rewritten away from %q, got:\n%s", repo, raw)
			}
		})
	}
}

// A worktree whose main checkout has no manifest must not be redirected: a
// stale-but-present path beats a confidently wrong one.
func TestWriteLauncherShimKeepsWorktreeWhenMainHasNoManifest(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	home := filepath.Join(root, "home")
	for _, d := range []string{repo, home} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRepo(t, repo)

	wt := filepath.Join(root, "wt")
	gitAddWorktree(t, repo, wt, "task")
	writeSecretFixture(t, wt)

	shim, err := writeLauncherShim(home, wt)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(shim)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), wt+`" exec`) {
		t.Errorf("shim was redirected to a manifest-less dir, got:\n%s", raw)
	}
}

// The self-heal half: a shim already baked against a since-deleted worktree
// must recover the enclosing manifest rather than dropping every secret.
func TestExecRecoversFromDeletedWorktreeDir(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	home := filepath.Join(root, "home")
	for _, d := range []string{repo, home} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", home)
	t.Setenv("TEAM_ENV_BLOB", "EXCALIDRAW_API_KEY=recovered-key\n")

	writeSecretFixture(t, repo)
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
