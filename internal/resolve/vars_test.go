package resolve

import (
	"fmt"
	"strings"
	"testing"

	"github.com/MHilhorst/ainfra/internal/manifest"
	"github.com/MHilhorst/ainfra/internal/provider"
)

// substituteVars tests

func TestSubstituteVarsBasic(t *testing.T) {
	got, err := substituteVars("hi {{NAME}}", map[string]string{"NAME": "Al"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "hi Al" {
		t.Errorf("got %q, want %q", got, "hi Al")
	}
}

func TestSubstituteVarsRepeatedPlaceholder(t *testing.T) {
	got, err := substituteVars("{{A}} and {{A}}", map[string]string{"A": "x"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "x and x" {
		t.Errorf("got %q, want %q", got, "x and x")
	}
}

func TestSubstituteVarsMultipleKeys(t *testing.T) {
	got, err := substituteVars("{{X}}-{{Y}}", map[string]string{"X": "foo", "Y": "bar"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "foo-bar" {
		t.Errorf("got %q, want %q", got, "foo-bar")
	}
}

func TestSubstituteVarsUndefinedKey(t *testing.T) {
	_, err := substituteVars("{{MISSING}}", map[string]string{})
	if err == nil {
		t.Fatal("expected error for undefined variable, got nil")
	}
	if !strings.Contains(err.Error(), "MISSING") {
		t.Errorf("error = %q, want it to mention MISSING", err)
	}
}

func TestSubstituteVarsNoPlaceholders(t *testing.T) {
	got, err := substituteVars("no placeholders here", map[string]string{"X": "y"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "no placeholders here" {
		t.Errorf("got %q", got)
	}
}

func TestSubstituteVarsUnclosedBrace(t *testing.T) {
	// An unclosed {{ is not a valid placeholder and must be left alone without error.
	got, err := substituteVars("{{ x", map[string]string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "{{ x" {
		t.Errorf("got %q, want {{ x", got)
	}
}

// collectVars tests

func TestCollectVarsTeamOnly(t *testing.T) {
	layers := map[manifest.Layer]*manifest.Manifest{
		manifest.LayerTeam: {Vars: map[string]manifest.Var{
			"NAME": {From: "value", Value: "Acme"},
		}},
	}
	got := collectVars(layers)
	if v, ok := got["NAME"]; !ok || v.Value != "Acme" {
		t.Errorf("NAME = %+v", got["NAME"])
	}
}

func TestCollectVarsTeamWinsOverPersonal(t *testing.T) {
	// team layer is first-seen, so team value wins for shared key
	layers := map[manifest.Layer]*manifest.Manifest{
		manifest.LayerTeam: {Vars: map[string]manifest.Var{
			"NAME": {From: "value", Value: "Team"},
		}},
		manifest.LayerPersonal: {Vars: map[string]manifest.Var{
			"NAME": {From: "value", Value: "Personal"},
		}},
	}
	got := collectVars(layers)
	if got["NAME"].Value != "Team" {
		t.Errorf("NAME.Value = %q, want Team (team wins)", got["NAME"].Value)
	}
}

func TestCollectVarsMissingLayerSkipped(t *testing.T) {
	layers := map[manifest.Layer]*manifest.Manifest{
		manifest.LayerRepo: {Vars: map[string]manifest.Var{
			"X": {From: "value", Value: "repo"},
		}},
	}
	got := collectVars(layers)
	if _, ok := got["X"]; !ok {
		t.Error("X missing from collectVars result")
	}
	if len(got) != 1 {
		t.Errorf("got %d vars, want 1", len(got))
	}
}

// resolveVars tests

func TestResolveVarsValue(t *testing.T) {
	specs := map[string]manifest.Var{
		"NAME": {From: "value", Value: "Acme"},
	}
	got, err := resolveVars(specs, provider.NewFakeRunner())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["NAME"] != "Acme" {
		t.Errorf("NAME = %q, want Acme", got["NAME"])
	}
}

func TestResolveVarsEnv(t *testing.T) {
	t.Setenv("MY_TEST_VAR", "hello-from-env")
	specs := map[string]manifest.Var{
		"MY_TEST_VAR": {From: "env", Env: "MY_TEST_VAR"},
	}
	got, err := resolveVars(specs, provider.NewFakeRunner())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["MY_TEST_VAR"] != "hello-from-env" {
		t.Errorf("MY_TEST_VAR = %q, want hello-from-env", got["MY_TEST_VAR"])
	}
}

func TestResolveVarsCommand(t *testing.T) {
	runner := provider.NewFakeRunner()
	runner.Script["sh -c git config user.name"] = provider.FakeResult{
		Output: []byte("Alice\n"),
	}
	specs := map[string]manifest.Var{
		"FULL_NAME": {From: "command", Command: "git config user.name"},
	}
	got, err := resolveVars(specs, runner)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["FULL_NAME"] != "Alice" {
		t.Errorf("FULL_NAME = %q, want Alice", got["FULL_NAME"])
	}
}

func TestResolveVarsCommandError(t *testing.T) {
	runner := provider.NewFakeRunner()
	runner.Script["sh -c git config user.name"] = provider.FakeResult{
		Err: fmt.Errorf("exit status 1"),
	}
	specs := map[string]manifest.Var{
		"FULL_NAME": {From: "command", Command: "git config user.name"},
	}
	_, err := resolveVars(specs, runner)
	if err == nil {
		t.Fatal("expected error from failed command, got nil")
	}
	if !strings.Contains(err.Error(), "FULL_NAME") {
		t.Errorf("error = %q, want it to mention FULL_NAME", err)
	}
}
