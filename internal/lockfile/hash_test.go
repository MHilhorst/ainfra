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

func TestContentHashHandlesStringMap(t *testing.T) {
	// map[string]string (e.g. MCPServer.Env) must hash identically to the
	// same content as map[string]any — callers must not need to pre-convert.
	asString := ContentHash(map[string]string{"FOO": "bar", "BAZ": "qux"})
	asAny := ContentHash(map[string]any{"FOO": "bar", "BAZ": "qux"})
	if asString != asAny {
		t.Errorf("map[string]string hash %q != map[string]any hash %q", asString, asAny)
	}
}

func TestContentHashDistinguishesIntFromString(t *testing.T) {
	asInt := ContentHash(map[string]any{"port": 8080})
	asStr := ContentHash(map[string]any{"port": "8080"})
	if asInt == asStr {
		t.Error("int 8080 and string \"8080\" must hash differently")
	}
}

func TestContentHashNoDelimiterCollision(t *testing.T) {
	a := ContentHash(map[string]any{"a:b": "c"})
	b := ContentHash(map[string]any{"a": "b:c"})
	if a == b {
		t.Error("a key containing ':' must not collide with a value containing ':'")
	}
}

func TestContentHashHandlesNestedStructures(t *testing.T) {
	base := map[string]any{"env": map[string]any{"H": "host"}, "args": []any{"-y", "pkg"}}
	same := map[string]any{"args": []any{"-y", "pkg"}, "env": map[string]any{"H": "host"}}
	diff := map[string]any{"env": map[string]any{"H": "other"}, "args": []any{"-y", "pkg"}}
	if ContentHash(base) != ContentHash(same) {
		t.Error("nested structure hash must be key-order independent")
	}
	if ContentHash(base) == ContentHash(diff) {
		t.Error("nested structure hash must change when nested content changes")
	}
}
