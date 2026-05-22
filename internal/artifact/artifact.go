// Package artifact owns the published-artifact layout a subscriber machine
// fetches: a copy of ainfra.lock, the ainfra.sub.json descriptor, optional
// bundles, and a MANIFEST.sha256 integrity index.
// See docs/superpowers/specs/2026-05-22-subscriber-mode-design.md §3.
package artifact

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// DescriptorName is the descriptor filename inside an artifact.
const DescriptorName = "ainfra.sub.json"

// ManifestName is the integrity-index filename inside an artifact.
const ManifestName = "MANIFEST.sha256"

// Descriptor is the subscription descriptor a subscriber machine reads.
type Descriptor struct {
	SchemaVersion int    `json:"schemaVersion"`
	ArtifactURL   string `json:"artifactURL"`
	Agent         string `json:"agent"`
	Sync          Sync   `json:"sync"`
}

// Sync controls the subscriber's generated scheduled job.
type Sync struct {
	IntervalMinutes int  `json:"intervalMinutes"`
	RunAtLogin      bool `json:"runAtLogin"`
}

// Write creates an artifact directory: every entry of files, the descriptor,
// and a MANIFEST.sha256 hashing all of them (the descriptor included, the
// manifest itself excluded).
func Write(dir string, d Descriptor, files map[string][]byte) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	all := map[string][]byte{}
	for name, content := range files {
		all[name] = content
	}
	desc, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	all[DescriptorName] = desc

	for name, content := range all {
		full := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(full, content, 0o644); err != nil {
			return err
		}
	}
	return os.WriteFile(filepath.Join(dir, ManifestName), []byte(hashIndex(all)), 0o644)
}

// hashIndex renders a deterministic "<sha256>  <name>" line per file, sorted
// by name.
func hashIndex(files map[string][]byte) string {
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, n := range names {
		sum := sha256.Sum256(files[n])
		fmt.Fprintf(&b, "%s  %s\n", hex.EncodeToString(sum[:]), n)
	}
	return b.String()
}

// Verify recomputes hashes for every file listed in MANIFEST.sha256 and fails
// if any file is missing or its content does not match.
func Verify(dir string) error {
	idx, err := os.ReadFile(filepath.Join(dir, ManifestName))
	if err != nil {
		return fmt.Errorf("artifact: reading %s: %w", ManifestName, err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(idx)), "\n") {
		if line == "" {
			continue
		}
		want, name, ok := strings.Cut(line, "  ")
		if !ok {
			return fmt.Errorf("artifact: malformed %s line %q", ManifestName, line)
		}
		content, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return fmt.Errorf("artifact: %s missing: %w", name, err)
		}
		sum := sha256.Sum256(content)
		if got := hex.EncodeToString(sum[:]); got != want {
			return fmt.Errorf("artifact: %s hash mismatch (want %s, got %s)", name, want, got)
		}
	}
	return nil
}

// ReadDescriptor loads and parses the descriptor from an artifact directory.
func ReadDescriptor(dir string) (Descriptor, error) {
	var d Descriptor
	raw, err := os.ReadFile(filepath.Join(dir, DescriptorName))
	if err != nil {
		return d, err
	}
	err = json.Unmarshal(raw, &d)
	return d, err
}
