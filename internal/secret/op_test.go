package secret

import (
	"fmt"
	"strings"
	"testing"
)

// stubRunner is a Runner returning a canned result, for tests.
type stubRunner struct {
	stdout string
	err    error
	gotCmd []string
}

func (s *stubRunner) Run(name string, args ...string) (string, error) {
	s.gotCmd = append([]string{name}, args...)
	return s.stdout, s.err
}

func TestOpResolverResolvesViaCLI(t *testing.T) {
	runner := &stubRunner{stdout: "hunter2"}
	got, err := OpResolver{Runner: runner}.Resolve("op://Engineering/db/password")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != "hunter2" {
		t.Errorf("Resolve = %q, want %q", got, "hunter2")
	}
	want := []string{"op", "read", "op://Engineering/db/password"}
	if strings.Join(runner.gotCmd, " ") != strings.Join(want, " ") {
		t.Errorf("ran %v, want %v", runner.gotCmd, want)
	}
}

func TestOpResolverNotSignedInGivesActionableError(t *testing.T) {
	runner := &stubRunner{err: fmt.Errorf("[ERROR] you are not currently signed in")}
	err := OpResolver{Runner: runner}.Check("op://Engineering/db/password")
	if err == nil || !strings.Contains(err.Error(), "op signin") {
		t.Errorf("error = %v, want it to mention `op signin`", err)
	}
}

func TestOpResolverNotInstalledGivesActionableError(t *testing.T) {
	runner := &stubRunner{err: fmt.Errorf(`exec: "op": executable file not found in $PATH`)}
	_, err := OpResolver{Runner: runner}.Resolve("op://Engineering/db/password")
	if err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Errorf("error = %v, want it to mention the CLI is not installed", err)
	}
}

func TestOpResolverNoAccountsConfiguredGivesActionableError(t *testing.T) {
	// op phrases the not-signed-in state several ways; the capitalized
	// "No accounts configured" one used to slip through as raw output.
	runner := &stubRunner{err: fmt.Errorf("No accounts configured for use with 1Password CLI.")}
	err := OpResolver{Runner: runner}.Check("op://Engineering/db/password")
	if err == nil || !strings.Contains(err.Error(), "op signin") {
		t.Errorf("error = %v, want it to mention `op signin`", err)
	}
}

func TestOpResolverAvailableProbesWhoami(t *testing.T) {
	runner := &stubRunner{stdout: `{"email":"dev@example.com"}`}
	if err := (OpResolver{Runner: runner}).Available(); err != nil {
		t.Fatalf("Available: %v", err)
	}
	if want := "op whoami"; strings.Join(runner.gotCmd, " ") != want {
		t.Errorf("ran %v, want %v", runner.gotCmd, want)
	}
}

func TestOpResolverAvailableFailsWhenNotReady(t *testing.T) {
	runner := &stubRunner{err: fmt.Errorf("[ERROR] you are not currently signed in")}
	err := OpResolver{Runner: runner}.Available()
	if err == nil || !strings.Contains(err.Error(), "op signin") {
		t.Errorf("error = %v, want it to mention `op signin`", err)
	}
	if strings.Contains(err.Error(), "secret") {
		t.Errorf("readiness error should not be phrased per-secret: %v", err)
	}
}

// scriptRunner returns a different canned result per command, keyed by the
// joined command line, so a test can model op's real split behaviour.
type scriptRunner struct {
	results map[string]struct {
		stdout string
		err    error
	}
	ran []string
}

func (s *scriptRunner) Run(name string, args ...string) (string, error) {
	cmd := strings.Join(append([]string{name}, args...), " ")
	s.ran = append(s.ran, cmd)
	if r, ok := s.results[cmd]; ok {
		return r.stdout, r.err
	}
	return "", fmt.Errorf("unexpected command: %s", cmd)
}

func TestOpResolverAvailableAcceptsAppIntegrationWithoutSession(t *testing.T) {
	// The desktop-app integration authorizes each read via biometrics without
	// ever creating a CLI session, so `op whoami` fails while reads succeed.
	// Gating on whoami alone made install unusable on that setup.
	runner := &scriptRunner{results: map[string]struct {
		stdout string
		err    error
	}{
		"op whoami":                   {err: fmt.Errorf("[ERROR] account is not signed in")},
		"op vault list --format=json": {stdout: `[{"id":"abc","name":"Private"}]`},
	}}
	if err := (OpResolver{Runner: runner}).Available(); err != nil {
		t.Fatalf("Available = %v, want nil (op can read despite no session)", err)
	}
	if len(runner.ran) != 2 || runner.ran[1] != "op vault list --format=json" {
		t.Errorf("ran %v, want whoami then the vault-list fallback", runner.ran)
	}
}

func TestOpResolverAvailableFailsWhenBothProbesFail(t *testing.T) {
	// Genuinely unusable op: neither the session probe nor a read works.
	runner := &scriptRunner{results: map[string]struct {
		stdout string
		err    error
	}{
		"op whoami":                   {err: fmt.Errorf("[ERROR] account is not signed in")},
		"op vault list --format=json": {err: fmt.Errorf("[ERROR] account is not signed in")},
	}}
	err := (OpResolver{Runner: runner}).Available()
	if err == nil || !strings.Contains(err.Error(), "op signin") {
		t.Errorf("error = %v, want the actionable not-signed-in hint", err)
	}
}
