package fsmerge

import (
	"reflect"
	"strings"
	"testing"
)

func TestMergeManagedRegionCreatesRegionPreservingUserContent(t *testing.T) {
	fs := newMemFS()
	fs.files["/AGENTS.md"] = []byte("# My project\n\nHand-written guidance.\n")

	err := MergeManagedRegion(fs, "/AGENTS.md",
		map[string]string{"incident-response": "Page the on-call."},
		[]string{"incident-response"})
	if err != nil {
		t.Fatal(err)
	}
	out := string(fs.files["/AGENTS.md"])
	if !strings.Contains(out, "# My project") || !strings.Contains(out, "Hand-written guidance.") {
		t.Errorf("user content not preserved:\n%s", out)
	}
	if !strings.Contains(out, "<!-- ainfra:begin -->") || !strings.Contains(out, "<!-- ainfra:end -->") {
		t.Errorf("region markers missing:\n%s", out)
	}
	if !strings.Contains(out, "<!-- ainfra:rule incident-response -->") {
		t.Errorf("rule marker missing:\n%s", out)
	}
	if !strings.Contains(out, "Page the on-call.") {
		t.Errorf("rule content missing:\n%s", out)
	}
}

func TestManagedRegionIDsRoundTrip(t *testing.T) {
	fs := newMemFS()
	if err := MergeManagedRegion(fs, "/AGENTS.md",
		map[string]string{"a": "Content A", "b": "Content B"}, []string{"a", "b"}); err != nil {
		t.Fatal(err)
	}
	ids, err := ManagedRegionIDs(fs, "/AGENTS.md")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ids, []string{"a", "b"}) {
		t.Errorf("ids = %v, want [a b]", ids)
	}
}

func TestManagedRegionIDsMissingFile(t *testing.T) {
	fs := newMemFS()
	ids, err := ManagedRegionIDs(fs, "/nope.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 0 {
		t.Errorf("ids = %v, want empty", ids)
	}
}

func TestMergeManagedRegionUpdatesAndRemoves(t *testing.T) {
	fs := newMemFS()
	if err := MergeManagedRegion(fs, "/AGENTS.md",
		map[string]string{"a": "old A", "b": "Content B"}, []string{"a", "b"}); err != nil {
		t.Fatal(err)
	}
	// Update a, remove b.
	if err := MergeManagedRegion(fs, "/AGENTS.md",
		map[string]string{"a": "new A"}, []string{"a", "b"}); err != nil {
		t.Fatal(err)
	}
	out := string(fs.files["/AGENTS.md"])
	if !strings.Contains(out, "new A") || strings.Contains(out, "old A") {
		t.Errorf("rule 'a' not updated:\n%s", out)
	}
	if strings.Contains(out, "Content B") || strings.Contains(out, "ainfra:rule b") {
		t.Errorf("rule 'b' not removed:\n%s", out)
	}
}

func TestMergeManagedRegionRemovesEmptyRegion(t *testing.T) {
	fs := newMemFS()
	fs.files["/AGENTS.md"] = []byte("# Title\n")
	if err := MergeManagedRegion(fs, "/AGENTS.md",
		map[string]string{"a": "A"}, []string{"a"}); err != nil {
		t.Fatal(err)
	}
	// Remove the only rule — the region must disappear entirely.
	if err := MergeManagedRegion(fs, "/AGENTS.md",
		map[string]string{}, []string{"a"}); err != nil {
		t.Fatal(err)
	}
	out := string(fs.files["/AGENTS.md"])
	if strings.Contains(out, "ainfra:begin") || strings.Contains(out, "ainfra:end") {
		t.Errorf("region should be gone:\n%s", out)
	}
	if !strings.Contains(out, "# Title") {
		t.Errorf("user content lost:\n%s", out)
	}
}

func TestMergeManagedRegionExactOutput(t *testing.T) {
	fs := newMemFS()
	fs.files["/AGENTS.md"] = []byte("# Title\n")
	if err := MergeManagedRegion(fs, "/AGENTS.md",
		map[string]string{"a": "Rule A"}, []string{"a"}); err != nil {
		t.Fatal(err)
	}
	want := "# Title\n\n" +
		"<!-- ainfra:begin -->\n" +
		"<!-- ainfra:rule a -->\n" +
		"Rule A\n" +
		"<!-- ainfra:end -->\n"
	got := string(fs.files["/AGENTS.md"])
	if got != want {
		t.Errorf("exact output mismatch:\nwant:\n%q\ngot:\n%q", want, got)
	}
}

func TestMergeManagedRegionRejectsUnterminatedRegion(t *testing.T) {
	fs := newMemFS()
	fs.files["/AGENTS.md"] = []byte("<!-- ainfra:begin -->\nno end marker here\n")
	err := MergeManagedRegion(fs, "/AGENTS.md", map[string]string{"a": "A"}, []string{"a"})
	if err == nil {
		t.Error("expected an error for a region with no end marker, got nil")
	}
}
