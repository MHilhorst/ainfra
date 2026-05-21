package manifest

import "testing"

func TestResolveAgentDefaultsToClaudeCode(t *testing.T) {
	layers := map[Layer]*Manifest{
		LayerRepo: {Version: 1},
	}
	id, _, explicit := ResolveAgent(layers)
	if id != "claude-code" {
		t.Errorf("id = %q, want claude-code", id)
	}
	if explicit {
		t.Error("explicit = true, want false when no layer sets agent")
	}
}

func TestResolveAgentUsesPersonalWhenRepoSilent(t *testing.T) {
	layers := map[Layer]*Manifest{
		LayerRepo:     {Version: 1},
		LayerPersonal: {Version: 1, Agent: "codex"},
	}
	id, layer, explicit := ResolveAgent(layers)
	if id != "codex" {
		t.Errorf("id = %q, want codex", id)
	}
	if layer != LayerPersonal {
		t.Errorf("layer = %q, want personal", layer)
	}
	if !explicit {
		t.Error("explicit = false, want true")
	}
}

func TestResolveAgentRepoBeatsPersonal(t *testing.T) {
	layers := map[Layer]*Manifest{
		LayerRepo:     {Version: 1, Agent: "claude-code"},
		LayerPersonal: {Version: 1, Agent: "codex"},
	}
	id, layer, _ := ResolveAgent(layers)
	if id != "claude-code" {
		t.Errorf("id = %q, want claude-code (repo outranks personal)", id)
	}
	if layer != LayerRepo {
		t.Errorf("layer = %q, want repo", layer)
	}
}
