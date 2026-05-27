package fetch

import (
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// NPMFetcher resolves npm:[@scope/]pkg@ver sources by reading the registry's
// version metadata, verifying dist.integrity, and extracting the tarball.
type NPMFetcher struct {
	HTTPClient  *http.Client
	Token       string
	Cache       *Cache
	RegistryURL string
}

func (n NPMFetcher) client() *http.Client {
	if n.HTTPClient != nil {
		return n.HTTPClient
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func (n NPMFetcher) registry() string {
	if n.RegistryURL != "" {
		return strings.TrimRight(n.RegistryURL, "/")
	}
	return "https://registry.npmjs.org"
}

func (n NPMFetcher) token() string {
	if n.Token != "" {
		return n.Token
	}
	return os.Getenv("NPM_TOKEN")
}

type npmDist struct {
	Tarball   string `json:"tarball"`
	Integrity string `json:"integrity"`
	SHASum    string `json:"shasum"`
}

type npmVersion struct {
	Name    string  `json:"name"`
	Version string  `json:"version"`
	Dist    npmDist `json:"dist"`
}

// parseNPMSource decodes "npm:[@scope/]pkg[@ver]". If ver is missing in the
// source, fallbackVersion is used.
func parseNPMSource(source, fallbackVersion string) (pkg, ver string, err error) {
	body := strings.TrimPrefix(source, "npm:")
	if body == "" {
		return "", "", fmt.Errorf("fetch: empty npm source")
	}
	// Scoped: @scope/name[@ver]
	if strings.HasPrefix(body, "@") {
		slash := strings.Index(body, "/")
		if slash < 0 {
			return "", "", fmt.Errorf("fetch: invalid scoped npm source %q", source)
		}
		rest := body[slash+1:]
		if at := strings.Index(rest, "@"); at >= 0 {
			pkg = body[:slash+1] + rest[:at]
			ver = rest[at+1:]
		} else {
			pkg = body
		}
	} else {
		if at := strings.Index(body, "@"); at >= 0 {
			pkg = body[:at]
			ver = body[at+1:]
		} else {
			pkg = body
		}
	}
	if ver == "" {
		ver = fallbackVersion
	}
	if ver == "" {
		return "", "", fmt.Errorf("fetch: npm source %q has no version", source)
	}
	return pkg, ver, nil
}

// Fetch implements Fetcher.
func (n NPMFetcher) Fetch(source, version string) (Bundle, error) {
	b, _, err := n.FetchResolved(source, version)
	return b, err
}

// FetchResolved returns the bundle and pinning info (tarball URL, integrity).
func (n NPMFetcher) FetchResolved(source, version string) (Bundle, Resolved, error) {
	pkg, ver, err := parseNPMSource(source, version)
	if err != nil {
		return nil, Resolved{}, err
	}

	meta, err := n.metadata(pkg, ver)
	if err != nil {
		return nil, Resolved{}, err
	}
	if meta.Dist.Tarball == "" {
		return nil, Resolved{}, fmt.Errorf("fetch: npm %s@%s: missing dist.tarball", pkg, ver)
	}

	resolved := Resolved{
		Scheme:     "npm",
		Address:    fmt.Sprintf("%s@%s|%s", pkg, ver, meta.Dist.Integrity),
		TarballURL: meta.Dist.Tarball,
		Integrity:  meta.Dist.Integrity,
	}
	if b, ok := n.Cache.Get(resolved); ok {
		return b, resolved, nil
	}

	req, err := http.NewRequest("GET", meta.Dist.Tarball, nil)
	if err != nil {
		return nil, resolved, err
	}
	if tok := n.token(); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := n.client().Do(req)
	if err != nil {
		if isNetworkError(err) {
			return nil, resolved, fmt.Errorf("fetch: source not cached and network unreachable: %w", err)
		}
		return nil, resolved, fmt.Errorf("fetch: GET %s: %w", meta.Dist.Tarball, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, resolved, fmt.Errorf("fetch: GET %s: status %d", meta.Dist.Tarball, resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxArtifactBytes))
	if err != nil {
		return nil, resolved, err
	}
	if err := verifyIntegrity(data, meta.Dist.Integrity); err != nil {
		return nil, resolved, err
	}
	// npm tarballs use "package/" as the top-level prefix.
	bundle, err := extractTarAuto(data, "")
	if err != nil {
		return nil, resolved, err
	}
	if err := n.Cache.Put(resolved, bundle); err != nil {
		return bundle, resolved, fmt.Errorf("fetch: cache write: %w", err)
	}
	return bundle, resolved, nil
}

func (n NPMFetcher) metadata(pkg, ver string) (*npmVersion, error) {
	// URL-encode pkg (the "/" in @scope/name must become %2F).
	encoded := url.PathEscape(pkg)
	api := fmt.Sprintf("%s/%s/%s", n.registry(), encoded, url.PathEscape(ver))
	req, err := http.NewRequest("GET", api, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if tok := n.token(); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := n.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch: npm metadata: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return nil, fmt.Errorf("fetch: npm %s@%s: not found", pkg, ver)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("fetch: npm %s@%s metadata: status %d", pkg, ver, resp.StatusCode)
	}
	var v npmVersion
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&v); err != nil {
		return nil, fmt.Errorf("fetch: npm metadata decode: %w", err)
	}
	return &v, nil
}

// verifyIntegrity checks data against an SRI string "sha512-base64==" or
// "sha256-base64==". An empty integrity is permitted (registries that omit
// it).
func verifyIntegrity(data []byte, integrity string) error {
	if integrity == "" {
		return nil
	}
	dash := strings.Index(integrity, "-")
	if dash < 0 {
		return fmt.Errorf("fetch: integrity %q malformed", integrity)
	}
	algo := integrity[:dash]
	want, err := base64.StdEncoding.DecodeString(integrity[dash+1:])
	if err != nil {
		return fmt.Errorf("fetch: integrity %q malformed: %w", integrity, err)
	}
	var got []byte
	switch algo {
	case "sha512":
		s := sha512.Sum512(data)
		got = s[:]
	case "sha256":
		s := sha256.Sum256(data)
		got = s[:]
	default:
		return fmt.Errorf("fetch: integrity algorithm %q unsupported", algo)
	}
	if !bytesEqual(got, want) {
		return fmt.Errorf("fetch: integrity mismatch")
	}
	return nil
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
