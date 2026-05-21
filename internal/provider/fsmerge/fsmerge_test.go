package fsmerge

import (
	"os"
	"strings"
	"testing"
)

// memFS is a minimal in-memory FS for tests only.
type memFS struct {
	files map[string][]byte
}

func newMemFS() *memFS {
	return &memFS{files: map[string][]byte{}}
}

func (m *memFS) ReadFile(path string) ([]byte, error) {
	d, ok := m.files[path]
	if !ok {
		return nil, &notExistError{path}
	}
	return append([]byte(nil), d...), nil
}

func (m *memFS) WriteFile(path string, data []byte, _ os.FileMode) error {
	m.files[path] = append([]byte(nil), data...)
	return nil
}

func (m *memFS) MkdirAll(path string, _ os.FileMode) error {
	return nil
}

type notExistError struct{ path string }

func (e *notExistError) Error() string { return "not exist: " + e.path }

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}

func countLines(s, line string) int {
	count := 0
	for _, l := range strings.Split(s, "\n") {
		if strings.TrimSpace(l) == line {
			count++
		}
	}
	return count
}

func TestMergeJSONKeysPreservesForeignKeys(t *testing.T) {
	fs := newMemFS()
	fs.files["/c.json"] = []byte(`{"mcpServers":{"foreign":{"x":1},"old":{"y":2}}}`)

	err := MergeJSONKeys(fs, "/c.json", "mcpServers",
		map[string]any{"new": map[string]any{"z": 3}},
		[]string{"old", "new"}) // owned keys: old (now gone), new (desired)
	if err != nil {
		t.Fatal(err)
	}
	out := string(fs.files["/c.json"])
	for _, want := range []string{`"foreign"`, `"new"`} {
		if !contains(out, want) {
			t.Errorf("result missing %s: %s", want, out)
		}
	}
	if contains(out, `"old"`) {
		t.Errorf("owned-but-undesired key not removed: %s", out)
	}
}

func TestEnsureImportLineIdempotent(t *testing.T) {
	fs := newMemFS()
	if err := EnsureImportLine(fs, "/CLAUDE.md", ".claude/ainfra/context.md"); err != nil {
		t.Fatal(err)
	}
	if err := EnsureImportLine(fs, "/CLAUDE.md", ".claude/ainfra/context.md"); err != nil {
		t.Fatal(err)
	}
	out := string(fs.files["/CLAUDE.md"])
	if n := countLines(out, "@.claude/ainfra/context.md"); n != 1 {
		t.Errorf("import line appears %d times, want 1: %q", n, out)
	}
}
