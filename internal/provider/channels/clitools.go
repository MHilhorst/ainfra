package channels

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

// Apply executes the channel plan. For Create/Update it selects a package
// adapter from the install payload and installs if not already present. If no
// supported adapter is declared it runs a declare-and-check probe. Delete
// changes are a no-op — ainfra does not uninstall CLI tools (see design §6).
// Honors env.DryRun.
func (CLITools) Apply(env provider.Env, plan provider.ChannelPlan) (provider.ApplyResult, error) {
	var applied []provider.Change

	for _, c := range plan.Changes {
		if c.Kind == provider.ChangeNoop {
			continue
		}

		if c.Kind == provider.ChangeDelete {
			// ainfra does not uninstall CLI tools; treat as a no-op.
			applied = append(applied, c)
			continue
		}

		// ChangeCreate or ChangeUpdate
		id := c.ID

		installMap, _ := c.Resource.Payload["install"].(map[string]any)

		var matched provider.Change
		handled := false

		for _, method := range slices.Sorted(maps.Keys(installMap)) {
			adapter, ok := pkg.Select(method)
			if !ok {
				continue
			}

			if !env.DryRun {
				installed, err := adapter.IsInstalled(env, id)
				if err != nil {
					return provider.ApplyResult{}, fmt.Errorf("cliTools: checking %q via %s: %w", id, adapter.Name(), err)
				}
				if !installed {
					if err := adapter.Install(env, id); err != nil {
						return provider.ApplyResult{}, fmt.Errorf("cliTools: installing %q via %s: %w", id, adapter.Name(), err)
					}
				}
			}

			matched = c
			handled = true
			break
		}

		if handled {
			applied = append(applied, matched)
			continue
		}

		// No recognised adapter — declare-and-check fallback.
		if !env.DryRun {
			if _, err := env.Runner.Run(id, "--version"); err != nil {
				return provider.ApplyResult{}, fmt.Errorf("cliTools: %q is not installed and no supported install method is declared; install it manually", id)
			}
		}
		applied = append(applied, c)
	}

	return provider.ApplyResult{
		Channel: "cliTools",
		Applied: applied,
	}, nil
}
