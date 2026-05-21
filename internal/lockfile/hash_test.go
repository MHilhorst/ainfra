package lockfile

import "testing"

func TestContentHashIsKeyOrderIndependent(t *testing.T) {
	a := map[string]any{"x": 1, "y": 2}
	b := map[string]any{"y": 2, "x": 1}
	if ContentHash(a) != ContentHash(b) {
		t.Error("hash must not depend on map key order")
	}
}

func TestContentHashChangesWithContent(t *testing.T) {
	a := map[string]any{"x": 1}
	b := map[string]any{"x": 2}
	if ContentHash(a) == ContentHash(b) {
		t.Error("hash must change when content changes")
	}
}

func TestContentHashHasPrefix(t *testing.T) {
	if got := ContentHash(map[string]any{}); got[:7] != "sha256:" {
		t.Errorf("hash %q missing sha256: prefix", got)
	}
}
