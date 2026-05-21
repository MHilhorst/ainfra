package ui

import (
	"bytes"
	"strings"
	"testing"
)

func TestConfirmAcceptsExactlyYes(t *testing.T) {
	var out bytes.Buffer
	ok, err := Confirm(strings.NewReader("yes\n"), &out, "Apply? ")
	if err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	if !ok {
		t.Error("Confirm(yes) = false, want true")
	}
	if !strings.Contains(out.String(), "Apply? ") {
		t.Errorf("prompt not written: %q", out.String())
	}
}

func TestConfirmRejectsAnythingElse(t *testing.T) {
	for _, in := range []string{"y\n", "no\n", "YES\n", "\n", ""} {
		ok, err := Confirm(strings.NewReader(in), &bytes.Buffer{}, "Apply? ")
		if err != nil {
			t.Fatalf("Confirm(%q): %v", in, err)
		}
		if ok {
			t.Errorf("Confirm(%q) = true, want false", in)
		}
	}
}
