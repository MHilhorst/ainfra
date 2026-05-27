package fetch

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"path"
	"strings"
)

// extractTarGz reads a gzipped tar from data and returns a Bundle keyed by
// path relative to the archive root. If stripPrefix is non-empty, that prefix
// (treated as a path segment) is stripped from each entry; entries outside the
// prefix are skipped. Path-traversal entries are rejected.
func extractTarGz(data []byte, stripPrefix string) (Bundle, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("fetch: gzip open: %w", err)
	}
	defer gz.Close()
	return extractTarStream(gz, stripPrefix)
}

// extractTarAuto strips the top-level directory of the archive (the common
// shape for github and npm tarballs) and returns the remainder. If stripSub
// is non-empty, it is interpreted relative to the stripped top-level: only
// entries beneath that sub-path are kept.
func extractTarAuto(data []byte, stripSub string) (Bundle, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("fetch: gzip open: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	bundle := make(Bundle)
	total := int64(0)
	var topLevel string

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("fetch: tar read: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg && hdr.Typeflag != tar.TypeRegA {
			continue
		}
		name := path.Clean(hdr.Name)
		if name == "." || name == "/" {
			continue
		}
		if strings.HasPrefix(name, "/") || strings.HasPrefix(name, "../") || strings.Contains(name, "/../") {
			return nil, fmt.Errorf("fetch: path escapes fetch root: %q", hdr.Name)
		}
		parts := strings.SplitN(name, "/", 2)
		if topLevel == "" {
			topLevel = parts[0]
		}
		if parts[0] != topLevel || len(parts) < 2 {
			// Skip entries not under the single top-level dir.
			continue
		}
		rel := parts[1]
		if stripSub != "" {
			sub := strings.TrimSuffix(strings.TrimPrefix(path.Clean(stripSub), "./"), "/")
			if rel == sub {
				continue
			}
			if !strings.HasPrefix(rel, sub+"/") {
				continue
			}
			rel = strings.TrimPrefix(rel, sub+"/")
		}
		buf, err := readCapped(tr, &total)
		if err != nil {
			return nil, err
		}
		bundle[rel] = buf
	}
	return bundle, nil
}

func extractTarStream(r io.Reader, stripPrefix string) (Bundle, error) {
	tr := tar.NewReader(r)
	bundle := make(Bundle)
	total := int64(0)
	prefix := strings.TrimSuffix(stripPrefix, "/")

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("fetch: tar read: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg && hdr.Typeflag != tar.TypeRegA {
			continue
		}
		name := path.Clean(hdr.Name)
		if strings.HasPrefix(name, "/") || strings.HasPrefix(name, "../") || strings.Contains(name, "/../") {
			return nil, fmt.Errorf("fetch: path escapes fetch root: %q", hdr.Name)
		}
		if prefix != "" {
			if name == prefix {
				continue
			}
			if !strings.HasPrefix(name, prefix+"/") {
				continue
			}
			name = strings.TrimPrefix(name, prefix+"/")
		}
		buf, err := readCapped(tr, &total)
		if err != nil {
			return nil, err
		}
		bundle[name] = buf
	}
	return bundle, nil
}

// extractZip extracts a zip archive into a Bundle. The first single top-level
// directory is stripped (like extractTarAuto), and stripSub trims further.
func extractZip(data []byte, stripSub string) (Bundle, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("fetch: zip open: %w", err)
	}
	bundle := make(Bundle)
	total := int64(0)
	var topLevel string
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		name := path.Clean(f.Name)
		if strings.HasPrefix(name, "/") || strings.HasPrefix(name, "../") || strings.Contains(name, "/../") {
			return nil, fmt.Errorf("fetch: path escapes fetch root: %q", f.Name)
		}
		parts := strings.SplitN(name, "/", 2)
		if topLevel == "" {
			topLevel = parts[0]
		}
		var rel string
		if parts[0] == topLevel && len(parts) == 2 {
			rel = parts[1]
		} else {
			rel = name
		}
		if stripSub != "" {
			sub := strings.TrimSuffix(strings.TrimPrefix(path.Clean(stripSub), "./"), "/")
			if rel == sub {
				continue
			}
			if !strings.HasPrefix(rel, sub+"/") {
				continue
			}
			rel = strings.TrimPrefix(rel, sub+"/")
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		buf, err := readCapped(rc, &total)
		rc.Close()
		if err != nil {
			return nil, err
		}
		bundle[rel] = buf
	}
	return bundle, nil
}

func readCapped(r io.Reader, running *int64) ([]byte, error) {
	remaining := int64(maxArtifactBytes) - *running
	if remaining <= 0 {
		return nil, fmt.Errorf("fetch: archive exceeds %d bytes", maxArtifactBytes)
	}
	buf, err := io.ReadAll(io.LimitReader(r, remaining+1))
	if err != nil {
		return nil, err
	}
	if int64(len(buf)) > remaining {
		return nil, fmt.Errorf("fetch: archive exceeds %d bytes", maxArtifactBytes)
	}
	*running += int64(len(buf))
	return buf, nil
}
