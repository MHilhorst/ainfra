package main

import (
	"path/filepath"
	"testing"

	"github.com/MHilhorst/ainfra/internal/provider"
	"github.com/MHilhorst/ainfra/internal/resolve"
)

// TestRenderChannelContract renders a broad manifest and feeds every rendered
// resource into its channel provider's Apply. A renderer/provider payload
// mismatch makes the change silently drop from ApplyResult.Applied — this test
// fails when that happens. cliTools runs under NoInstall so no real package
// manager is invoked; marketplaces and plugins are covered by their own unit
// tests (their Apply shells out to the `claude` CLI).
func TestRenderChannelContract(t *testing.T) {
	dir := t.TempDir()
	copyTestdata(t, filepath.Join("testdata", "representative"), dir)

	rendered, err := resolve.RenderResources(dir)
	if err != nil {
		t.Fatalf("RenderResources: %v", err)
	}

	providers, err := providersForDir(dir)
	if err != nil {
		t.Fatalf("providersForDir: %v", err)
	}
	byChannel := map[string]provider.Provider{}
	for _, p := range providers {
		byChannel[p.Channel()] = p
	}

	// Channels whose Apply writes files or runs no command under NoInstall.
	// marketplaces and plugins are excluded — their Apply requires the `claude`
	// CLI; they are covered by internal/provider/claudecode unit tests.
	contractChannels := []string{
		"cliTools", "mcpServers", "hooks", "commands", "rules", "tools",
	}

	for _, ch := range contractChannels {
		resources := rendered[ch]
		if len(resources) == 0 {
			t.Errorf("channel %q rendered no resources; fixture should exercise it", ch)
			continue
		}
		p, ok := byChannel[ch]
		if !ok {
			t.Errorf("no provider registered for channel %q", ch)
			continue
		}
		for _, r := range resources {
			t.Run(ch+"/"+r.ID, func(t *testing.T) {
				env := provider.Env{
					FS:        provider.NewMemFilesystem(),
					Runner:    provider.NewFakeRunner(),
					Root:      dir,
					Home:      filepath.Join(dir, "home"),
					NoInstall: true,
				}
				plan := provider.ChannelPlan{
					Channel: ch,
					Changes: []provider.Change{{
						Kind:     provider.ChangeCreate,
						ID:       r.ID,
						Resource: r,
					}},
				}
				res, err := p.Apply(env, plan)
				if err != nil {
					t.Fatalf("%s/%s: Apply returned error: %v", ch, r.ID, err)
				}
				if len(res.Failed) != 0 {
					t.Errorf("%s/%s: Apply reported failures: %+v", ch, r.ID, res.Failed)
				}
				if len(res.Applied) != 1 {
					t.Errorf("%s/%s: rendered payload not consumed — Applied=%d, want 1 (renderer/provider contract mismatch?)",
						ch, r.ID, len(res.Applied))
				}
			})
		}
	}
}
