package resolve

import (
	"strings"
	"testing"
)

func TestInterpolateNamespaces(t *testing.T) {
	scope := Scope{
		Params:   map[string]any{"host": "db.example"},
		Instance: map[string]any{"id": "analytics-db"},
		Resolved: map[string]any{"tunnelPort": 13306},
		Secret:   map[string]any{"pw": "<secret:pw>"},
	}
	cases := map[string]string{
		"${params.host}":              "db.example",
		"${instance.id}-tunnel":       "analytics-db-tunnel",
		"port ${resolved.tunnelPort}": "port 13306",
		"${secret.pw}":                "<secret:pw>",
	}
	for in, want := range cases {
		got, err := Interpolate(in, scope)
		if err != nil {
			t.Fatalf("Interpolate(%q): %v", in, err)
		}
		if got != want {
			t.Errorf("Interpolate(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestInterpolateUnknownReferenceErrors(t *testing.T) {
	_, err := Interpolate("${params.nope}", Scope{Params: map[string]any{}})
	if err == nil {
		t.Fatal("want error for unknown reference")
	}
}

func TestInterpolateUnknownNamespaceErrors(t *testing.T) {
	_, err := Interpolate("${bogus.key}", Scope{})
	if err == nil {
		t.Fatal("want error for unknown namespace")
	}
}

func TestInterpolateReportsFirstError(t *testing.T) {
	_, err := Interpolate("${params.first} ${params.second}", Scope{Params: map[string]any{}})
	if err == nil || !strings.Contains(err.Error(), "first") {
		t.Fatalf("want error naming the first bad reference, got %v", err)
	}
}
