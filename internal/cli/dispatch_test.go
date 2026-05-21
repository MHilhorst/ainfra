package cli

import (
	"bytes"
	"flag"
	"strings"
	"testing"
)

func dispatchRegistry(out, errOut *bytes.Buffer) *Registry {
	r := NewRegistry(out, errOut, "0.0.0-test")
	r.Add(&Command{
		Name: "lock", Summary: "resolve and lock", UsageLine: "ainfra lock",
		Run: func(ctx Context) int {
			ctx.Stdout.Write([]byte("locked in " + ctx.Dir + "\n"))
			return 0
		},
	})
	echo := &Command{Name: "echo", Summary: "echo a flag", UsageLine: "ainfra echo"}
	var loud bool
	echo.SetFlags = func(fs *flag.FlagSet) { fs.BoolVar(&loud, "loud", false, "shout") }
	echo.Run = func(ctx Context) int {
		if loud {
			ctx.Stdout.Write([]byte("LOUD\n"))
		} else {
			ctx.Stdout.Write([]byte("quiet\n"))
		}
		return 0
	}
	r.Add(echo)
	return r
}

func TestDispatchRunsCommand(t *testing.T) {
	var out, errOut bytes.Buffer
	code := dispatchRegistry(&out, &errOut).Dispatch([]string{"lock"})
	if code != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%q", code, errOut.String())
	}
	if !strings.Contains(out.String(), "locked in") {
		t.Errorf("lock did not run: %q", out.String())
	}
}

func TestDispatchNoArgsPrintsOverview(t *testing.T) {
	var out bytes.Buffer
	code := dispatchRegistry(&out, &bytes.Buffer{}).Dispatch(nil)
	if code != 0 || !strings.Contains(out.String(), "Usage:") {
		t.Errorf("no-args dispatch: code=%d out=%q", code, out.String())
	}
}

func TestDispatchUnknownCommandExits2(t *testing.T) {
	var errOut bytes.Buffer
	code := dispatchRegistry(&bytes.Buffer{}, &errOut).Dispatch([]string{"lok"})
	if code != 2 {
		t.Errorf("exit = %d, want 2", code)
	}
	if !strings.Contains(errOut.String(), `Did you mean "lock"?`) {
		t.Errorf("no suggestion: %q", errOut.String())
	}
}

func TestDispatchVersionFlag(t *testing.T) {
	var out bytes.Buffer
	code := dispatchRegistry(&out, &bytes.Buffer{}).Dispatch([]string{"--version"})
	if code != 0 || !strings.Contains(out.String(), "ainfra 0.0.0-test") {
		t.Errorf("--version: code=%d out=%q", code, out.String())
	}
}

func TestDispatchHelpForCommand(t *testing.T) {
	var out bytes.Buffer
	code := dispatchRegistry(&out, &bytes.Buffer{}).Dispatch([]string{"lock", "--help"})
	if code != 0 || !strings.Contains(out.String(), "ainfra lock") {
		t.Errorf("lock --help: code=%d out=%q", code, out.String())
	}
}

func TestDispatchPerCommandFlag(t *testing.T) {
	var out bytes.Buffer
	code := dispatchRegistry(&out, &bytes.Buffer{}).Dispatch([]string{"echo", "--loud"})
	if code != 0 || !strings.Contains(out.String(), "LOUD") {
		t.Errorf("echo --loud: code=%d out=%q", code, out.String())
	}
}

func TestDispatchChdirIsPassedToContext(t *testing.T) {
	var out bytes.Buffer
	code := dispatchRegistry(&out, &bytes.Buffer{}).Dispatch([]string{"--chdir", "/tmp/x", "lock"})
	if code != 0 || !strings.Contains(out.String(), "locked in /tmp/x") {
		t.Errorf("--chdir: code=%d out=%q", code, out.String())
	}
}

func TestDispatchVersionFlagAfterChdir(t *testing.T) {
	var out bytes.Buffer
	code := dispatchRegistry(&out, &bytes.Buffer{}).Dispatch([]string{"--chdir", "/tmp/x", "--version"})
	if code != 0 || !strings.Contains(out.String(), "ainfra 0.0.0-test") {
		t.Errorf("--version after --chdir: code=%d out=%q", code, out.String())
	}
}

func TestDispatchShortHelpForCommand(t *testing.T) {
	var out bytes.Buffer
	code := dispatchRegistry(&out, &bytes.Buffer{}).Dispatch([]string{"lock", "-h"})
	if code != 0 || !strings.Contains(out.String(), "ainfra lock") {
		t.Errorf("lock -h: code=%d out=%q", code, out.String())
	}
}
