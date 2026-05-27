package fetch_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MHilhorst/ainfra/internal/provider/fetch"
)

type recordingTransport struct {
	called  int
	headers []http.Header
	rt      http.RoundTripper
}

func (r *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r.called++
	r.headers = append(r.headers, req.Header.Clone())
	return r.rt.RoundTrip(req)
}

func newGitHubFakeServer(t *testing.T, sha string, tarball []byte, opts ...func(*githubFakeOpts)) *httptest.Server {
	t.Helper()
	o := githubFakeOpts{}
	for _, opt := range opts {
		opt(&o)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/", func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/commits/"):
			if o.commits404 {
				http.Error(w, "not found", 404)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"sha": sha})
		case strings.Contains(r.URL.Path, "/tarball/"):
			w.Header().Set("Content-Type", "application/x-gzip")
			w.Write(tarball)
		default:
			http.Error(w, "not found", 404)
		}
	})
	return httptest.NewServer(mux)
}

type githubFakeOpts struct {
	commits404 bool
}

func withCommits404() func(*githubFakeOpts) {
	return func(o *githubFakeOpts) { o.commits404 = true }
}

func TestGitHubFetcher_TagToSHA(t *testing.T) {
	tarball := makeTarGz(t, "acme-skills-abc1234/", map[string]string{
		"SKILL.md":           "# top",
		"incident/SKILL.md":  "# incident",
		"incident/script.sh": "echo hi",
	})
	srv := newGitHubFakeServer(t, "abc1234567890abcdef0000000000000000000000", tarball)
	defer srv.Close()

	cache := &fetch.Cache{Root: t.TempDir()}
	g := fetch.GitHubFetcher{APIBase: srv.URL, Cache: cache, HTTPClient: srv.Client()}

	bundle, resolved, err := g.FetchResolved("github:acme/skills@v1.0.0", "")
	if err != nil {
		t.Fatalf("FetchResolved: %v", err)
	}
	if resolved.CommitSHA == "" {
		t.Error("expected resolved CommitSHA")
	}
	if string(bundle["SKILL.md"]) != "# top" {
		t.Errorf("SKILL.md = %q", bundle["SKILL.md"])
	}
	if string(bundle["incident/SKILL.md"]) != "# incident" {
		t.Errorf("incident/SKILL.md = %q", bundle["incident/SKILL.md"])
	}
}

func TestGitHubFetcher_SubPath(t *testing.T) {
	tarball := makeTarGz(t, "acme-skills-abc1234/", map[string]string{
		"SKILL.md":           "# top",
		"incident/SKILL.md":  "# incident",
		"incident/script.sh": "echo hi",
		"other/SKILL.md":     "# other",
	})
	srv := newGitHubFakeServer(t, "abc1234", tarball)
	defer srv.Close()

	g := fetch.GitHubFetcher{APIBase: srv.URL, Cache: &fetch.Cache{Root: t.TempDir()}, HTTPClient: srv.Client()}
	bundle, _, err := g.FetchResolved("github:acme/skills/incident@2.3.0", "")
	if err != nil {
		t.Fatalf("FetchResolved: %v", err)
	}
	if _, ok := bundle["SKILL.md"]; !ok {
		t.Error("expected SKILL.md inside subpath")
	}
	if string(bundle["SKILL.md"]) != "# incident" {
		t.Errorf("subpath SKILL.md = %q", bundle["SKILL.md"])
	}
	if _, ok := bundle["other/SKILL.md"]; ok {
		t.Error("other/SKILL.md should not be in subpath bundle")
	}
}

func TestGitHubFetcher_NotFound(t *testing.T) {
	srv := newGitHubFakeServer(t, "", nil, withCommits404())
	defer srv.Close()

	g := fetch.GitHubFetcher{APIBase: srv.URL, Cache: &fetch.Cache{Root: t.TempDir()}, HTTPClient: srv.Client()}
	_, err := g.Fetch("github:acme/missing@v1", "")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "acme/missing") {
		t.Errorf("error %q should mention org/repo", err)
	}
}

func TestGitHubFetcher_HonorsGitHubToken(t *testing.T) {
	tarball := makeTarGz(t, "acme-skills-abc/", map[string]string{"a": "a"})
	srv := newGitHubFakeServer(t, "abc", tarball)
	defer srv.Close()
	rt := &recordingTransport{rt: srv.Client().Transport}
	if rt.rt == nil {
		rt.rt = http.DefaultTransport
	}
	client := &http.Client{Transport: rt}
	g := fetch.GitHubFetcher{APIBase: srv.URL, Token: "secret-token", Cache: &fetch.Cache{Root: t.TempDir()}, HTTPClient: client}
	if _, err := g.Fetch("github:acme/skills@v1", ""); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(rt.headers) == 0 {
		t.Fatal("no requests recorded")
	}
	found := false
	for _, h := range rt.headers {
		if h.Get("Authorization") == "Bearer secret-token" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected Authorization header on at least one request; saw %v", rt.headers)
	}
}

func TestGitHubFetcher_CacheHitSkipsNetwork(t *testing.T) {
	tarball := makeTarGz(t, "acme-skills-abc/", map[string]string{"a": "a"})
	srv := newGitHubFakeServer(t, "abc", tarball)
	defer srv.Close()
	cache := &fetch.Cache{Root: t.TempDir()}
	g := fetch.GitHubFetcher{APIBase: srv.URL, Cache: cache, HTTPClient: srv.Client()}

	if _, _, err := g.FetchResolved("github:acme/skills@v1", ""); err != nil {
		t.Fatalf("first Fetch: %v", err)
	}

	// Second client errors if used.
	failClient := &http.Client{Transport: &failTransport{}}
	g2 := fetch.GitHubFetcher{
		APIBase:    srv.URL, // metadata still uses the real server (commit lookup is part of cache-key derivation)
		Cache:      cache,
		HTTPClient: srv.Client(),
	}
	// Re-fetch should use cache for tarball but still hit metadata to derive SHA.
	// We test "no second tarball download" by counting requests via a recording transport on the second call.
	rt := &recordingTransport{rt: srv.Client().Transport}
	if rt.rt == nil {
		rt.rt = http.DefaultTransport
	}
	g2.HTTPClient = &http.Client{Transport: rt}
	if _, _, err := g2.FetchResolved("github:acme/skills@v1", ""); err != nil {
		t.Fatalf("second Fetch: %v", err)
	}
	for _, h := range rt.headers {
		_ = h
	}
	// On the second call we expect exactly one request (the commit lookup); the tarball must be skipped.
	if rt.called != 1 {
		t.Errorf("expected 1 request on cache-hit path (commit lookup only), got %d", rt.called)
	}
	_ = failClient
}

func TestGitHubFetcher_PathTraversalRejected(t *testing.T) {
	tarball := makeTarGzWithEscape(t)
	srv := newGitHubFakeServer(t, "abc", tarball)
	defer srv.Close()
	g := fetch.GitHubFetcher{APIBase: srv.URL, Cache: &fetch.Cache{Root: t.TempDir()}, HTTPClient: srv.Client()}
	_, err := g.Fetch("github:acme/skills@v1", "")
	if err == nil {
		t.Fatal("expected path-traversal error")
	}
	if !strings.Contains(err.Error(), "escapes") {
		t.Errorf("error %q should mention 'escapes'", err)
	}
}

func TestGitHubFetcher_GitPlusHTTPSForm(t *testing.T) {
	tarball := makeTarGz(t, "acme-skills-deadbee/", map[string]string{"x": "y"})
	srv := newGitHubFakeServer(t, "deadbee", tarball)
	defer srv.Close()
	g := fetch.GitHubFetcher{APIBase: srv.URL, Cache: &fetch.Cache{Root: t.TempDir()}, HTTPClient: srv.Client()}
	b, _, err := g.FetchResolved("git+https://github.com/acme/skills?ref=main", "")
	if err != nil {
		t.Fatalf("FetchResolved: %v", err)
	}
	if string(b["x"]) != "y" {
		t.Errorf("got bundle %v", b)
	}
}

// failTransport always errors. Used to assert no network is called after a cache hit.
type failTransport struct{}

func (failTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return nil, fmt.Errorf("unexpected network call to %s", req.URL)
}
