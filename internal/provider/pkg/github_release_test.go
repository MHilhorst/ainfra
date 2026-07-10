package pkg_test

import (
	"errors"
	"testing"

	"github.com/MHilhorst/ainfra/internal/provider"
	"github.com/MHilhorst/ainfra/internal/provider/pkg"
)

// fakeFetcher is a test double for GitHubReleaseFetcher.
type fakeFetcher struct {
	url string
	err error
}

func (f fakeFetcher) ResolveAssetURL(owner, repo, tag, assetName string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return f.url, nil
}

func (f fakeFetcher) Download(url string) ([]byte, error) {
	if f.err != nil {
		return nil, f.err
	}
	return []byte("test-binary-data"), nil
}

func TestGitHubReleaseAdapterName(t *testing.T) {
	a := pkg.GitHubReleaseAdapter{}
	if got := a.Name(); got != "github-release" {
		t.Errorf("Name() = %q, want %q", got, "github-release")
	}
}

func TestGitHubReleaseAdapterIsInstalled_True(t *testing.T) {
	fs := provider.NewMemFilesystem()
	_ = fs.MkdirAll("/home/.local/bin", 0o755)
	_ = fs.WriteFile("/home/.local/bin/repo", []byte("binary"), 0o755)

	env := provider.Env{
		FS:   fs,
		Home: "/home",
	}

	a := pkg.GitHubReleaseAdapter{}
	installed, err := a.IsInstalled(env, map[string]any{
		"owner":        "test",
		"repo":         "repo",
		"assetPattern": "myapp-{os}-{arch}",
	})
	if err != nil {
		t.Fatalf("IsInstalled: %v", err)
	}
	if !installed {
		t.Error("expected installed=true")
	}
}

func TestGitHubReleaseAdapterIsInstalled_False(t *testing.T) {
	fs := provider.NewMemFilesystem()

	env := provider.Env{
		FS:   fs,
		Home: "/home",
	}

	a := pkg.GitHubReleaseAdapter{}
	installed, err := a.IsInstalled(env, map[string]any{
		"owner":        "test",
		"repo":         "repo",
		"assetPattern": "myapp-{os}-{arch}",
	})
	if err != nil {
		t.Fatalf("IsInstalled: %v", err)
	}
	if installed {
		t.Error("expected installed=false")
	}
}

func TestGitHubReleaseAdapterInstall(t *testing.T) {
	fs := provider.NewMemFilesystem()

	env := provider.Env{
		FS:   fs,
		Home: "/home",
	}

	fakeF := fakeFetcher{
		url: "https://github.com/test/repo/releases/download/v1.0/myapp-darwin-arm64",
	}

	a := pkg.GitHubReleaseAdapter{
		Fetcher: fakeF,
	}

	err := a.Install(env, map[string]any{
		"owner":        "test",
		"repo":         "repo",
		"assetPattern": "myapp-{os}-{arch}",
	})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}

	// Verify the binary was written
	data, err := fs.ReadFile("/home/.local/bin/repo")
	if err != nil {
		t.Errorf("binary not found: %v", err)
	}
	if string(data) != "test-binary-data" {
		t.Errorf("binary content = %q, want %q", data, "test-binary-data")
	}
}

func TestGitHubReleaseAdapterInstall_CustomBinary(t *testing.T) {
	fs := provider.NewMemFilesystem()

	env := provider.Env{
		FS:   fs,
		Home: "/home",
	}

	fakeF := fakeFetcher{
		url: "https://github.com/test/repo/releases/download/v1.0/myapp-darwin-arm64",
	}

	a := pkg.GitHubReleaseAdapter{
		Fetcher: fakeF,
	}

	err := a.Install(env, map[string]any{
		"owner":        "test",
		"repo":         "repo",
		"assetPattern": "myapp-{os}-{arch}",
		"binary":       "custom-name",
	})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}

	// Verify the binary was written with the custom name
	_, err = fs.ReadFile("/home/.local/bin/custom-name")
	if err != nil {
		t.Errorf("binary with custom name not found: %v", err)
	}
}

func TestGitHubReleaseAdapterInstall_MissingOwner(t *testing.T) {
	env := provider.Env{
		FS:   provider.NewMemFilesystem(),
		Home: "/home",
	}

	a := pkg.GitHubReleaseAdapter{}

	err := a.Install(env, map[string]any{
		"repo":         "repo",
		"assetPattern": "myapp-{os}-{arch}",
	})
	if err == nil {
		t.Error("expected error for missing owner")
	}
}

func TestGitHubReleaseAdapterInstall_MissingRepo(t *testing.T) {
	env := provider.Env{
		FS:   provider.NewMemFilesystem(),
		Home: "/home",
	}

	a := pkg.GitHubReleaseAdapter{}

	err := a.Install(env, map[string]any{
		"owner":        "test",
		"assetPattern": "myapp-{os}-{arch}",
	})
	if err == nil {
		t.Error("expected error for missing repo")
	}
}

func TestGitHubReleaseAdapterInstall_MissingAssetPattern(t *testing.T) {
	env := provider.Env{
		FS:   provider.NewMemFilesystem(),
		Home: "/home",
	}

	a := pkg.GitHubReleaseAdapter{}

	err := a.Install(env, map[string]any{
		"owner": "test",
		"repo":  "repo",
	})
	if err == nil {
		t.Error("expected error for missing assetPattern")
	}
}

func TestGitHubReleaseAdapterInstall_FetchError(t *testing.T) {
	env := provider.Env{
		FS:   provider.NewMemFilesystem(),
		Home: "/home",
	}

	fakeF := fakeFetcher{
		err: errors.New("network error"),
	}

	a := pkg.GitHubReleaseAdapter{
		Fetcher: fakeF,
	}

	err := a.Install(env, map[string]any{
		"owner":        "test",
		"repo":         "repo",
		"assetPattern": "myapp-{os}-{arch}",
	})
	if err == nil {
		t.Error("expected error for fetch failure")
	}
}

func TestGitHubReleaseAdapterInstall_Tag(t *testing.T) {
	fs := provider.NewMemFilesystem()

	env := provider.Env{
		FS:   fs,
		Home: "/home",
	}

	fakeF := fakeFetcher{
		url: "https://github.com/test/repo/releases/download/v1.5.0/myapp-darwin-arm64",
	}

	a := pkg.GitHubReleaseAdapter{
		Fetcher: fakeF,
	}

	err := a.Install(env, map[string]any{
		"owner":        "test",
		"repo":         "repo",
		"assetPattern": "myapp-{os}-{arch}",
		"tag":          "v1.5.0",
	})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}

	// Verify the binary was written
	_, err = fs.ReadFile("/home/.local/bin/repo")
	if err != nil {
		t.Errorf("binary not found: %v", err)
	}
}
