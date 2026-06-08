package claudecode

import (
	"strings"
	"testing"
)

// An ssh-tunnel service has no spec.command — just host/port fields. The start
// script must render a real, idempotent `ssh -f -N -L` command, not the generic
// "# TODO: add start command" stub.
func TestBuildStartScript_SSHTunnel(t *testing.T) {
	spec := map[string]any{
		"localPort":  "13307",
		"remoteHost": "127.0.0.1",
		"remotePort": 3306,
		"sshUser":    "deploy",
		"sshHost":    "trein-vertraging-platform-prod",
	}
	got := buildStartScript("db-tunnel", "ssh-tunnel", spec)

	if strings.Contains(got, "TODO") {
		t.Fatalf("ssh-tunnel start script still a stub:\n%s", got)
	}
	wantCmd := "ssh -f -N -L 13307:127.0.0.1:3306 deploy@trein-vertraging-platform-prod"
	if !strings.Contains(got, wantCmd) {
		t.Errorf("missing tunnel command %q in:\n%s", wantCmd, got)
	}
	// Idempotency guard: re-running on a live tunnel must be a no-op, so the
	// script checks whether the local port is already listening before dialing.
	if !strings.Contains(got, "13307") || !strings.Contains(got, "nc -z") {
		t.Errorf("start script lacks a port-already-listening guard:\n%s", got)
	}
}

func TestBuildStopScript_SSHTunnel(t *testing.T) {
	spec := map[string]any{
		"localPort":  "13307",
		"remoteHost": "127.0.0.1",
		"remotePort": 3306,
		"sshUser":    "deploy",
		"sshHost":    "trein-vertraging-platform-prod",
	}
	got := buildStopScript("db-tunnel", "ssh-tunnel", spec)
	if strings.Contains(got, "TODO") {
		t.Fatalf("ssh-tunnel stop script still a stub:\n%s", got)
	}
	if !strings.Contains(got, "pkill -f") || !strings.Contains(got, "13307:127.0.0.1:3306") {
		t.Errorf("stop script does not target the tunnel process:\n%s", got)
	}
}

// A generic service that does provide spec.command keeps the existing behavior.
func TestBuildStartScript_GenericCommandUnchanged(t *testing.T) {
	got := buildStartScript("svc", "process", map[string]any{"command": "run-me --flag"})
	if !strings.Contains(got, "run-me --flag") {
		t.Errorf("generic command not rendered:\n%s", got)
	}
}
