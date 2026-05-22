package shared

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/MHilhorst/ainfra/internal/provider"
	"github.com/MHilhorst/ainfra/internal/provider/pkg"
)

// CLITools reconciles CLI tool installations. It delegates install/probe to a
// PackageAdapter when a recognised method is declared in the resource payload.
// Resource.Payload keys: "install" (map of method -> spec; the first key whose
// method pkg.Select recognises is used) and "check" (optional — its "command"
// drives the declare-and-check probe when no adapter matches).
type CLITools struct{}

// Channel returns the channel name this provider manages.
func (CLITools) Channel() string { return "cliTools" }

// Observe returns nil, nil. Installs are idempotent; the orchestrator will plan
// a create, and Apply re-checks before installing.
func (CLITools) Observe(_ provider.Env) ([]provider.Resource, error) {
	return nil, nil
}

// installMethods reads the "install" payload as a method -> spec map. It
// tolerates either decode path: a map[string]map[string]any (the manifest
// type) or a map[string]any whose values are themselves map[string]any.
func installMethods(payload map[string]any) map[string]map[string]any {
	out := map[string]map[string]any{}
	switch m := payload["install"].(type) {
	case map[string]map[string]any:
		return m
	case map[string]any:
		for method, v := range m {
			if spec, ok := v.(map[string]any); ok {
				out[method] = spec
			}
		}
	}
	return out
}

// probeCommand returns the declare-and-check probe for a tool: the manifest
// check.command split into binary + args, or "<id> --version" if no check
// command is declared.
func probeCommand(id string, payload map[string]any) (string, []string) {
	if check, ok := payload["check"].(map[string]any); ok {
		if cmd, ok := check["command"].(string); ok {
			if fields := strings.Fields(cmd); len(fields) > 0 {
				return fields[0], fields[1:]
			}
		}
	}
	return id, []string{"--version"}
}

// Apply executes the channel plan. For Create/Update it selects a package
// adapter from the install payload and installs if not already present. If no
// supported adapter is declared it runs a declare-and-check probe using the
// manifest check.command. Delete changes are a no-op — ainfra does not
// uninstall CLI tools (see design §6). Honors env.DryRun.
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
		methods := installMethods(c.Resource.Payload)

		handled := false
		for _, method := range slices.Sorted(maps.Keys(methods)) {
			adapter, ok := pkg.Select(method)
			if !ok {
				continue
			}

			if !env.DryRun {
				spec := methods[method]
				installed, err := adapter.IsInstalled(env, spec)
				if err != nil {
					return provider.ApplyResult{}, fmt.Errorf("cliTools: checking %q via %s: %w", id, adapter.Name(), err)
				}
				if !installed {
					if err := adapter.Install(env, spec); err != nil {
						return provider.ApplyResult{}, fmt.Errorf("cliTools: installing %q via %s: %w", id, adapter.Name(), err)
					}
				}
			}

			handled = true
			break
		}

		if handled {
			applied = append(applied, c)
			continue
		}

		// No recognised adapter — declare-and-check fallback using check.command.
		if !env.DryRun {
			bin, args := probeCommand(id, c.Resource.Payload)
			if _, err := env.Runner.Run(bin, args...); err != nil {
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
