package check

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/MHilhorst/ainfra/internal/lockfile"
	"github.com/MHilhorst/ainfra/internal/mcpclient"
)

type fakeTool struct {
	name        string
	description string
	inputSchema map[string]any
}

func scriptRunner(t *testing.T, tools []fakeTool) *mcpclient.FakeRunner {
	t.Helper()
	raw := make([]map[string]any, 0, len(tools))
	for _, f := range tools {
		raw = append(raw, map[string]any{
			"name":        f.name,
			"description": f.description,
			"inputSchema": f.inputSchema,
		})
	}
	body, err := json.Marshal(map[string]any{"tools": raw})
	if err != nil {
		t.Fatalf("marshal scripted tools: %v", err)
	}
	return &mcpclient.FakeRunner{
		Responses: map[string]json.RawMessage{
			"initialize": json.RawMessage(`{"protocolVersion":"2024-11-05","capabilities":{}}`),
			"tools/list": body,
		},
	}
}

// lockedEntryFromTools synthesizes a lockfile.Entry as if RunLock had just
// introspected the same `tools`. It avoids reaching into resolve internals.
func lockedEntryFromTools(t *testing.T, command string, args []string, env map[string]string, raw []fakeTool) lockfile.Entry {
	t.Helper()
	// Build the canonical ToolList the way mcpclient.Introspect would, by
	// running it once against a scripted runner.
	runner := scriptRunner(t, raw)
	tools, err := mcpclient.Introspect(context.Background(), mcpclient.Request{
		Command: command,
		Args:    args,
		Env:     env,
		Runner:  runner,
	})
	if err != nil {
		t.Fatalf("seed introspect: %v", err)
	}

	locked := make([]lockfile.LockedTool, 0, len(tools))
	for _, tl := range tools {
		locked = append(locked, lockfile.LockedTool{
			Name:            tl.Name,
			DescriptionHash: lockfile.ContentHash(tl.Description),
			InputSchemaHash: lockfile.ContentHash(string(tl.InputSchema)),
		})
	}
	return lockfile.Entry{
		Layer:       "repo",
		Command:     command,
		Args:        args,
		Env:         env,
		ToolsetHash: lockfile.ContentHash(tools),
		LockedTools: locked,
	}
}

func mkCommittedLock(entries map[string]lockfile.Entry) *lockfile.Lock {
	l := &lockfile.Lock{Version: 1}
	l.Entries.MCPServers = entries
	return l
}

func TestCheckToolsetDriftCleanWhenLiveMatches(t *testing.T) {
	tools := []fakeTool{
		{name: "alpha", description: "a", inputSchema: map[string]any{"type": "object"}},
		{name: "beta", description: "b", inputSchema: map[string]any{"type": "object"}},
	}
	committed := mkCommittedLock(map[string]lockfile.Entry{
		"one": lockedEntryFromTools(t, "fake-mcp", nil, nil, tools),
		"two": lockedEntryFromTools(t, "fake-mcp", nil, nil, tools),
	})
	personal := &lockfile.Lock{Version: 1}

	report := checkToolsetDriftWithRunner(committed, personal, scriptRunner(t, tools))
	if report.HasDrift() {
		t.Fatalf("expected clean; got drifts=%+v", report.Drifts)
	}
	if report.UnverifiedCount != 0 {
		t.Errorf("expected 0 unverified; got %d", report.UnverifiedCount)
	}
}

func TestCheckToolsetDriftDescriptionChange(t *testing.T) {
	lockedTools := []fakeTool{
		{name: "alpha", description: "old", inputSchema: map[string]any{"type": "object"}},
	}
	liveTools := []fakeTool{
		{name: "alpha", description: "new", inputSchema: map[string]any{"type": "object"}},
	}
	committed := mkCommittedLock(map[string]lockfile.Entry{
		"one": lockedEntryFromTools(t, "fake-mcp", nil, nil, lockedTools),
	})

	report := checkToolsetDriftWithRunner(committed, &lockfile.Lock{}, scriptRunner(t, liveTools))
	if !report.HasDrift() {
		t.Fatal("expected drift")
	}
	if len(report.Drifts) != 1 {
		t.Fatalf("expected 1 drift; got %d", len(report.Drifts))
	}
	d := report.Drifts[0]
	if d.ServerID != "one" {
		t.Errorf("ServerID = %q", d.ServerID)
	}
	if len(d.Diff) != 1 || d.Diff[0].Name != "alpha" || d.Diff[0].Kind != ToolDescriptionChanged {
		t.Errorf("expected one description-changed diff for alpha; got %+v", d.Diff)
	}
}

func TestCheckToolsetDriftToolAdded(t *testing.T) {
	lockedTools := []fakeTool{
		{name: "alpha", description: "a", inputSchema: map[string]any{"type": "object"}},
	}
	liveTools := []fakeTool{
		{name: "alpha", description: "a", inputSchema: map[string]any{"type": "object"}},
		{name: "gamma", description: "g", inputSchema: map[string]any{"type": "object"}},
	}
	committed := mkCommittedLock(map[string]lockfile.Entry{
		"one": lockedEntryFromTools(t, "fake-mcp", nil, nil, lockedTools),
	})

	report := checkToolsetDriftWithRunner(committed, &lockfile.Lock{}, scriptRunner(t, liveTools))
	if !report.HasDrift() {
		t.Fatal("expected drift")
	}
	d := report.Drifts[0]
	if len(d.Diff) != 1 || d.Diff[0].Name != "gamma" || d.Diff[0].Kind != ToolAdded {
		t.Errorf("expected one added=gamma; got %+v", d.Diff)
	}
}

func TestCheckToolsetDriftToolRemoved(t *testing.T) {
	lockedTools := []fakeTool{
		{name: "alpha", description: "a", inputSchema: map[string]any{"type": "object"}},
		{name: "beta", description: "b", inputSchema: map[string]any{"type": "object"}},
	}
	liveTools := []fakeTool{
		{name: "alpha", description: "a", inputSchema: map[string]any{"type": "object"}},
	}
	committed := mkCommittedLock(map[string]lockfile.Entry{
		"one": lockedEntryFromTools(t, "fake-mcp", nil, nil, lockedTools),
	})

	report := checkToolsetDriftWithRunner(committed, &lockfile.Lock{}, scriptRunner(t, liveTools))
	if !report.HasDrift() {
		t.Fatal("expected drift")
	}
	d := report.Drifts[0]
	if len(d.Diff) != 1 || d.Diff[0].Name != "beta" || d.Diff[0].Kind != ToolRemoved {
		t.Errorf("expected one removed=beta; got %+v", d.Diff)
	}
}

func TestCheckToolsetDriftMultipleChanges(t *testing.T) {
	lockedTools := []fakeTool{
		{name: "alpha", description: "old", inputSchema: map[string]any{"type": "object"}},
		{name: "beta", description: "b", inputSchema: map[string]any{"type": "object"}},
	}
	liveTools := []fakeTool{
		{name: "alpha", description: "new", inputSchema: map[string]any{"type": "object"}},
		{name: "gamma", description: "g", inputSchema: map[string]any{"type": "object"}},
	}
	committed := mkCommittedLock(map[string]lockfile.Entry{
		"one": lockedEntryFromTools(t, "fake-mcp", nil, nil, lockedTools),
	})

	report := checkToolsetDriftWithRunner(committed, &lockfile.Lock{}, scriptRunner(t, liveTools))
	if !report.HasDrift() {
		t.Fatal("expected drift")
	}
	d := report.Drifts[0]
	gotKinds := map[string]ToolDiffKind{}
	for _, td := range d.Diff {
		gotKinds[td.Name] = td.Kind
	}
	if gotKinds["alpha"] != ToolDescriptionChanged {
		t.Errorf("expected alpha description changed; got %+v", d.Diff)
	}
	if gotKinds["beta"] != ToolRemoved {
		t.Errorf("expected beta removed; got %+v", d.Diff)
	}
	if gotKinds["gamma"] != ToolAdded {
		t.Errorf("expected gamma added; got %+v", d.Diff)
	}
}

func TestCheckToolsetDriftInputSchemaChange(t *testing.T) {
	lockedTools := []fakeTool{
		{name: "alpha", description: "a", inputSchema: map[string]any{"type": "object"}},
	}
	liveTools := []fakeTool{
		{name: "alpha", description: "a", inputSchema: map[string]any{"type": "object", "properties": map[string]any{"x": map[string]any{"type": "string"}}}},
	}
	committed := mkCommittedLock(map[string]lockfile.Entry{
		"one": lockedEntryFromTools(t, "fake-mcp", nil, nil, lockedTools),
	})

	report := checkToolsetDriftWithRunner(committed, &lockfile.Lock{}, scriptRunner(t, liveTools))
	if !report.HasDrift() {
		t.Fatal("expected drift")
	}
	d := report.Drifts[0]
	if len(d.Diff) != 1 || d.Diff[0].Name != "alpha" || d.Diff[0].Kind != ToolInputSchemaChanged {
		t.Errorf("expected input schema changed for alpha; got %+v", d.Diff)
	}
}

func TestCheckToolsetDriftUnverifiedEntryNotDrift(t *testing.T) {
	committed := mkCommittedLock(map[string]lockfile.Entry{
		"one": {Layer: "repo", Command: "fake-mcp"}, // ToolsetHash empty
	})

	// Runner should not be used; pass a runner that would 500 if called.
	runner := &mcpclient.FakeRunner{StartErr: errors.New("should not be called")}
	report := checkToolsetDriftWithRunner(committed, &lockfile.Lock{}, runner)
	if report.HasDrift() {
		t.Fatalf("unverified entry should not be drift; got %+v", report.Drifts)
	}
	if report.UnverifiedCount != 1 {
		t.Errorf("UnverifiedCount = %d, want 1", report.UnverifiedCount)
	}
}

func TestCheckToolsetDriftIntrospectionFailure(t *testing.T) {
	lockedTools := []fakeTool{
		{name: "alpha", description: "a", inputSchema: map[string]any{"type": "object"}},
	}
	committed := mkCommittedLock(map[string]lockfile.Entry{
		"one": lockedEntryFromTools(t, "fake-mcp", nil, nil, lockedTools),
	})
	runner := &mcpclient.FakeRunner{StartErr: errors.New("boom: subprocess could not start")}

	report := checkToolsetDriftWithRunner(committed, &lockfile.Lock{}, runner)
	if !report.HasDrift() {
		t.Fatal("expected drift (introspection failure)")
	}
	d := report.Drifts[0]
	if d.IntrospectErr == "" {
		t.Error("expected IntrospectErr to be set")
	}
	if d.LiveHash != "" {
		t.Errorf("LiveHash should be empty on introspection failure; got %q", d.LiveHash)
	}
}

func TestCheckToolsetDriftMixedCleanAndDirty(t *testing.T) {
	lockedTools := []fakeTool{
		{name: "alpha", description: "a", inputSchema: map[string]any{"type": "object"}},
	}
	// Note: scriptRunner returns the SAME response for every Start, so to
	// model "one clean, one drifted" we use distinct commands and a runner
	// that always returns the same drifted list — both entries with the same
	// locked tools but live differs for both. We instead test that across
	// multiple servers with the same scripted live response, drifted ones are
	// reported and the clean one is omitted. So locked entry "clean" has the
	// same tools as live; "dirty" has different locked tools.
	cleanLocked := lockedTools
	dirtyLocked := []fakeTool{
		{name: "alpha", description: "old", inputSchema: map[string]any{"type": "object"}},
	}
	committed := mkCommittedLock(map[string]lockfile.Entry{
		"clean": lockedEntryFromTools(t, "fake-mcp", nil, nil, cleanLocked),
		"dirty": lockedEntryFromTools(t, "fake-mcp", nil, nil, dirtyLocked),
	})

	report := checkToolsetDriftWithRunner(committed, &lockfile.Lock{}, scriptRunner(t, lockedTools))
	if !report.HasDrift() {
		t.Fatal("expected drift")
	}
	if len(report.Drifts) != 1 {
		t.Fatalf("expected exactly 1 drift (only dirty); got %d: %+v", len(report.Drifts), report.Drifts)
	}
	if report.Drifts[0].ServerID != "dirty" {
		t.Errorf("expected dirty drifted; got %q", report.Drifts[0].ServerID)
	}
}

func TestCheckToolsetDriftPersonalEntries(t *testing.T) {
	lockedTools := []fakeTool{
		{name: "alpha", description: "old", inputSchema: map[string]any{"type": "object"}},
	}
	liveTools := []fakeTool{
		{name: "alpha", description: "new", inputSchema: map[string]any{"type": "object"}},
	}
	personal := &lockfile.Lock{Version: 1}
	personal.Entries.MCPServers = map[string]lockfile.Entry{
		"private": lockedEntryFromTools(t, "fake-mcp", nil, nil, lockedTools),
	}

	report := checkToolsetDriftWithRunner(&lockfile.Lock{}, personal, scriptRunner(t, liveTools))
	if !report.HasDrift() {
		t.Fatal("expected drift on personal entry")
	}
	if report.Drifts[0].ServerID != "private" {
		t.Errorf("expected private drifted; got %q", report.Drifts[0].ServerID)
	}
}

func TestCheckToolsetDriftMissingCommandFlagsReLock(t *testing.T) {
	committed := mkCommittedLock(map[string]lockfile.Entry{
		"old": {Layer: "repo", ToolsetHash: "sha256:deadbeef"},
	})
	report := checkToolsetDriftWithRunner(committed, &lockfile.Lock{}, nil)
	if !report.HasDrift() {
		t.Fatal("expected drift on entry with hash but no command")
	}
	if report.Drifts[0].IntrospectErr == "" {
		t.Error("expected IntrospectErr to nudge re-lock")
	}
}
