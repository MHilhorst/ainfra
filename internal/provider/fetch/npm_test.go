package fetch_test

import (
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MHilhorst/ainfra/internal/provider/fetch"
)

func sha512Integrity(data []byte) string {
	s := sha512.Sum512(data)
	return "sha512-" + base64.StdEncoding.EncodeToString(s[:])
}

type npmFake struct {
	server         *httptest.Server
	lastMetaPath   string
	tarballHits    int
	metadataHits   int
	authzHeader    string
	tarballPayload []byte
	integrity      string
	notFound       bool
}

func newNPMFake(t *testing.T, tarball []byte, integrity string) *npmFake {
	t.Helper()
	f := &npmFake{tarballPayload: tarball, integrity: integrity}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		f.authzHeader = r.Header.Get("Authorization")
		if strings.HasSuffix(r.URL.Path, ".tgz") {
			f.tarballHits++
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Write(f.tarballPayload)
			return
		}
		// metadata
		f.metadataHits++
		if r.URL.RawPath != "" {
			f.lastMetaPath = r.URL.RawPath
		} else {
			f.lastMetaPath = r.URL.Path
		}
		if f.notFound {
			http.Error(w, "not found", 404)
			return
		}
		base := "http://" + r.Host + r.URL.Path + ".tgz"
		_ = json.NewEncoder(w).Encode(map[string]any{
			"name":    "x",
			"version": "1",
			"dist": map[string]string{
				"tarball":   base,
				"integrity": f.integrity,
			},
		})
	})
	f.server = httptest.NewServer(mux)
	return f
}

func TestNPMFetcher_TarballIntegrityOK(t *testing.T) {
	tarball := makeTarGz(t, "package/", map[string]string{
		"index.js":     "module.exports = 1",
		"package.json": `{"name":"x"}`,
	})
	f := newNPMFake(t, tarball, sha512Integrity(tarball))
	defer f.server.Close()

	n := fetch.NPMFetcher{RegistryURL: f.server.URL, Cache: &fetch.Cache{Root: t.TempDir()}, HTTPClient: f.server.Client()}
	b, r, err := n.FetchResolved("npm:x@1.0.0", "")
	if err != nil {
		t.Fatalf("FetchResolved: %v", err)
	}
	if string(b["index.js"]) != "module.exports = 1" {
		t.Errorf("index.js content wrong: %q", b["index.js"])
	}
	if r.Integrity == "" {
		t.Error("expected resolved Integrity")
	}
}

func TestNPMFetcher_IntegrityMismatch(t *testing.T) {
	tarball := makeTarGz(t, "package/", map[string]string{"a": "a"})
	f := newNPMFake(t, tarball, "sha512-"+base64.StdEncoding.EncodeToString(make([]byte, 64)))
	defer f.server.Close()

	n := fetch.NPMFetcher{RegistryURL: f.server.URL, Cache: &fetch.Cache{Root: t.TempDir()}, HTTPClient: f.server.Client()}
	_, err := n.Fetch("npm:x@1.0.0", "")
	if err == nil {
		t.Fatal("expected integrity mismatch")
	}
	if !strings.Contains(err.Error(), "integrity mismatch") {
		t.Errorf("error %q should mention 'integrity mismatch'", err)
	}
}

func TestNPMFetcher_ScopedPackageURLEncoded(t *testing.T) {
	tarball := makeTarGz(t, "package/", map[string]string{"a": "a"})
	f := newNPMFake(t, tarball, sha512Integrity(tarball))
	defer f.server.Close()

	n := fetch.NPMFetcher{RegistryURL: f.server.URL, Cache: &fetch.Cache{Root: t.TempDir()}, HTTPClient: f.server.Client()}
	if _, err := n.Fetch("npm:@scope/pkg@1.0.0", ""); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	// PathEscape("@scope/pkg") => "@scope%2Fpkg"
	if !strings.Contains(f.lastMetaPath, "%2F") {
		t.Errorf("expected URL-encoded slash in metadata path, got %q", f.lastMetaPath)
	}
}

func TestNPMFetcher_VersionFallback(t *testing.T) {
	tarball := makeTarGz(t, "package/", map[string]string{"a": "a"})
	f := newNPMFake(t, tarball, sha512Integrity(tarball))
	defer f.server.Close()
	n := fetch.NPMFetcher{RegistryURL: f.server.URL, Cache: &fetch.Cache{Root: t.TempDir()}, HTTPClient: f.server.Client()}
	// No @ver in source, version comes via the version arg.
	if _, err := n.Fetch("npm:lodash", "4.17.21"); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
}

func TestNPMFetcher_CacheHit(t *testing.T) {
	tarball := makeTarGz(t, "package/", map[string]string{"a": "a"})
	f := newNPMFake(t, tarball, sha512Integrity(tarball))
	defer f.server.Close()
	cache := &fetch.Cache{Root: t.TempDir()}
	n := fetch.NPMFetcher{RegistryURL: f.server.URL, Cache: cache, HTTPClient: f.server.Client()}
	if _, err := n.Fetch("npm:x@1.0.0", ""); err != nil {
		t.Fatalf("first Fetch: %v", err)
	}
	before := f.tarballHits
	if _, err := n.Fetch("npm:x@1.0.0", ""); err != nil {
		t.Fatalf("second Fetch: %v", err)
	}
	if f.tarballHits != before {
		t.Errorf("expected no additional tarball download on cache hit; before=%d after=%d", before, f.tarballHits)
	}
}

func TestNPMFetcher_HonorsToken(t *testing.T) {
	tarball := makeTarGz(t, "package/", map[string]string{"a": "a"})
	f := newNPMFake(t, tarball, sha512Integrity(tarball))
	defer f.server.Close()
	n := fetch.NPMFetcher{RegistryURL: f.server.URL, Cache: &fetch.Cache{Root: t.TempDir()}, HTTPClient: f.server.Client(), Token: "tok"}
	if _, err := n.Fetch("npm:x@1.0.0", ""); err != nil {
		t.Fatal(err)
	}
	if f.authzHeader != "Bearer tok" {
		t.Errorf("expected Bearer tok header, got %q", f.authzHeader)
	}
}

func TestNPMFetcher_NotFound(t *testing.T) {
	f := newNPMFake(t, nil, "")
	f.notFound = true
	defer f.server.Close()
	n := fetch.NPMFetcher{RegistryURL: f.server.URL, Cache: &fetch.Cache{Root: t.TempDir()}, HTTPClient: f.server.Client()}
	_, err := n.Fetch("npm:nope@1.0.0", "")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected not-found error, got %v", err)
	}
}
