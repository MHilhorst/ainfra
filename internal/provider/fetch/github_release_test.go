package fetch_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MHilhorst/ainfra/internal/provider/fetch"
)

func newGitHubReleaseFakeServer(t *testing.T, opts ...func(*githubReleaseFakeOpts)) *httptest.Server {
	t.Helper()
	o := githubReleaseFakeOpts{
		assets: map[string]string{
			"slack-mcp-server-darwin-amd64": "binary-amd64",
			"slack-mcp-server-darwin-arm64": "binary-arm64",
			"slack-mcp-server-linux-amd64":  "binary-linux",
		},
	}
	for _, opt := range opts {
		opt(&o)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/", func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/test/repo/releases/latest" && o.latestNotFound:
			http.Error(w, "not found", 404)
			return
		case r.URL.Path == "/repos/test/repo/releases/latest":
			rel := map[string]any{
				"assets": []map[string]string{},
			}
			for name := range o.assets {
				rel["assets"] = append(rel["assets"].([]map[string]string), map[string]string{
					"name":                   name,
					"browser_download_url":   "https://github.com/test/repo/releases/download/v1.0/" + name,
				})
			}
			_ = json.NewEncoder(w).Encode(rel)
		case r.URL.Path == "/repos/test/repo/releases/tags/v1.2.3":
			rel := map[string]any{
				"assets": []map[string]string{},
			}
			for name := range o.assets {
				rel["assets"] = append(rel["assets"].([]map[string]string), map[string]string{
					"name":                   name,
					"browser_download_url":   "https://github.com/test/repo/releases/download/v1.2.3/" + name,
				})
			}
			_ = json.NewEncoder(w).Encode(rel)
		default:
			http.Error(w, "not found", 404)
		}
	})
	return httptest.NewServer(mux)
}

type githubReleaseFakeOpts struct {
	assets         map[string]string
	latestNotFound bool
}

func TestGitHubReleaseFetcher_ResolveAssetURL_Latest(t *testing.T) {
	srv := newGitHubReleaseFakeServer(t)
	defer srv.Close()

	f := fetch.GitHubReleaseFetcher{
		HTTPClient: srv.Client(),
		APIBase:    srv.URL,
	}

	url, err := f.ResolveAssetURL("test", "repo", "latest", "slack-mcp-server-darwin-arm64")
	if err != nil {
		t.Fatalf("ResolveAssetURL: %v", err)
	}
	if url == "" {
		t.Error("expected non-empty URL")
	}
	if !contains(url, "slack-mcp-server-darwin-arm64") {
		t.Errorf("URL does not contain asset name: %s", url)
	}
}

func TestGitHubReleaseFetcher_ResolveAssetURL_Tag(t *testing.T) {
	srv := newGitHubReleaseFakeServer(t)
	defer srv.Close()

	f := fetch.GitHubReleaseFetcher{
		HTTPClient: srv.Client(),
		APIBase:    srv.URL,
	}

	url, err := f.ResolveAssetURL("test", "repo", "v1.2.3", "slack-mcp-server-linux-amd64")
	if err != nil {
		t.Fatalf("ResolveAssetURL: %v", err)
	}
	if url == "" {
		t.Error("expected non-empty URL")
	}
	if !contains(url, "v1.2.3") {
		t.Errorf("URL does not contain tag: %s", url)
	}
}

func TestGitHubReleaseFetcher_ResolveAssetURL_NotFound(t *testing.T) {
	srv := newGitHubReleaseFakeServer(t)
	defer srv.Close()

	f := fetch.GitHubReleaseFetcher{
		HTTPClient: srv.Client(),
		APIBase:    srv.URL,
	}

	_, err := f.ResolveAssetURL("test", "repo", "latest", "nonexistent-asset")
	if err == nil {
		t.Error("expected error for missing asset")
	}
	if !contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' in error, got: %v", err)
	}
}

func TestGitHubReleaseFetcher_ResolveAssetURL_ReleaseNotFound(t *testing.T) {
	srv := newGitHubReleaseFakeServer(t, func(o *githubReleaseFakeOpts) {
		o.latestNotFound = true
	})
	defer srv.Close()

	f := fetch.GitHubReleaseFetcher{
		HTTPClient: srv.Client(),
		APIBase:    srv.URL,
	}

	_, err := f.ResolveAssetURL("test", "repo", "latest", "slack-mcp-server-darwin-arm64")
	if err == nil {
		t.Error("expected error for missing release")
	}
	if !contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' in error, got: %v", err)
	}
}

func TestSubstituteAssetPattern(t *testing.T) {
	pattern := "slack-mcp-server-{os}-{arch}"
	result := fetch.SubstituteAssetPattern(pattern)
	if contains(result, "{") || contains(result, "}") {
		t.Errorf("substitution failed, still contains placeholders: %s", result)
	}
	// Just verify it produces a valid-looking result; exact values depend on test machine
	if result == pattern {
		t.Error("substitution produced no change")
	}
	t.Logf("Substituted pattern: %s", result)
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 || (len(s) > 0 && len(substr) > 0 && s[0:len(substr)] == substr) || (len(s) > len(substr) && len(substr) > 0 && s[len(s)-len(substr):] == substr) || func() bool {
		for i := 0; i < len(s)-len(substr)+1; i++ {
			if s[i:i+len(substr)] == substr {
				return true
			}
		}
		return false
	}())
}
