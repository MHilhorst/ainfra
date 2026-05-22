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

func TestBrewAdapterIsInstalled_Formula_true(t *testing.T) {
	runner := provider.NewFakeRunner()
	runner.Script["brew list --versions mysql-client"] = provider.FakeResult{Output: []byte("mysql-client 8.0\n")}
	env := provider.Env{Runner: runner}

	ok, err := pkg.BrewAdapter{}.IsInstalled(env, map[string]any{"formula": "mysql-client"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("expected IsInstalled = true")
	}
	if len(runner.Calls) != 1 || runner.Calls[0] != "brew list --versions mysql-client" {
		t.Errorf("unexpected calls: %v", runner.Calls)
	}
}

func TestBrewAdapterIsInstalled_Formula_false(t *testing.T) {
	runner := provider.NewFakeRunner()
	runner.Script["brew list --versions missing"] = provider.FakeResult{Err: errors.New("not found")}
	env := provider.Env{Runner: runner}

	ok, err := pkg.BrewAdapter{}.IsInstalled(env, map[string]any{"formula": "missing"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("expected IsInstalled = false")
	}
}

func TestBrewAdapterIsInstalled_Cask(t *testing.T) {
	runner := provider.NewFakeRunner()
	runner.Script["brew list --cask --versions 1password-cli"] = provider.FakeResult{Output: []byte("1password-cli 2.0\n")}
	env := provider.Env{Runner: runner}

	ok, err := pkg.BrewAdapter{}.IsInstalled(env, map[string]any{"cask": "1password-cli"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("expected IsInstalled = true for cask")
	}
	if len(runner.Calls) != 1 || runner.Calls[0] != "brew list --cask --versions 1password-cli" {
		t.Errorf("unexpected calls: %v", runner.Calls)
	}
}

func TestBrewAdapterInstall_Formula(t *testing.T) {
	runner := provider.NewFakeRunner()
	runner.Script["brew install mysql-client"] = provider.FakeResult{}
	env := provider.Env{Runner: runner}

	a := pkg.BrewAdapter{}
	if err := a.Install(env, map[string]any{"formula": "mysql-client"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(runner.Calls) != 1 || runner.Calls[0] != "brew install mysql-client" {
		t.Errorf("unexpected calls: %v", runner.Calls)
	}
}

func TestBrewAdapterInstall_Cask(t *testing.T) {
	runner := provider.NewFakeRunner()
	runner.Script["brew install --cask 1password-cli"] = provider.FakeResult{}
	env := provider.Env{Runner: runner}

	a := pkg.BrewAdapter{}
	if err := a.Install(env, map[string]any{"cask": "1password-cli"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(runner.Calls) != 1 || runner.Calls[0] != "brew install --cask 1password-cli" {
		t.Errorf("unexpected calls: %v", runner.Calls)
	}
}

func TestBrewAdapter_MissingSpec(t *testing.T) {
	runner := provider.NewFakeRunner()
	env := provider.Env{Runner: runner}

	_, err := pkg.BrewAdapter{}.IsInstalled(env, map[string]any{})
	if err == nil {
		t.Error("expected error for missing formula/cask in spec")
	}

	err = pkg.BrewAdapter{}.Install(env, map[string]any{})
	if err == nil {
		t.Error("expected error for missing formula/cask in spec")
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

	ok, err := pkg.NpmAdapter{}.IsInstalled(env, map[string]any{"package": "typescript"})
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

	ok, err := pkg.NpmAdapter{}.IsInstalled(env, map[string]any{"package": "missing"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("expected IsInstalled = false")
	}
}

func TestNpmAdapterInstall_WithVersion(t *testing.T) {
	runner := provider.NewFakeRunner()
	runner.Script["npm install -g x@1.1.1"] = provider.FakeResult{}
	env := provider.Env{Runner: runner}

	na := pkg.NpmAdapter{}
	if err := na.Install(env, map[string]any{"package": "x", "version": "1.1.1"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(runner.Calls) != 1 || runner.Calls[0] != "npm install -g x@1.1.1" {
		t.Errorf("unexpected calls: %v", runner.Calls)
	}
}

func TestNpmAdapterInstall_WithoutVersion(t *testing.T) {
	runner := provider.NewFakeRunner()
	runner.Script["npm install -g x"] = provider.FakeResult{}
	env := provider.Env{Runner: runner}

	na := pkg.NpmAdapter{}
	if err := na.Install(env, map[string]any{"package": "x"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(runner.Calls) != 1 || runner.Calls[0] != "npm install -g x" {
		t.Errorf("unexpected calls: %v", runner.Calls)
	}
}

func TestNpmAdapter_MissingPackage(t *testing.T) {
	runner := provider.NewFakeRunner()
	env := provider.Env{Runner: runner}

	_, err := pkg.NpmAdapter{}.IsInstalled(env, map[string]any{})
	if err == nil {
		t.Error("expected error for missing package in spec")
	}

	err = pkg.NpmAdapter{}.Install(env, map[string]any{})
	if err == nil {
		t.Error("expected error for missing package in spec")
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
