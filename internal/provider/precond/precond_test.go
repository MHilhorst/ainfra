package precond_test

import (
	"errors"
	"testing"

	"github.com/MHilhorst/ainfra/internal/provider"
	"github.com/MHilhorst/ainfra/internal/provider/precond"
)

func TestCheckAll_Pass(t *testing.T) {
	runner := provider.NewFakeRunner()
	runner.Script["sh -c true"] = provider.FakeResult{Output: []byte("")}

	env := provider.Env{Runner: runner}

	ps := []precond.Precondition{
		{ID: "check-true", Command: "true", Remediation: "nothing needed"},
	}

	failures := precond.CheckAll(env, ps)
	if len(failures) != 0 {
		t.Fatalf("CheckAll: expected 0 failures, got %d: %v", len(failures), failures)
	}
}

func TestCheckAll_Fail(t *testing.T) {
	runner := provider.NewFakeRunner()
	runner.Script["sh -c false"] = provider.FakeResult{Err: errors.New("exit status 1")}

	env := provider.Env{Runner: runner}

	ps := []precond.Precondition{
		{ID: "check-false", Command: "false", Remediation: "fix it by doing X"},
	}

	failures := precond.CheckAll(env, ps)
	if len(failures) != 1 {
		t.Fatalf("CheckAll: expected 1 failure, got %d", len(failures))
	}
	if failures[0].ID != "check-false" {
		t.Errorf("failure.ID = %q, want %q", failures[0].ID, "check-false")
	}
	if failures[0].Remediation != "fix it by doing X" {
		t.Errorf("failure.Remediation = %q, want %q", failures[0].Remediation, "fix it by doing X")
	}
}

func TestCheckAll_Aggregate(t *testing.T) {
	runner := provider.NewFakeRunner()
	runner.Script["sh -c true"] = provider.FakeResult{Output: []byte("")}
	runner.Script["sh -c false"] = provider.FakeResult{Err: errors.New("exit status 1")}
	runner.Script["sh -c git status"] = provider.FakeResult{Err: errors.New("not a git repo")}

	env := provider.Env{Runner: runner}

	ps := []precond.Precondition{
		{ID: "ok-check", Command: "true", Remediation: ""},
		{ID: "fail-check", Command: "false", Remediation: "run setup.sh"},
		{ID: "git-check", Command: "git status", Remediation: "initialize a git repo"},
	}

	failures := precond.CheckAll(env, ps)
	if len(failures) != 2 {
		t.Fatalf("CheckAll: expected 2 failures, got %d: %v", len(failures), failures)
	}

	ids := map[string]string{}
	for _, f := range failures {
		ids[f.ID] = f.Remediation
	}
	if ids["fail-check"] != "run setup.sh" {
		t.Errorf("fail-check remediation = %q, want %q", ids["fail-check"], "run setup.sh")
	}
	if ids["git-check"] != "initialize a git repo" {
		t.Errorf("git-check remediation = %q, want %q", ids["git-check"], "initialize a git repo")
	}
}

func TestCheckAll_EmptyCommand_Skipped(t *testing.T) {
	runner := provider.NewFakeRunner()

	env := provider.Env{Runner: runner}

	ps := []precond.Precondition{
		{ID: "empty-cmd", Command: "", Remediation: "should be skipped"},
	}

	failures := precond.CheckAll(env, ps)
	if len(failures) != 0 {
		t.Fatalf("CheckAll: expected 0 failures for empty command, got %d", len(failures))
	}
	if len(runner.Calls) != 0 {
		t.Errorf("expected no runner calls for empty command, got %v", runner.Calls)
	}
}

func TestCheckAll_EmptySlice(t *testing.T) {
	runner := provider.NewFakeRunner()
	env := provider.Env{Runner: runner}

	failures := precond.CheckAll(env, nil)
	if len(failures) != 0 {
		t.Fatalf("CheckAll(nil): expected 0 failures, got %d", len(failures))
	}
}

func TestCheckAll_DNSResolves(t *testing.T) {
	env := provider.Env{Runner: provider.NewFakeRunner()}
	ps := []precond.Precondition{
		{ID: "resolves", Kind: "dns-resolves", Host: "localhost"},
		{ID: "missing", Kind: "dns-resolves", Host: "nonexistent-host.invalid", Remediation: "connect the VPN"},
		{ID: "no-host", Kind: "dns-resolves", Host: ""},
	}
	failures := precond.CheckAll(env, ps)
	if len(failures) != 1 {
		t.Fatalf("got %d failures %v, want 1 (only the .invalid host)", len(failures), failures)
	}
	if failures[0].ID != "missing" {
		t.Errorf("failure ID = %q, want %q", failures[0].ID, "missing")
	}
	if failures[0].Remediation != "connect the VPN" {
		t.Errorf("remediation = %q, want it carried through", failures[0].Remediation)
	}
}
