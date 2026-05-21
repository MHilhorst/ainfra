package ui

import (
	"bytes"
	"strings"
	"testing"

	"github.com/MHilhorst/ainfra/internal/provider"
)

func TestRenderPlanMixedChanges(t *testing.T) {
	plans := map[string]provider.ChannelPlan{
		"mcpServers": {
			Channel: "mcpServers",
			Changes: []provider.Change{
				{Kind: provider.ChangeCreate, ID: "analytics", Detail: "new server"},
				{Kind: provider.ChangeNoop, ID: "existing", Detail: ""},
			},
		},
		"hooks": {
			Channel: "hooks",
			Changes: []provider.Change{
				{Kind: provider.ChangeUpdate, ID: "pre-commit", Detail: "timeout changed"},
				{Kind: provider.ChangeDelete, ID: "old-hook", Detail: ""},
			},
		},
	}

	var b bytes.Buffer
	c := Colorizer{}
	RenderPlan(&b, c, plans)
	out := b.String()

	// hooks comes before mcpServers alphabetically
	if !strings.Contains(out, "  + mcpServers.analytics") {
		t.Errorf("missing add line, got:\n%s", out)
	}
	if !strings.Contains(out, "  ~ hooks.pre-commit") {
		t.Errorf("missing change line, got:\n%s", out)
	}
	if !strings.Contains(out, "  - hooks.old-hook") {
		t.Errorf("missing delete line, got:\n%s", out)
	}
	if strings.Contains(out, "existing") {
		t.Errorf("noop change should not appear, got:\n%s", out)
	}
	if !strings.Contains(out, "1 to add") {
		t.Errorf("summary missing add count, got:\n%s", out)
	}
	if !strings.Contains(out, "1 to change") {
		t.Errorf("summary missing change count, got:\n%s", out)
	}
	if !strings.Contains(out, "1 to destroy") {
		t.Errorf("summary missing destroy count, got:\n%s", out)
	}
}

func TestRenderPlanSortedOrder(t *testing.T) {
	plans := map[string]provider.ChannelPlan{
		"zzz": {
			Channel: "zzz",
			Changes: []provider.Change{
				{Kind: provider.ChangeCreate, ID: "z-item", Detail: ""},
			},
		},
		"aaa": {
			Channel: "aaa",
			Changes: []provider.Change{
				{Kind: provider.ChangeCreate, ID: "a-item", Detail: ""},
			},
		},
	}

	var b bytes.Buffer
	RenderPlan(&b, Colorizer{}, plans)
	out := b.String()

	posA := strings.Index(out, "aaa.a-item")
	posZ := strings.Index(out, "zzz.z-item")
	if posA < 0 || posZ < 0 {
		t.Fatalf("expected both items in output, got:\n%s", out)
	}
	if posA > posZ {
		t.Errorf("channels not in sorted order, aaa appears after zzz")
	}
}

func TestRenderPlanNoChanges(t *testing.T) {
	plans := map[string]provider.ChannelPlan{
		"mcpServers": {
			Channel: "mcpServers",
			Changes: []provider.Change{
				{Kind: provider.ChangeNoop, ID: "server1"},
			},
		},
	}

	var b bytes.Buffer
	RenderPlan(&b, Colorizer{}, plans)
	out := b.String()

	want := "No changes. Environment matches the lockfile."
	if !strings.Contains(out, want) {
		t.Errorf("expected %q, got:\n%s", want, out)
	}
}

func TestRenderPlanEmptyMap(t *testing.T) {
	var b bytes.Buffer
	RenderPlan(&b, Colorizer{}, map[string]provider.ChannelPlan{})
	out := b.String()

	want := "No changes. Environment matches the lockfile."
	if !strings.Contains(out, want) {
		t.Errorf("expected %q, got:\n%s", want, out)
	}
}

func TestRenderPlanDetailIncluded(t *testing.T) {
	plans := map[string]provider.ChannelPlan{
		"hooks": {
			Channel: "hooks",
			Changes: []provider.Change{
				{Kind: provider.ChangeUpdate, ID: "pre-push", Detail: "command changed"},
			},
		},
	}

	var b bytes.Buffer
	RenderPlan(&b, Colorizer{}, plans)
	out := b.String()

	if !strings.Contains(out, "command changed") {
		t.Errorf("detail not included in output, got:\n%s", out)
	}
}
