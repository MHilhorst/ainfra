package audit

import (
	"testing"

	"github.com/MHilhorst/ainfra/internal/lockfile"
	"github.com/MHilhorst/ainfra/internal/manifest"
)

func TestReconcile_ManagedFromRepoLockfile(t *testing.T) {
	rows := []Row{
		{Layer: LayerProject, Channel: "skills", ID: "foo", Status: Status{Unmanaged: true}},
	}
	committed := &lockfile.Lock{
		Entries: lockfile.Entries{
			Skills: map[string]lockfile.Entry{
				"foo": {Layer: "repo", Version: "1.0.0"},
			},
		},
	}
	out := Reconcile(rows, nil, committed, nil)
	if !out[0].Status.Managed || out[0].Status.Unmanaged {
		t.Fatalf("expected managed, got %+v", out[0].Status)
	}
	if out[0].Source != "from: repo manifest" {
		t.Fatalf("expected repo source, got %q", out[0].Source)
	}
	if out[0].Version != "1.0.0" {
		t.Fatalf("expected version copied across, got %q", out[0].Version)
	}
}

func TestReconcile_TeamSourceUsesManifestExtends(t *testing.T) {
	rows := []Row{
		{Layer: LayerProject, Channel: "skills", ID: "x", Status: Status{Unmanaged: true}},
	}
	committed := &lockfile.Lock{
		Entries: lockfile.Entries{
			Skills: map[string]lockfile.Entry{"x": {Layer: "team"}},
		},
	}
	layers := map[manifest.Layer]*manifest.Manifest{
		manifest.LayerRepo: {Extends: []manifest.Source{{Location: "github:org/cfg@1.0"}}},
	}
	out := Reconcile(rows, layers, committed, nil)
	if out[0].Source != "from: github:org/cfg@1.0" {
		t.Errorf("expected team source from extends, got %q", out[0].Source)
	}
}

func TestReconcile_UnmanagedWhenAbsentFromLock(t *testing.T) {
	rows := []Row{
		{Layer: LayerGlobal, Channel: "plugins", ID: "stray", Status: Status{Unmanaged: true}},
	}
	out := Reconcile(rows, nil, nil, nil)
	if !out[0].Status.Unmanaged || out[0].Status.Managed {
		t.Fatalf("expected unmanaged, got %+v", out[0].Status)
	}
	if out[0].Source != "" {
		t.Errorf("expected empty source on unmanaged, got %q", out[0].Source)
	}
}

func TestReconcile_ShadowingGlobalByProject(t *testing.T) {
	rows := []Row{
		{Layer: LayerGlobal, Channel: "skills", ID: "foo", Status: Status{Unmanaged: true}},
		{Layer: LayerProject, Channel: "skills", ID: "foo", Status: Status{Unmanaged: true}},
	}
	out := Reconcile(rows, nil, nil, nil)
	var g, p Row
	for _, r := range out {
		if r.Layer == LayerGlobal {
			g = r
		}
		if r.Layer == LayerProject {
			p = r
		}
	}
	if !g.Status.Shadowed || g.ShadowedBy != "project" {
		t.Errorf("expected global shadowed-by project, got %+v", g)
	}
	if p.Status.Shadowed {
		t.Errorf("expected project not shadowed, got %+v", p)
	}
}

func TestBuildFooter_AdoptableSuggestsUserScope(t *testing.T) {
	rows := []Row{
		{Layer: LayerGlobal, Channel: "mcpServers", ID: "a", Status: Status{Unmanaged: true}},
		{Layer: LayerGlobal, Channel: "mcpServers", ID: "b", Status: Status{Unmanaged: true}},
	}
	f := BuildFooter(rows)
	if f.Adoptable != 2 || f.Suggested != "ainfra adopt --scope=user" || f.Healthy {
		t.Fatalf("unexpected footer: %+v", f)
	}
}

func TestBuildFooter_AdoptableOnlyCountsAdoptableChannels(t *testing.T) {
	rows := []Row{
		{Layer: LayerGlobal, Channel: "mcpServers", ID: "a", Status: Status{Unmanaged: true}},
		{Layer: LayerGlobal, Channel: "plugins", ID: "p", Status: Status{Unmanaged: true}},
		{Layer: LayerGlobal, Channel: "skills", ID: "s", Status: Status{Unmanaged: true}},
	}
	f := BuildFooter(rows)
	if f.Adoptable != 1 {
		t.Errorf("expected only adoptable channels counted (mcpServers); got Adoptable=%d", f.Adoptable)
	}
}

func TestBuildFooter_HealthyWhenAllManaged(t *testing.T) {
	rows := []Row{
		{Layer: LayerGlobal, Channel: "skills", ID: "x", Status: Status{Managed: true}},
	}
	f := BuildFooter(rows)
	if !f.Healthy || f.Suggested != "" {
		t.Errorf("expected healthy, no suggestion; got %+v", f)
	}
}

func TestBuildFooter_NoConfigDetected(t *testing.T) {
	f := BuildFooter(nil)
	if !f.NoConfigDetected {
		t.Errorf("expected NoConfigDetected on empty rows, got %+v", f)
	}
}
