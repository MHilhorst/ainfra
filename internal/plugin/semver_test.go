package plugin

import "testing"

func TestBump(t *testing.T) {
	cases := []struct {
		in, level, want string
	}{
		{"2.11.0", "patch", "2.11.1"},
		{"2.11.0", "minor", "2.12.0"},
		{"2.11.3", "major", "3.0.0"},
	}
	for _, c := range cases {
		got, err := Bump(c.in, c.level)
		if err != nil {
			t.Fatalf("Bump(%q,%q): %v", c.in, c.level, err)
		}
		if got != c.want {
			t.Errorf("Bump(%q,%q)=%q want %q", c.in, c.level, got, c.want)
		}
	}
	if _, err := Bump("2.11", "patch"); err == nil {
		t.Error("expected error on malformed version")
	}
	if _, err := Bump("2.11.0", "sideways"); err == nil {
		t.Error("expected error on unknown level")
	}
}
