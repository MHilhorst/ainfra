package main

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/MHilhorst/ainfra/internal/provider"
	"github.com/MHilhorst/ainfra/internal/resolve"
)

// TestRenderChannelContract renders a broad manifest and feeds every rendered
// resource into its channel provider's Apply. A renderer/provider payload
// mismatch makes the change silently drop from ApplyResult.Applied — this test
// fails when that happens. The test is hermetic: env.Runner is a scripted
// FakeRunner (no real package manager runs) and env.FS is in-memory (no real
// disk writes).
//
// Four channels are excluded from contractChannels:
//   - marketplaces and plugins: their Apply shells out to the `claude` CLI;
//     covered by internal/provider/claudecode unit tests.
//   - skills: Apply needs env.Fetch wired to a real or fake fetcher, not
//     supplied here; can be added once the fixture/wiring supports it.
//   - backgroundServices: the fixture declares no backgroundServices and its
//     template's produces: block emits only an mcpServer, so the channel renders zero resources; add once fixture covers it.
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

	// See the function comment for which channels are excluded and why.
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
				runner := provider.NewFakeRunner()
				// The fixture's cliTool is `jq` installed via brew. Script its adapter
				// commands so the cliTools channel exercises the full payload -> adapter
				// path for real; the fake runner keeps it hermetic (no real brew runs).
				runner.Script["brew list --versions jq"] = provider.FakeResult{Err: errors.New("not installed")}
				runner.Script["brew install jq"] = provider.FakeResult{Output: []byte("ok")}
				env := provider.Env{
					FS:     provider.NewMemFilesystem(),
					Runner: runner,
					Root:   dir,
					Home:   filepath.Join(dir, "home"),
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
