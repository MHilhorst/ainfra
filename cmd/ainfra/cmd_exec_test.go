package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MHilhorst/ainfra/internal/cli"
	"github.com/MHilhorst/ainfra/internal/lockfile"
	"github.com/MHilhorst/ainfra/internal/secret"
)

func TestExecResolvesSecretsIntoChildEnv(t *testing.T) {
	dir := t.TempDir()
	lock := &lockfile.Lock{
		Version: 1,
		Secrets: map[string]lockfile.SecretRef{
			"AINFRA_SECRET_MCPSERVERS_LINEAR_TOKEN": {
				Var: "AINFRA_SECRET_MCPSERVERS_LINEAR_TOKEN",
				Ref: "op://Eng/linear/mcp", Scheme: "op", Scope: "shared", Layer: "repo",
			},
		},
	}
	if err := lockfile.Write(filepath.Join(dir, "ainfra.lock"), lock); err != nil {
		t.Fatalf("Write lock: %v", err)
	}

	reg := secret.NewRegistry()
	reg.Add(fakeExecResolver{scheme: "op", value: "resolved-token"})

	var gotEnv []string
	execFn := func(argv0 string, argv, envv []string) error {
		gotEnv = envv
		return nil
	}

	ctx := cli.Context{Dir: dir, Args: []string{"echo", "hi"}, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}
	if code := runExecWith(ctx, reg, execFn); code != 0 {
		t.Fatalf("runExecWith exit code = %d, want 0", code)
	}

	found := false
	for _, kv := range gotEnv {
		if kv == "AINFRA_SECRET_MCPSERVERS_LINEAR_TOKEN=resolved-token" {
			found = true
		}
	}
	if !found {
		t.Errorf("child env missing resolved secret; env = %v", gotEnv)
	}
}

func TestExecAbortsWhenSecretUnresolvable(t *testing.T) {
	dir := t.TempDir()
	lock := &lockfile.Lock{
		Version: 1,
		Secrets: map[string]lockfile.SecretRef{
			"AINFRA_SECRET_X": {Var: "AINFRA_SECRET_X", Ref: "op://Eng/x/y", Scheme: "op", Layer: "repo"},
		},
	}
	if err := lockfile.Write(filepath.Join(dir, "ainfra.lock"), lock); err != nil {
		t.Fatalf("Write lock: %v", err)
	}

	reg := secret.NewRegistry()
	reg.Add(fakeExecResolver{scheme: "op", err: true})

	called := false
	execFn := func(string, []string, []string) error { called = true; return nil }

	var stderr bytes.Buffer
	ctx := cli.Context{Dir: dir, Args: []string{"echo"}, Stdout: &bytes.Buffer{}, Stderr: &stderr}
	if code := runExecWith(ctx, reg, execFn); code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if called {
		t.Error("child must not be launched when a secret fails to resolve")
	}
	if !strings.Contains(stderr.String(), "resolve") {
		t.Errorf("stderr = %q, want a resolution-failure message", stderr.String())
	}
}

// fakeExecResolver is a Resolver for the exec tests.
type fakeExecResolver struct {
	scheme, value string
	err           bool
}

func (f fakeExecResolver) Scheme() string { return f.scheme }
func (f fakeExecResolver) Resolve(string) (string, error) {
	if f.err {
		return "", errExecResolve
	}
	return f.value, nil
}
func (f fakeExecResolver) Check(string) error {
	if f.err {
		return errExecResolve
	}
	return nil
}

var errExecResolve = stringError("could not resolve")

type stringError string

func (e stringError) Error() string { return string(e) }
