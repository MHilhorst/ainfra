package pkg_test

import (
	"errors"
	"testing"

	"github.com/MHilhorst/ainfra/internal/provider"
	"github.com/MHilhorst/ainfra/internal/provider/pkg"
)

func TestBrewAdapterName(t *testing.T) {
	var a pkg.BrewAdapter
	if got := a.Name(); got != "brew" {
		t.Errorf("BrewAdapter.Name() = %q, want brew", got)
	}
}

func TestBrewAdapterIsInstalled_true(t *testing.T) {
	runner := provider.NewFakeRunner()
	runner.Script["brew list --versions jq"] = provider.FakeResult{Output: []byte("jq 1.7\n")}
	env := provider.Env{Runner: runner}

	ok, err := pkg.BrewAdapter{}.IsInstalled(env, "jq")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("expected IsInstalled = true")
	}
	if len(runner.Calls) != 1 || runner.Calls[0] != "brew list --versions jq" {
		t.Errorf("unexpected calls: %v", runner.Calls)
	}
}

func TestBrewAdapterIsInstalled_false(t *testing.T) {
	runner := provider.NewFakeRunner()
	runner.Script["brew list --versions missing"] = provider.FakeResult{Err: errors.New("not found")}
	env := provider.Env{Runner: runner}

	ok, err := pkg.BrewAdapter{}.IsInstalled(env, "missing")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("expected IsInstalled = false")
	}
}

func TestBrewAdapterInstall(t *testing.T) {
	runner := provider.NewFakeRunner()
	runner.Script["brew install jq"] = provider.FakeResult{}
	env := provider.Env{Runner: runner}

	a := pkg.BrewAdapter{}
	if err := a.Install(env, "jq"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(runner.Calls) != 1 || runner.Calls[0] != "brew install jq" {
		t.Errorf("unexpected calls: %v", runner.Calls)
	}
}

func TestNpmAdapterName(t *testing.T) {
	var a pkg.NpmAdapter
	if got := a.Name(); got != "npm" {
		t.Errorf("NpmAdapter.Name() = %q, want npm", got)
	}
}

func TestNpmAdapterIsInstalled_true(t *testing.T) {
	runner := provider.NewFakeRunner()
	runner.Script["npm ls -g --depth 0 typescript"] = provider.FakeResult{Output: []byte("typescript@5.4.5\n")}
	env := provider.Env{Runner: runner}

	ok, err := pkg.NpmAdapter{}.IsInstalled(env, "typescript")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("expected IsInstalled = true")
	}
}

func TestNpmAdapterIsInstalled_false(t *testing.T) {
	runner := provider.NewFakeRunner()
	runner.Script["npm ls -g --depth 0 missing"] = provider.FakeResult{Err: errors.New("not found")}
	env := provider.Env{Runner: runner}

	ok, err := pkg.NpmAdapter{}.IsInstalled(env, "missing")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("expected IsInstalled = false")
	}
}

func TestNpmAdapterInstall(t *testing.T) {
	runner := provider.NewFakeRunner()
	runner.Script["npm install -g typescript"] = provider.FakeResult{}
	env := provider.Env{Runner: runner}

	na := pkg.NpmAdapter{}
	if err := na.Install(env, "typescript"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(runner.Calls) != 1 || runner.Calls[0] != "npm install -g typescript" {
		t.Errorf("unexpected calls: %v", runner.Calls)
	}
}

func TestSelectKnownAdapters(t *testing.T) {
	cases := []struct {
		method string
		name   string
	}{
		{"brew", "brew"},
		{"npm", "npm"},
		{"npm-g", "npm"},
	}
	for _, c := range cases {
		a, ok := pkg.Select(c.method)
		if !ok {
			t.Errorf("Select(%q) ok = false, want true", c.method)
			continue
		}
		if a == nil {
			t.Errorf("Select(%q) returned nil adapter", c.method)
			continue
		}
		if got := a.Name(); got != c.name {
			t.Errorf("Select(%q).Name() = %q, want %q", c.method, got, c.name)
		}
	}
}

func TestSelectUnknownAdapter(t *testing.T) {
	a, ok := pkg.Select("apt")
	if ok {
		t.Error("Select(apt) ok = true, want false")
	}
	if a != nil {
		t.Errorf("Select(apt) = %v, want nil", a)
	}
}
