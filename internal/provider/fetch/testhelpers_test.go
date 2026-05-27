package fetch_test

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"sort"
	"testing"
	"time"
)

// makeTarGz builds an in-memory gzipped tar with entries keyed by path. The
// caller controls the prefix (e.g. "acme-skills-abc1234/" for github,
// "package/" for npm). Iteration order is sorted for reproducibility.
func makeTarGz(t *testing.T, prefix string, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	paths := make([]string, 0, len(entries))
	for p := range entries {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	// Emit the top-level dir entry first.
	if prefix != "" {
		if err := tw.WriteHeader(&tar.Header{Name: prefix, Mode: 0o755, Typeflag: tar.TypeDir, ModTime: time.Unix(0, 0)}); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range paths {
		body := entries[p]
		hdr := &tar.Header{
			Name:     prefix + p,
			Mode:     0o644,
			Size:     int64(len(body)),
			Typeflag: tar.TypeReg,
			ModTime:  time.Unix(0, 0),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// makeTarGzWithEscape returns a tar containing an entry whose name escapes
// the archive root via "..". Used to test path-traversal rejection.
func makeTarGzWithEscape(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	body := "evil"
	if err := tw.WriteHeader(&tar.Header{Name: "../escape.txt", Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

// makeZip builds an in-memory zip with the entries provided.
func makeZip(t *testing.T, prefix string, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	paths := make([]string, 0, len(entries))
	for p := range entries {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		w, err := zw.Create(prefix + p)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(entries[p])); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
