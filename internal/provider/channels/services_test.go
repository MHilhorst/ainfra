package channels_test

import (
	"strings"
	"testing"

	"github.com/MHilhorst/ainfra/internal/provider"
	"github.com/MHilhorst/ainfra/internal/provider/channels"
)

func TestServicesChannel(t *testing.T) {
	p := channels.Services{}
	if got := p.Channel(); got != "backgroundServices" {
		t.Fatalf("Channel() = %q, want %q", got, "backgroundServices")
	}
}

func TestServicesObserve_Empty(t *testing.T) {
	mem := provider.NewMemFilesystem()
	env := provider.Env{FS: mem, Root: "/repo"}

	p := channels.Services{}
	resources, err := p.Observe(env)
	if err != nil {
		t.Fatalf("Observe: unexpected error: %v", err)
	}
	if len(resources) != 0 {
		t.Fatalf("Observe: got %d resources, want 0", len(resources))
	}
}

func TestServicesObserve_WithServices(t *testing.T) {
	mem := provider.NewMemFilesystem()
	env := provider.Env{FS: mem, Root: "/repo"}

	// create two service directories
	if err := mem.MkdirAll("/repo/.ainfra/services/svc-a", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := mem.MkdirAll("/repo/.ainfra/services/svc-b", 0o755); err != nil {
		t.Fatal(err)
	}

	p := channels.Services{}
	resources, err := p.Observe(env)
	if err != nil {
		t.Fatalf("Observe: unexpected error: %v", err)
	}
	if len(resources) != 2 {
		t.Fatalf("Observe: got %d resources, want 2", len(resources))
	}

	ids := map[string]bool{}
	for _, r := range resources {
		ids[r.ID] = true
		if r.Channel != "backgroundServices" {
			t.Errorf("resource %q: Channel = %q, want %q", r.ID, r.Channel, "backgroundServices")
		}
	}
	if !ids["svc-a"] {
		t.Error("expected resource with id 'svc-a'")
	}
	if !ids["svc-b"] {
		t.Error("expected resource with id 'svc-b'")
	}
}

func TestServicesObserve_IgnoresFiles(t *testing.T) {
	mem := provider.NewMemFilesystem()
	env := provider.Env{FS: mem, Root: "/repo"}

	// one real service directory
	if err := mem.MkdirAll("/repo/.ainfra/services/real-svc", 0o755); err != nil {
		t.Fatal(err)
	}
	// one stray file directly under services/
	if err := mem.WriteFile("/repo/.ainfra/services/.gitkeep", []byte{}, 0o644); err != nil {
		t.Fatal(err)
	}

	p := channels.Services{}
	resources, err := p.Observe(env)
	if err != nil {
		t.Fatalf("Observe: unexpected error: %v", err)
	}
	if len(resources) != 1 {
		t.Fatalf("Observe: got %d resources, want 1 (stray file must be skipped); resources = %v", len(resources), resources)
	}
	if resources[0].ID != "real-svc" {
		t.Errorf("Observe: resource ID = %q, want %q", resources[0].ID, "real-svc")
	}
}

func TestServicesApply_Create(t *testing.T) {
	mem := provider.NewMemFilesystem()
	env := provider.Env{FS: mem, Root: "/repo"}

	plan := provider.ChannelPlan{
		Channel: "backgroundServices",
		Changes: []provider.Change{
			{
				Kind: provider.ChangeCreate,
				ID:   "my-svc",
				Resource: provider.Resource{
					ID:      "my-svc",
					Channel: "backgroundServices",
					Payload: map[string]any{
						"kind": "worker",
						"spec": map[string]any{
							"command": "node worker.js",
						},
					},
				},
			},
		},
	}

	p := channels.Services{}
	result, err := p.Apply(env, plan)
	if err != nil {
		t.Fatalf("Apply: unexpected error: %v", err)
	}
	if result.Channel != "backgroundServices" {
		t.Errorf("result.Channel = %q, want %q", result.Channel, "backgroundServices")
	}
	if len(result.Applied) != 1 {
		t.Fatalf("result.Applied: got %d, want 1", len(result.Applied))
	}

	// verify start.sh was written
	startContent, err := mem.ReadFile("/repo/.ainfra/services/my-svc/start.sh")
	if err != nil {
		t.Fatalf("ReadFile start.sh: %v", err)
	}
	if !strings.Contains(string(startContent), "node worker.js") {
		t.Errorf("start.sh missing command, got: %q", startContent)
	}
	if !strings.Contains(string(startContent), "my-svc") {
		t.Errorf("start.sh missing service id, got: %q", startContent)
	}
	if !strings.Contains(string(startContent), "worker") {
		t.Errorf("start.sh missing kind, got: %q", startContent)
	}

	// verify stop.sh was written
	stopContent, err := mem.ReadFile("/repo/.ainfra/services/my-svc/stop.sh")
	if err != nil {
		t.Fatalf("ReadFile stop.sh: %v", err)
	}
	if !strings.HasPrefix(string(stopContent), "#!/bin/sh") {
		t.Errorf("stop.sh missing shebang, got: %q", stopContent)
	}
}

func TestServicesApply_Create_NoCommand(t *testing.T) {
	mem := provider.NewMemFilesystem()
	env := provider.Env{FS: mem, Root: "/repo"}

	plan := provider.ChannelPlan{
		Channel: "backgroundServices",
		Changes: []provider.Change{
			{
				Kind: provider.ChangeCreate,
				ID:   "my-svc",
				Resource: provider.Resource{
					ID:      "my-svc",
					Channel: "backgroundServices",
					Payload: map[string]any{
						"kind": "worker",
						"spec": map[string]any{},
					},
				},
			},
		},
	}

	p := channels.Services{}
	_, err := p.Apply(env, plan)
	if err != nil {
		t.Fatalf("Apply: unexpected error: %v", err)
	}

	startContent, err := mem.ReadFile("/repo/.ainfra/services/my-svc/start.sh")
	if err != nil {
		t.Fatalf("ReadFile start.sh: %v", err)
	}
	if !strings.Contains(string(startContent), "# TODO") {
		t.Errorf("start.sh should contain TODO placeholder when no command given, got: %q", startContent)
	}
}

func TestServicesApply_Delete(t *testing.T) {
	mem := provider.NewMemFilesystem()
	env := provider.Env{FS: mem, Root: "/repo"}

	// pre-populate a service directory
	if err := mem.MkdirAll("/repo/.ainfra/services/old-svc", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := mem.WriteFile("/repo/.ainfra/services/old-svc/start.sh", []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := mem.WriteFile("/repo/.ainfra/services/old-svc/stop.sh", []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	plan := provider.ChannelPlan{
		Channel: "backgroundServices",
		Changes: []provider.Change{
			{
				Kind: provider.ChangeDelete,
				ID:   "old-svc",
				Resource: provider.Resource{
					ID:      "old-svc",
					Channel: "backgroundServices",
				},
			},
		},
	}

	p := channels.Services{}
	result, err := p.Apply(env, plan)
	if err != nil {
		t.Fatalf("Apply Delete: unexpected error: %v", err)
	}
	if len(result.Applied) != 1 {
		t.Fatalf("result.Applied: got %d, want 1", len(result.Applied))
	}

	// verify files were removed
	if _, err := mem.ReadFile("/repo/.ainfra/services/old-svc/start.sh"); err == nil {
		t.Error("start.sh still exists after Delete")
	}
	if _, err := mem.ReadFile("/repo/.ainfra/services/old-svc/stop.sh"); err == nil {
		t.Error("stop.sh still exists after Delete")
	}
}

func TestServicesApply_DryRun(t *testing.T) {
	mem := provider.NewMemFilesystem()
	env := provider.Env{FS: mem, Root: "/repo", DryRun: true}

	plan := provider.ChannelPlan{
		Channel: "backgroundServices",
		Changes: []provider.Change{
			{
				Kind: provider.ChangeCreate,
				ID:   "my-svc",
				Resource: provider.Resource{
					ID:      "my-svc",
					Channel: "backgroundServices",
					Payload: map[string]any{
						"kind": "worker",
						"spec": map[string]any{
							"command": "node worker.js",
						},
					},
				},
			},
		},
	}

	p := channels.Services{}
	result, err := p.Apply(env, plan)
	if err != nil {
		t.Fatalf("Apply DryRun: unexpected error: %v", err)
	}
	if len(result.Applied) != 1 {
		t.Fatalf("DryRun: result.Applied: got %d, want 1", len(result.Applied))
	}

	// no files should have been written
	if _, err := mem.ReadFile("/repo/.ainfra/services/my-svc/start.sh"); err == nil {
		t.Error("DryRun: start.sh was written, should not have been")
	}
	if _, err := mem.ReadFile("/repo/.ainfra/services/my-svc/stop.sh"); err == nil {
		t.Error("DryRun: stop.sh was written, should not have been")
	}
}
