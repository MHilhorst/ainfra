package resolve

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MHilhorst/ainfra/internal/lockfile"
	"github.com/MHilhorst/ainfra/internal/mcpclient"
	"github.com/MHilhorst/ainfra/internal/provider"
)

// withIntrospectRunner swaps IntrospectRunner for the duration of a test and
// restores it (which is DisableIntrospection in TestMain) afterward.
func withIntrospectRunner(t *testing.T, r mcpclient.Runner) {
	t.Helper()
	prev := IntrospectRunner
	IntrospectRunner = r
	t.Cleanup(func() { IntrospectRunner = prev })
}

func newOkIntrospectRunner() *mcpclient.FakeRunner {
	tools := []map[string]any{
		{"name": "alpha", "description": "a", "inputSchema": map[string]any{"type": "object"}},
		{"name": "beta", "description": "b", "inputSchema": map[string]any{"type": "object"}},
	}
	body, _ := json.Marshal(map[string]any{"tools": tools})
	return &mcpclient.FakeRunner{
		Responses: map[string]json.RawMessage{
			"initialize": json.RawMessage(`{"protocolVersion":"2024-11-05","capabilities":{}}`),
			"tools/list": body,
		},
	}
}

func TestLockPipelinePopulatesToolsetHash(t *testing.T) {
	withIntrospectRunner(t, newOkIntrospectRunner())
	dir := t.TempDir()
	yaml := `version: 1
mcpServers:
  fs:
    command: fake-mcp
    args: ["--root", "."]
    transport: stdio
    version: "1.0.0"
`
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := RunLockWithResult(dir, provider.ExecRunner{})
	if err != nil {
		t.Fatalf("RunLockWithResult: %v", err)
	}
	if len(res.ToolsetWarnings) != 0 {
		t.Errorf("unexpected warnings: %+v", res.ToolsetWarnings)
	}
	lock, err := lockfile.Read(filepath.Join(dir, "ainfra.lock"))
	if err != nil {
		t.Fatal(err)
	}
	e := lock.Entries.MCPServers["fs"]
	if e.ToolsetHash == "" {
		t.Fatalf("ToolsetHash empty; entry=%+v", e)
	}
	if !strings.HasPrefix(e.ToolsetHash, "sha256:") {
		t.Errorf("ToolsetHash missing sha256 prefix: %q", e.ToolsetHash)
	}
	// U3: LockedTools and Command/Args/Env are populated alongside ToolsetHash
	// so `ainfra check` can re-introspect and identify the changed tool by name.
	if len(e.LockedTools) != 2 {
		t.Errorf("expected 2 LockedTools; got %d: %+v", len(e.LockedTools), e.LockedTools)
	}
	if e.Command != "fake-mcp" {
		t.Errorf("expected Command=fake-mcp; got %q", e.Command)
	}
	if len(e.Args) != 2 || e.Args[0] != "--root" {
		t.Errorf("expected Args=[--root .]; got %+v", e.Args)
	}
}

func TestLockPipelineDistinctToolsetsHashDifferently(t *testing.T) {
	// Two servers with the same command path but different scripted tool
	// lists must end up with different ToolsetHashes. FakeRunner shares
	// scripted responses across calls, so we run two locks back-to-back with
	// different scripted runners.
	hashFor := func(toolName string) string {
		body, _ := json.Marshal(map[string]any{"tools": []map[string]any{
			{"name": toolName, "description": "x", "inputSchema": map[string]any{"type": "object"}},
		}})
		withIntrospectRunner(t, &mcpclient.FakeRunner{
			Responses: map[string]json.RawMessage{
				"initialize": json.RawMessage(`{"protocolVersion":"2024-11-05","capabilities":{}}`),
				"tools/list": body,
			},
		})
		dir := t.TempDir()
		yaml := `version: 1
mcpServers:
  one: { command: fake, transport: stdio, version: "1.0.0" }
`
		if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(yaml), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := RunLockWithResult(dir, provider.ExecRunner{}); err != nil {
			t.Fatal(err)
		}
		l, _ := lockfile.Read(filepath.Join(dir, "ainfra.lock"))
		return l.Entries.MCPServers["one"].ToolsetHash
	}
	a := hashFor("toolA")
	b := hashFor("toolB")
	if a == "" || b == "" {
		t.Fatalf("expected non-empty hashes, got a=%q b=%q", a, b)
	}
	if a == b {
		t.Errorf("distinct toolsets produced the same hash: %q", a)
	}
}

func TestLockPipelineTemplatedServerHashesPerInstance(t *testing.T) {
	withIntrospectRunner(t, newOkIntrospectRunner())
	dir := t.TempDir()
	yaml := `version: 1
templates:
  fs:
    params: { root: { type: string, required: true } }
    produces:
      mcpServer:
        transport: stdio
        command: fake-mcp
        args: ["${params.root}"]
        version: "1.0.0"
mcpServers:
  a: { template: fs, params: { root: "/a" } }
  b: { template: fs, params: { root: "/b" } }
`
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := RunLockWithResult(dir, provider.ExecRunner{}); err != nil {
		t.Fatalf("RunLockWithResult: %v", err)
	}
	lock, _ := lockfile.Read(filepath.Join(dir, "ainfra.lock"))
	for _, id := range []string{"a", "b"} {
		if lock.Entries.MCPServers[id].ToolsetHash == "" {
			t.Errorf("server %q: ToolsetHash empty", id)
		}
	}
	// Both instances point at the same scripted runner so their toolset
	// hashes match; what we care about is that both were populated.
}

func TestLockPipelineSubprocessFailureRecordsWarning(t *testing.T) {
	withIntrospectRunner(t, &mcpclient.FakeRunner{StartErr: errSentinel("boom")})
	dir := t.TempDir()
	yaml := `version: 1
mcpServers:
  fs: { command: fake, transport: stdio, version: "1.0.0" }
`
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := RunLockWithResult(dir, provider.ExecRunner{})
	if err != nil {
		t.Fatalf("RunLockWithResult: %v", err)
	}
	lock, _ := lockfile.Read(filepath.Join(dir, "ainfra.lock"))
	if h := lock.Entries.MCPServers["fs"].ToolsetHash; h != "" {
		t.Errorf("expected empty ToolsetHash on subprocess failure, got %q", h)
	}
	if len(res.ToolsetWarnings) != 1 || res.ToolsetWarnings[0].ServerID != "fs" || res.ToolsetWarnings[0].Reason != "subprocess-failed" {
		t.Errorf("warnings = %+v, want one subprocess-failed for fs", res.ToolsetWarnings)
	}
}

func TestLockPipelineTimeoutRecordsWarning(t *testing.T) {
	// ResponseDelay greater than the runner's timeout would block forever;
	// instead we craft a runner whose Start returns a process that never
	// answers. The simplest way is a FakeRunner with ResponseDelay set high
	// and a very low introspection budget — but mcpclient.Introspect uses
	// req.Timeout (defaults 15s). Use a custom runner that returns a never-
	// answering process and tighten the timeout via the override below.
	withIntrospectRunner(t, &mcpclient.FakeRunner{
		Responses: map[string]json.RawMessage{
			"initialize": json.RawMessage(`{"protocolVersion":"2024-11-05","capabilities":{}}`),
			"tools/list": json.RawMessage(`{"tools":[]}`),
		},
		ResponseDelay: 2 * time.Second,
	})
	// Temporarily reduce the budget by swapping IntrospectTimeout if exposed.
	// If not, use a manually-short timeout via a wrapper. For this test we
	// simply expect timeout classification when the deadline trips. We get
	// there by injecting a short ctx in introspectMCPServer; lacking that
	// hook, we rely on FakeRunner's ResponseDelay being longer than the
	// per-call timeout below.
	prev := introspectTimeoutForTests
	introspectTimeoutForTests = 50 * time.Millisecond
	t.Cleanup(func() { introspectTimeoutForTests = prev })

	dir := t.TempDir()
	yaml := `version: 1
mcpServers:
  fs: { command: fake, transport: stdio, version: "1.0.0" }
`
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := RunLockWithResult(dir, provider.ExecRunner{})
	if err != nil {
		t.Fatalf("RunLockWithResult: %v", err)
	}
	lock, _ := lockfile.Read(filepath.Join(dir, "ainfra.lock"))
	if h := lock.Entries.MCPServers["fs"].ToolsetHash; h != "" {
		t.Errorf("expected empty ToolsetHash on timeout, got %q", h)
	}
	if len(res.ToolsetWarnings) != 1 || res.ToolsetWarnings[0].Reason != "timeout" {
		t.Errorf("warnings = %+v, want one timeout for fs", res.ToolsetWarnings)
	}
}

func TestLockPipelineNonStdioTransportSkipsIntrospect(t *testing.T) {
	runner := &mcpclient.FakeRunner{StartErr: errSentinel("should not start")}
	withIntrospectRunner(t, runner)
	dir := t.TempDir()
	yaml := `version: 1
mcpServers:
  api:
    transport: http
    url: "https://mcp.example.com"
`
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := RunLockWithResult(dir, provider.ExecRunner{})
	if err != nil {
		t.Fatalf("RunLockWithResult: %v", err)
	}
	if runner.LastCmd() != "" {
		t.Errorf("Introspect ran for a non-stdio server (cmd=%q)", runner.LastCmd())
	}
	if len(res.ToolsetWarnings) != 1 || res.ToolsetWarnings[0].Reason != "unsupported-transport" {
		t.Errorf("warnings = %+v, want unsupported-transport", res.ToolsetWarnings)
	}
}

func TestLockPipelineToolsetHashStableAcrossRuns(t *testing.T) {
	dir := t.TempDir()
	yaml := `version: 1
mcpServers:
  fs: { command: fake, transport: stdio, version: "1.0.0" }
`
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	hashOf := func() string {
		withIntrospectRunner(t, newOkIntrospectRunner())
		if _, err := RunLockWithResult(dir, provider.ExecRunner{}); err != nil {
			t.Fatal(err)
		}
		l, _ := lockfile.Read(filepath.Join(dir, "ainfra.lock"))
		return l.Entries.MCPServers["fs"].ToolsetHash
	}
	a := hashOf()
	b := hashOf()
	if a == "" {
		t.Fatal("hash empty on first run")
	}
	if a != b {
		t.Errorf("hash unstable across runs: %q vs %q", a, b)
	}
}

func TestLockPipelineWarningsSortedByServerID(t *testing.T) {
	withIntrospectRunner(t, &mcpclient.FakeRunner{StartErr: errSentinel("nope")})
	dir := t.TempDir()
	yaml := `version: 1
mcpServers:
  zeta: { command: fake, transport: stdio, version: "1.0.0" }
  alpha: { command: fake, transport: stdio, version: "1.0.0" }
  middle: { command: fake, transport: stdio, version: "1.0.0" }
`
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := RunLockWithResult(dir, provider.ExecRunner{})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, w := range res.ToolsetWarnings {
		got = append(got, w.ServerID)
	}
	want := []string{"alpha", "middle", "zeta"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("warnings not sorted: got %v, want %v", got, want)
	}
}

// errSentinel is a tiny error type for tests.
type errSentinel string

func (e errSentinel) Error() string { return string(e) }

func TestLockPipelineOnMultiDBExample(t *testing.T) {
	dir := t.TempDir()
	manifestYAML := `version: 1
cliTools:
  ssh: { versionConstraint: ">=8.0" }
templates:
  tun:
    params: { host: { type: string, required: true } }
    resolved: { tunnelPort: { kind: allocated-port } }
    produces:
      mcpServer:
        command: npx
        version: "1.0.0"
        env: { P: "${resolved.tunnelPort}" }
        requires: [ { service: "${instance.id}-tunnel" } ]
      backgroundService:
        id: "${instance.id}-tunnel"
        kind: ssh-tunnel
        requires: [ { cliTool: ssh } ]
mcpServers:
  db-a: { template: tun, params: { host: a.example } }
  db-b: { template: tun, params: { host: b.example } }
`
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(manifestYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RunLock(dir, provider.ExecRunner{}); err != nil {
		t.Fatalf("RunLock: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "ainfra.lock"))
	if err != nil {
		t.Fatalf("lock not written: %v", err)
	}
	out := string(data)
	for _, want := range []string{"db-a", "db-b", "tunnelPort: 13306", "tunnelPort: 13307", "contentHash:"} {
		if !strings.Contains(out, want) {
			t.Errorf("lock missing %q\n---\n%s", want, out)
		}
	}
}

func TestLockPipelineResolvesHooksAndCommands(t *testing.T) {
	dir := t.TempDir()
	manifestYAML := `version: 1
cliTools:
  node: { versionConstraint: ">=20" }
hooks:
  guard-branch:
    event: PreToolUse
    matcher: "Edit|Write"
    command: node .ainfra/run/guard.js
    requires: [ { cliTool: node } ]
commands:
  ship:
    source: ./commands/ship.md
    description: Fast-path merge to the default branch.
`
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(manifestYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RunLock(dir, provider.ExecRunner{}); err != nil {
		t.Fatalf("RunLock: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "ainfra.lock"))
	if err != nil {
		t.Fatalf("lock not written: %v", err)
	}
	out := string(data)
	for _, want := range []string{"hooks:", "guard-branch", "commands:", "ship", "contentHash:"} {
		if !strings.Contains(out, want) {
			t.Errorf("lock missing %q\n---\n%s", want, out)
		}
	}
}

func TestLockPipelineResolvesSkillsPluginsRules(t *testing.T) {
	dir := t.TempDir()
	manifestYAML := `version: 1
cliTools:
  node: { versionConstraint: ">=20" }
skills:
  debug:
    source: ./skills/debug
    requires: [ { cliTool: node } ]
marketplaces:
  acme: { source: "github:acme/plugins" }
plugins:
  tvt: { marketplace: "acme", version: "2.0.1" }
rules:
  team: { target: CLAUDE.md, source: ./rules/team.md }
`
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(manifestYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RunLock(dir, provider.ExecRunner{}); err != nil {
		t.Fatalf("RunLock: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "ainfra.lock"))
	if err != nil {
		t.Fatalf("lock not written: %v", err)
	}
	out := string(data)
	for _, want := range []string{"skills:", "debug", "marketplaces:", "acme", "plugins:", "tvt", "rules:", "team", "cli:node"} {
		if !strings.Contains(out, want) {
			t.Errorf("lock missing %q\n---\n%s", want, out)
		}
	}
	lock, err := lockfile.Read(filepath.Join(dir, "ainfra.lock"))
	if err != nil {
		t.Fatalf("read lock: %v", err)
	}
	if got := lock.Entries.Skills["debug"].Requires; len(got) != 1 || got[0] != "cli:node" {
		t.Errorf("skill debug requires = %v, want [cli:node]", got)
	}
	if e, ok := lock.Entries.Plugins["tvt"]; !ok || e.Version != "2.0.1" {
		t.Errorf("plugin tvt = %+v, ok=%v", e, ok)
	}
	if e, ok := lock.Entries.Rules["team"]; !ok || e.ContentHash == "" {
		t.Errorf("rule team = %+v, ok=%v", e, ok)
	}
}

func TestLockPipelineResolvesMarketplaces(t *testing.T) {
	dir := t.TempDir()
	manifestYAML := `version: 1
marketplaces:
  my-org: { source: "github:my-org/plugins" }
  local-mp: { source: "./local-marketplace" }
`
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(manifestYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RunLock(dir, provider.ExecRunner{}); err != nil {
		t.Fatalf("RunLock: %v", err)
	}
	lock, err := lockfile.Read(filepath.Join(dir, "ainfra.lock"))
	if err != nil {
		t.Fatalf("read lock: %v", err)
	}
	e, ok := lock.Entries.Marketplaces["my-org"]
	if !ok {
		t.Fatal("marketplace my-org not in lock")
	}
	if e.ContentHash == "" {
		t.Error("marketplace my-org: ContentHash is empty")
	}
	if e.Layer != "repo" {
		t.Errorf("marketplace my-org: Layer = %q, want repo", e.Layer)
	}
	_, ok2 := lock.Entries.Marketplaces["local-mp"]
	if !ok2 {
		t.Fatal("marketplace local-mp not in lock")
	}
}

func TestLockPipelineMarketplaceSourceChangesHash(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()

	yaml1 := `version: 1
marketplaces:
  my-org: { source: "github:my-org/plugins" }
`
	yaml2 := `version: 1
marketplaces:
  my-org: { source: "github:my-org/other-plugins" }
`
	if err := os.WriteFile(filepath.Join(dir1, "ainfra.yaml"), []byte(yaml1), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir2, "ainfra.yaml"), []byte(yaml2), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RunLock(dir1, provider.ExecRunner{}); err != nil {
		t.Fatal(err)
	}
	if err := RunLock(dir2, provider.ExecRunner{}); err != nil {
		t.Fatal(err)
	}
	l1, _ := lockfile.Read(filepath.Join(dir1, "ainfra.lock"))
	l2, _ := lockfile.Read(filepath.Join(dir2, "ainfra.lock"))
	h1 := l1.Entries.Marketplaces["my-org"].ContentHash
	h2 := l2.Entries.Marketplaces["my-org"].ContentHash
	if h1 == h2 {
		t.Errorf("different marketplace sources produced the same hash: %q", h1)
	}
}

func TestLockPipelinePluginHashCoversMaketplace(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()

	yaml1 := `version: 1
marketplaces:
  acme: { source: "github:acme/plugins" }
plugins:
  p: { marketplace: "acme" }
`
	yaml2 := `version: 1
marketplaces:
  acme: { source: "github:acme/plugins" }
  other: { source: "github:other/plugins" }
plugins:
  p: { marketplace: "other" }
`
	if err := os.WriteFile(filepath.Join(dir1, "ainfra.yaml"), []byte(yaml1), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir2, "ainfra.yaml"), []byte(yaml2), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RunLock(dir1, provider.ExecRunner{}); err != nil {
		t.Fatal(err)
	}
	if err := RunLock(dir2, provider.ExecRunner{}); err != nil {
		t.Fatal(err)
	}
	l1, _ := lockfile.Read(filepath.Join(dir1, "ainfra.lock"))
	l2, _ := lockfile.Read(filepath.Join(dir2, "ainfra.lock"))
	h1 := l1.Entries.Plugins["p"].ContentHash
	h2 := l2.Entries.Plugins["p"].ContentHash
	if h1 == h2 {
		t.Errorf("different plugin marketplaces produced the same hash: %q", h1)
	}
}

func TestLockPipelinePersonalMarketplaceRoutesToPersonalLock(t *testing.T) {
	dir := t.TempDir()
	repoYAML := `version: 1`
	personalYAML := `version: 1
marketplaces:
  personal-org: { source: "github:personal/plugins" }
`
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(repoYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ainfra.personal.yaml"), []byte(personalYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RunLock(dir, provider.ExecRunner{}); err != nil {
		t.Fatalf("RunLock: %v", err)
	}
	committed, err := lockfile.Read(filepath.Join(dir, "ainfra.lock"))
	if err != nil {
		t.Fatalf("read committed lock: %v", err)
	}
	personal, err := lockfile.Read(filepath.Join(dir, "ainfra.personal.lock"))
	if err != nil {
		t.Fatalf("read personal lock: %v", err)
	}
	if _, ok := committed.Entries.Marketplaces["personal-org"]; ok {
		t.Error("personal marketplace ended up in committed lock")
	}
	if _, ok := personal.Entries.Marketplaces["personal-org"]; !ok {
		t.Error("personal marketplace missing from personal lock")
	}
}

func TestLockPipelineResolvesTools(t *testing.T) {
	dir := t.TempDir()
	manifestYAML := `version: 1
tools:
  builtins:
    disabled: [WebFetch]
  permissions:
    allow: ["Bash(go test:*)"]
    deny: ["Bash(rm -rf:*)"]
`
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(manifestYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RunLock(dir, provider.ExecRunner{}); err != nil {
		t.Fatalf("RunLock: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "ainfra.lock"))
	if err != nil {
		t.Fatalf("lock not written: %v", err)
	}
	out := string(data)
	// The map key is now the fixed string "tools"; the layer field records "repo".
	for _, want := range []string{"tools:", "layer: repo", "contentHash:"} {
		if !strings.Contains(out, want) {
			t.Errorf("lock missing %q\n---\n%s", want, out)
		}
	}
}

func TestLockPipelineResolvesInlineMCPServer(t *testing.T) {
	dir := t.TempDir()
	manifestYAML := `version: 1
mcpServers:
  github:
    command: npx
    args: ["-y", "@modelcontextprotocol/server-github"]
    version: "2025.4.0"
    transport: stdio
`
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(manifestYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RunLock(dir, provider.ExecRunner{}); err != nil {
		t.Fatalf("RunLock: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "ainfra.lock"))
	if err != nil {
		t.Fatalf("lock not written: %v", err)
	}
	out := string(data)
	for _, want := range []string{"github", "version: 2025.4.0", "contentHash:"} {
		if !strings.Contains(out, want) {
			t.Errorf("lock missing %q\n---\n%s", want, out)
		}
	}
}

func TestLockPipelineRecordsRequiresOnExistingChannels(t *testing.T) {
	dir := t.TempDir()
	manifestYAML := `version: 1
cliTools:
  node: { versionConstraint: ">=20" }
  ssh: { versionConstraint: ">=8" }
templates:
  tun:
    params: { host: { type: string, required: true } }
    resolved: { tunnelPort: { kind: allocated-port } }
    produces:
      mcpServer:
        command: npx
        version: "1.0.0"
        requires: [ { service: "${instance.id}-tunnel" } ]
      backgroundService:
        id: "${instance.id}-tunnel"
        kind: ssh-tunnel
        requires: [ { cliTool: ssh } ]
mcpServers:
  db-a: { template: tun, params: { host: a.example } }
hooks:
  guard: { event: Stop, command: "echo x", requires: [ { cliTool: node } ] }
commands:
  ship: { source: ./ship.md, requires: [ { cliTool: node } ] }
`
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(manifestYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RunLock(dir, provider.ExecRunner{}); err != nil {
		t.Fatalf("RunLock: %v", err)
	}
	lock, err := lockfile.Read(filepath.Join(dir, "ainfra.lock"))
	if err != nil {
		t.Fatalf("read lock: %v", err)
	}
	cases := []struct {
		name string
		got  []string
		want string
	}{
		{"templated MCP server", lock.Entries.MCPServers["db-a"].Requires, "svc:db-a-tunnel"},
		{"background service", lock.Entries.BackgroundServices["db-a-tunnel"].Requires, "cli:ssh"},
		{"hook", lock.Entries.Hooks["guard"].Requires, "cli:node"},
		{"command", lock.Entries.Commands["ship"].Requires, "cli:node"},
	}
	for _, c := range cases {
		if len(c.got) != 1 || c.got[0] != c.want {
			t.Errorf("%s requires = %v, want [%s]", c.name, c.got, c.want)
		}
	}
}

func TestLockPipelineRecordsManifestHash(t *testing.T) {
	dir := t.TempDir()
	manifestYAML := `version: 1
rules:
  team: { target: CLAUDE.md, source: ./rules/team.md }
`
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(manifestYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RunLock(dir, provider.ExecRunner{}); err != nil {
		t.Fatalf("RunLock: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "ainfra.lock"))
	if err != nil {
		t.Fatalf("lock not written: %v", err)
	}
	if !strings.Contains(string(data), "manifestHash: sha256:") {
		t.Errorf("ainfra.lock missing manifestHash\n---\n%s", data)
	}
	if !strings.Contains(string(data), "team") {
		t.Errorf("ainfra.lock dropped the rules entry\n---\n%s", data)
	}
}

func TestManifestHashIgnoresPersonalLayer(t *testing.T) {
	dir := t.TempDir()
	repoYAML := `version: 1
rules:
  team: { target: CLAUDE.md, source: ./rules/team.md }
`
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(repoYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RunLock(dir, provider.ExecRunner{}); err != nil {
		t.Fatalf("RunLock: %v", err)
	}
	first, err := lockfile.Read(filepath.Join(dir, "ainfra.lock"))
	if err != nil {
		t.Fatalf("read lock: %v", err)
	}

	// Adding a personal layer must not change the committed lock's hash.
	personalYAML := `version: 1
rules:
  mine: { target: CLAUDE.md, source: ./rules/mine.md }
`
	if err := os.WriteFile(filepath.Join(dir, "ainfra.personal.yaml"), []byte(personalYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RunLock(dir, provider.ExecRunner{}); err != nil {
		t.Fatalf("RunLock with personal layer: %v", err)
	}
	second, err := lockfile.Read(filepath.Join(dir, "ainfra.lock"))
	if err != nil {
		t.Fatalf("read lock: %v", err)
	}
	if first.ManifestHash == "" {
		t.Fatal("committed lock has no manifest hash")
	}
	if second.ManifestHash != first.ManifestHash {
		t.Errorf("committed manifest hash changed after adding a personal layer: %q -> %q",
			first.ManifestHash, second.ManifestHash)
	}
}

func TestLockPipelineResolvesCLITools(t *testing.T) {
	dir := t.TempDir()
	manifestYAML := `version: 1
cliTools:
  jq:
    versionConstraint: ">=1.6"
    install:
      brew: { formula: jq }
    check:
      command: jq --version
  gh:
    versionConstraint: ">=2.0"
`
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(manifestYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RunLock(dir, provider.ExecRunner{}); err != nil {
		t.Fatalf("RunLock: %v", err)
	}
	lock, err := lockfile.Read(filepath.Join(dir, "ainfra.lock"))
	if err != nil {
		t.Fatalf("read lock: %v", err)
	}
	for _, id := range []string{"jq", "gh"} {
		e, ok := lock.Entries.CLITools[id]
		if !ok {
			t.Errorf("lock missing cliTools entry %q", id)
			continue
		}
		if e.ContentHash == "" {
			t.Errorf("cliTools[%q] has no contentHash", id)
		}
	}
	if e := lock.Entries.CLITools["jq"]; e.Constraint != ">=1.6" {
		t.Errorf("cliTools[jq] constraint = %q, want >=1.6", e.Constraint)
	}
	if e := lock.Entries.CLITools["gh"]; e.Constraint != ">=2.0" {
		t.Errorf("cliTools[gh] constraint = %q, want >=2.0", e.Constraint)
	}
}

func TestLockPipelineAcceptsCleanHookAndCommandGraph(t *testing.T) {
	dir := t.TempDir()
	// A hook and a command both depending on the same cliTool is not a cycle;
	// the graph must accept it without a false positive.
	manifestYAML := `version: 1
cliTools:
  gh: { versionConstraint: ">=2" }
hooks:
  a: { event: Stop, command: "echo a", requires: [ { cliTool: gh } ] }
commands:
  b: { source: ./b.md, requires: [ { cliTool: gh } ] }
`
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(manifestYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RunLock(dir, provider.ExecRunner{}); err != nil {
		t.Fatalf("clean hook+command manifest must not error: %v", err)
	}
}

func TestLockPipelineHashesHeadersForDrift(t *testing.T) {
	run := func(token string) string {
		dir := t.TempDir()
		manifestYAML := `version: 1
templates:
  api:
    params: { tok: { type: string, required: true } }
    produces:
      mcpServer:
        transport: http
        url: https://mcp.example.com
        headers: { Authorization: "${params.tok}" }
mcpServers:
  svc: { template: api, params: { tok: "` + token + `" } }
`
		if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(manifestYAML), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := RunLock(dir, provider.ExecRunner{}); err != nil {
			t.Fatalf("RunLock: %v", err)
		}
		data, err := os.ReadFile(filepath.Join(dir, "ainfra.lock"))
		if err != nil {
			t.Fatalf("lock not written: %v", err)
		}
		return string(data)
	}
	hashLine := func(lock string) string {
		for _, line := range strings.Split(lock, "\n") {
			if strings.Contains(line, "contentHash:") {
				return strings.TrimSpace(line)
			}
		}
		return ""
	}
	a, b := run("token-one"), run("token-two")
	if hashLine(a) == "" {
		t.Fatal("no contentHash in lock")
	}
	if hashLine(a) == hashLine(b) {
		t.Errorf("a header change did not affect contentHash: %q", hashLine(a))
	}
}

func TestLockPipelineHashesArgsForDrift(t *testing.T) {
	run := func(arg string) string {
		dir := t.TempDir()
		manifestYAML := `version: 1
templates:
  api:
    params: { host: { type: string, required: true } }
    produces:
      mcpServer:
        command: npx
        version: "1.0.0"
        args: ["-y", "` + arg + `"]
mcpServers:
  svc: { template: api, params: { host: x } }
`
		if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(manifestYAML), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := RunLock(dir, provider.ExecRunner{}); err != nil {
			t.Fatalf("RunLock: %v", err)
		}
		data, err := os.ReadFile(filepath.Join(dir, "ainfra.lock"))
		if err != nil {
			t.Fatalf("lock not written: %v", err)
		}
		return string(data)
	}
	hashLine := func(lock string) string {
		for _, line := range strings.Split(lock, "\n") {
			if strings.Contains(line, "contentHash:") {
				return strings.TrimSpace(line)
			}
		}
		return ""
	}
	a, b := run("pkg-one"), run("pkg-two")
	if hashLine(a) == "" {
		t.Fatal("no contentHash in lock")
	}
	if hashLine(a) == hashLine(b) {
		t.Errorf("an args change did not affect contentHash: %q", hashLine(a))
	}
}
