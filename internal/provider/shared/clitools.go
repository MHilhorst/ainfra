package shared

import (
	"fmt"
	"maps"
	"slices"

	"github.com/MHilhorst/ainfra/internal/provider"
	"github.com/MHilhorst/ainfra/internal/provider/pkg"
)

// CLITools reconciles CLI tool installations. It delegates install/probe to a
// PackageAdapter when a recognised method is declared in the resource payload.
// Resource.Payload keys: "install" (map[string]any of method -> spec; the first
// key whose method pkg.Select recognises is used) and "check" (optional).
type CLITools struct{}

// Channel returns the channel name this provider manages.
func (CLITools) Channel() string { return "cliTools" }

// Observe returns nil, nil. Installs are idempotent; the orchestrator will plan
// a create, and Apply re-checks before installing.
func (CLITools) Observe(_ provider.Env) ([]provider.Resource, error) {
	return nil, nil
}

// Apply executes the channel plan. Each Create/Update entry is applied
// independently: it selects a package adapter from the install payload and
// installs if not already present, or runs a declare-and-check probe when no
// supported adapter is declared. A failed entry is recorded in
// ApplyResult.Failed and does not abort its siblings. Delete changes are a
// no-op — ainfra does not uninstall CLI tools (see design §6). Honors
// env.DryRun and env.NoInstall (both skip the install and probe).
func (CLITools) Apply(env provider.Env, plan provider.ChannelPlan) (provider.ApplyResult, error) {
	var applied []provider.Change
	var failed []provider.ChangeFailure

	for _, c := range plan.Changes {
		if c.Kind == provider.ChangeNoop {
			continue
		}
		if c.Kind == provider.ChangeDelete {
			// ainfra does not uninstall CLI tools; treat as a no-op.
			applied = append(applied, c)
			continue
		}
		if err := applyOne(env, c); err != nil {
			failed = append(failed, provider.ChangeFailure{Change: c, Err: err})
			continue
		}
		applied = append(applied, c)
	}

	return provider.ApplyResult{
		Channel: "cliTools",
		Applied: applied,
		Failed:  failed,
	}, nil
}

// applyOne installs a single CLI tool. It selects the first install method
// pkg.Select recognises and installs the tool if absent; if no supported
// adapter is declared it runs a `<tool> --version` probe. It returns nil when
// the tool is present (or env.DryRun/env.NoInstall suppressed the work).
func applyOne(env provider.Env, c provider.Change) error {
	id := c.ID
	installMap, _ := c.Resource.Payload["install"].(map[string]any)

	for _, method := range slices.Sorted(maps.Keys(installMap)) {
		adapter, ok := pkg.Select(method)
		if !ok {
			continue
		}
		if !env.DryRun && !env.NoInstall {
			installed, err := adapter.IsInstalled(env, id)
			if err != nil {
				return fmt.Errorf("checking install state via %s failed: %w", adapter.Name(), err)
			}
			if !installed {
				if err := adapter.Install(env, id); err != nil {
					return fmt.Errorf("install via %s failed: %w", adapter.Name(), err)
				}
			}
		}
		return nil
	}

	// No recognised adapter — declare-and-check fallback.
	if !env.DryRun && !env.NoInstall {
		if _, err := env.Runner.Run(id, "--version"); err != nil {
			return fmt.Errorf("not installed and no supported install method is declared; install it manually")
		}
	}
	return nil
}
