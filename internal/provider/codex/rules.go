package codex

import (
	"path/filepath"

	"github.com/MHilhorst/ainfra/internal/provider"
	"github.com/MHilhorst/ainfra/internal/provider/fsmerge"
)

// Rules reconciles rule content into an ainfra-managed region of the
// repository's AGENTS.md file — the instruction file the Codex CLI reads.
type Rules struct{}

// Channel returns the channel name this provider manages.
func (Rules) Channel() string { return "rules" }

func agentsPath(env provider.Env) string {
	return filepath.Join(env.Root, "AGENTS.md")
}

// Observe reads AGENTS.md and returns a Resource for each rule in the
// ainfra-managed region. A missing file or absent region is treated as no
// resources. ContentHash is left empty; the orchestrator backfills it.
func (Rules) Observe(env provider.Env) ([]provider.Resource, error) {
	ids, err := fsmerge.ManagedRegionIDs(env.FS, agentsPath(env))
	if err != nil {
		return nil, err
	}
	resources := make([]provider.Resource, 0, len(ids))
	for _, id := range ids {
		resources = append(resources, provider.Resource{ID: id, Channel: "rules"})
	}
	return resources, nil
}

// Apply executes the channel plan against the managed region of AGENTS.md.
// When env.DryRun is true the result is computed but the file is not written.
func (Rules) Apply(env provider.Env, plan provider.ChannelPlan) (provider.ApplyResult, error) {
	blocks := map[string]string{}
	ownedIDs := make([]string, 0, len(plan.Changes))
	var applied []provider.Change

	for _, c := range plan.Changes {
		if c.Kind == provider.ChangeNoop {
			continue
		}
		ownedIDs = append(ownedIDs, c.ID)
		applied = append(applied, c)
		if c.Kind == provider.ChangeCreate || c.Kind == provider.ChangeUpdate {
			content, _ := c.Resource.Payload["content"].(string)
			blocks[c.ID] = content
		}
		// ChangeDelete: in ownedIDs, not in blocks — the merge removes it.
	}

	if len(ownedIDs) == 0 {
		return provider.ApplyResult{Channel: "rules"}, nil
	}

	if !env.DryRun {
		if err := fsmerge.MergeManagedRegion(env.FS, agentsPath(env), blocks, ownedIDs); err != nil {
			return provider.ApplyResult{}, err
		}
	}

	return provider.ApplyResult{Channel: "rules", Applied: applied}, nil
}
