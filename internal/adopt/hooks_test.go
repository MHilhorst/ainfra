package adopt

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadHooksMissingFile(t *testing.T) {
	dir := t.TempDir()
	out, _, err := readHooks(filepath.Join(dir, ".claude", "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if out != nil {
		t.Errorf("expected nil, got %+v", out)
	}
}

func TestReadHooksMultipleEvents(t *testing.T) {
	dir := t.TempDir()
	settings := filepath.Join(dir, ".claude", "settings.json")
	os.MkdirAll(filepath.Dir(settings), 0o755)
	os.WriteFile(settings, []byte(`{
		"hooks": {
			"PostToolUse": [{"matcher":"Edit","hooks":[{"type":"command","command":"a","timeout":5}]}],
			"SessionStart": [{"hooks":[{"type":"command","command":"b"}]}]
		}
	}`), 0o644)
	out, ws, err := readHooks(settings)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 hooks, got %+v", out)
	}
	if len(ws) == 0 {
		t.Errorf("expected synthesized-id warning")
	}
}

func TestSynthesizeHookIDStable(t *testing.T) {
	a := synthesizeHookID("PostToolUse", "Edit|Write", "gofmt -w .")
	b := synthesizeHookID("PostToolUse", "Edit|Write", "gofmt -w .")
	if a != b {
		t.Errorf("not stable: %q vs %q", a, b)
	}
	c := synthesizeHookID("PostToolUse", "Edit|Write", "different")
	if a == c {
		t.Errorf("collision: %q", a)
	}
}
