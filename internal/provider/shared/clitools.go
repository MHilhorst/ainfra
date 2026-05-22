package shared

import (
	"errors"
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

// Observe reports the CLI tools ainfra manages, sourced from the applied
// ledger. A tool's presence on the machine cannot be probed without the
// manifest's check command, so the ledger is the authoritative record of what
// was applied; returning it keeps `check` idempotent. ContentHash is left
// empty — the orchestrator backfills it from the ledger. (A tool removed from
// the machine by hand after an apply is not detected here.)
func (CLITools) Observe(env provider.Env) ([]provider.Resource, error) {
	applied, err := provider.ReadApplied(env.Root)
	if err != nil {
		return nil, err
	}
	managed := provider.ResourcesByChannel(applied)["cliTools"]
	out := make([]provider.Resource, 0, len(managed))
	for _, r := range managed {
		out = append(out, provider.Resource{ID: r.ID, Channel: "cliTools"})
	}
	return out, nil
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

// Apply executes the channel plan. Each Create/Update entry is applied
// independently: it selects a package adapter from the install payload and
// installs if not already present, or runs a declare-and-check probe (using
// the manifest check.command) when no supported adapter is declared. A failed
// entry is recorded in ApplyResult.Failed and does not abort its siblings.
// Delete changes are a no-op — ainfra does not uninstall CLI tools (see design
// §6). Honors env.DryRun and env.NoInstall (both skip the install and probe).
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

// applyOne installs a single CLI tool: it selects the first install method
// pkg.Select recognises and installs the tool if absent, or runs the
// declare-and-check probe (the manifest check.command, or "<id> --version")
// when no supported adapter is declared. It returns nil when the tool is
// present (or env.DryRun/env.NoInstall suppressed the work).
func applyOne(env provider.Env, c provider.Change) error {
	id := c.ID
	methods := installMethods(c.Resource.Payload)

	for _, method := range slices.Sorted(maps.Keys(methods)) {
		adapter, ok := pkg.Select(method)
		if !ok {
			continue
		}
		if !env.DryRun && !env.NoInstall {
			spec := methods[method]
			installed, err := adapter.IsInstalled(env, spec)
			if err != nil {
				return fmt.Errorf("checking install state via %s failed: %w", adapter.Name(), err)
			}
			if !installed {
				if err := adapter.Install(env, spec); err != nil {
					return fmt.Errorf("install via %s failed: %w", adapter.Name(), err)
				}
			}
		}
		return nil
	}

	// No recognised adapter — declare-and-check fallback using check.command.
	if !env.DryRun && !env.NoInstall {
		bin, args := probeCommand(id, c.Resource.Payload)
		if _, err := env.Runner.Run(bin, args...); err != nil {
			return errors.New("not installed and no supported install method is declared; install it manually")
		}
	}
	return nil
}
