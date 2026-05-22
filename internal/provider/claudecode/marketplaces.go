package claudecode

import (
	"encoding/json"
	"errors"
	iofs "io/fs"
	"path/filepath"
	"strings"

	"github.com/MHilhorst/ainfra/internal/provider"
)

// Marketplaces registers and reconciles Claude Code plugin marketplaces via the
// `claude` CLI. Resource.Payload key consumed: "source" (string).
type Marketplaces struct{}

// Channel returns the channel name this provider manages.
func (Marketplaces) Channel() string { return "marketplaces" }

// knownMarketplacesPath returns the path to Claude Code's known_marketplaces.json
// under env.Home.
func knownMarketplacesPath(env provider.Env) string {
	return filepath.Join(env.Home, ".claude", "plugins", "known_marketplaces.json")
}

// Observe reads known_marketplaces.json and returns a Resource per registered
// marketplace keyed by its name. A missing file means no resources.
func (Marketplaces) Observe(env provider.Env) ([]provider.Resource, error) {
	raw, err := env.FS.ReadFile(knownMarketplacesPath(env))
	if errors.Is(err, iofs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}

	resources := make([]provider.Resource, 0, len(doc))
	for name := range doc {
		resources = append(resources, provider.Resource{
			ID:      name,
			Channel: "marketplaces",
		})
	}
	return resources, nil
}

// Apply executes the channel plan for marketplaces. Create runs
// `claude plugin marketplace add <source>`; Delete runs
// `claude plugin marketplace remove <name>`. "Already exists" / "already added"
// stderr is treated as success for idempotency. Honors env.DryRun.
func (Marketplaces) Apply(env provider.Env, plan provider.ChannelPlan) (provider.ApplyResult, error) {
	var applied []provider.Change

	for _, c := range plan.Changes {
		if c.Kind == provider.ChangeNoop {
			continue
		}

		if !env.DryRun {
			switch c.Kind {
			case provider.ChangeCreate, provider.ChangeUpdate:
				source, _ := c.Resource.Payload["source"].(string)
				_, err := env.Runner.Run("claude", "plugin", "marketplace", "add", source)
				if err != nil && !isAlreadyRegisteredError(err) {
					return provider.ApplyResult{}, err
				}
			case provider.ChangeDelete:
				if _, err := env.Runner.Run("claude", "plugin", "marketplace", "remove", c.ID); err != nil {
					return provider.ApplyResult{}, err
				}
			}
		}

		applied = append(applied, c)
	}

	return provider.ApplyResult{
		Channel: "marketplaces",
		Applied: applied,
	}, nil
}

// isAlreadyRegisteredError reports whether the error from `claude plugin
// marketplace add` indicates the marketplace is already registered.
func isAlreadyRegisteredError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "already exists") || strings.Contains(msg, "already added")
}
