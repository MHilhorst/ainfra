# Env Vars and Credential Files for MCPs and CLIs — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add HTTP MCP `url`/`headers`, CLI tool `env`/`secret`/`requires`, and a verify-only secret-to-file path to the ainfra manifest schema (Iteration 5).

**Architecture:** A schema iteration. Four new struct fields plus a transport-coupling validation rule; the templated-MCP resolve path interpolates `headers` and folds `url`/`headers` into the content hash. CLI tool and non-templated MCP *lock-time resolution* is deliberately deferred to the existing follow-up plan for non-templated entries — see the design doc §7. Docs and the showcase example are updated to match.

**Tech Stack:** Go, `gopkg.in/yaml.v3`, standard `testing`. Module path `github.com/MHilhorst/ainfra`. Run all commands from the worktree root `/Users/michael.hilhorst/projects/ainfra/.claude/worktrees/skills-clarify`.

**Spec:** `docs/superpowers/specs/2026-05-21-env-and-credential-files-design.md`

---

## File Structure

- `internal/manifest/types.go` — struct definitions; gains 5 fields. **Modify.**
- `internal/manifest/types_test.go` — YAML round-trip tests. **Modify.**
- `internal/manifest/validate.go` — static checks; gains a transport-coupling rule. **Modify.**
- `internal/manifest/validate_test.go` — validation tests. **Modify.**
- `internal/resolve/template.go` — template instantiation; interpolates `headers`. **Modify.**
- `internal/resolve/template_test.go` — instantiation tests. **Modify.**
- `internal/resolve/pipeline.go` — lock pipeline; folds `url`/`headers` into the MCP content hash. **Modify.**
- `internal/resolve/pipeline_test.go` — pipeline tests. **Modify.**
- `spec/manifest-schema.md` — manifest reference. **Modify.**
- `docs/assessment-vs-real-config.md` — gap tracking. **Modify.**
- `examples/multi-database/ainfra.yaml` — showcase manifest. **Modify.**

---

## Task 1: Schema fields on `MCPServer` and `CLITool`

**Files:**
- Modify: `internal/manifest/types.go`
- Test: `internal/manifest/types_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/manifest/types_test.go`:

```go
func TestManifestUnmarshalsEnvAndHeaderFields(t *testing.T) {
	src := []byte(`
version: 1
cliTools:
  aws-cli:
    versionConstraint: ">=2.0"
    env:
      AWS_REGION: eu-west-1
    secret:
      ssoToken: { mode: direct, ref: "op://Engineering/aws/sso" }
    requires:
      - precondition: aws-credentials
mcpServers:
  linear:
    transport: http
    url: https://mcp.linear.app/sse
    headers:
      Authorization: "Bearer xyz"
`)
	var m Manifest
	if err := yaml.Unmarshal(src, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	tool := m.CLITools["aws-cli"]
	if tool.Env["AWS_REGION"] != "eu-west-1" {
		t.Errorf("cliTool env = %v", tool.Env)
	}
	if _, ok := tool.Secret["ssoToken"]; !ok {
		t.Errorf("cliTool secret not parsed: %v", tool.Secret)
	}
	if len(tool.Requires) != 1 || tool.Requires[0].Precondition != "aws-credentials" {
		t.Errorf("cliTool requires not parsed: %v", tool.Requires)
	}
	srv := m.MCPServers["linear"]
	if srv.Transport != "http" || srv.URL != "https://mcp.linear.app/sse" {
		t.Errorf("http server not parsed: %+v", srv)
	}
	if srv.Headers["Authorization"] != "Bearer xyz" {
		t.Errorf("headers not parsed: %v", srv.Headers)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/manifest/ -run TestManifestUnmarshalsEnvAndHeaderFields`
Expected: FAIL — compile error, `srv.URL`, `srv.Headers`, `tool.Env`, `tool.Secret`, `tool.Requires` undefined.

- [ ] **Step 3: Add the fields**

In `internal/manifest/types.go`, in the `MCPServer` struct, add `URL` immediately after the `Transport` field and `Headers` immediately after the `Env` field. The struct becomes:

```go
// MCPServer is an MCP channel entry or template body (spec §5).
type MCPServer struct {
	Template     string            `yaml:"template"`
	Params       map[string]any    `yaml:"params"`
	Secret       map[string]any    `yaml:"secret"`
	Transport    string            `yaml:"transport"`
	URL          string            `yaml:"url"`
	Command      string            `yaml:"command"`
	Args         []string          `yaml:"args"`
	Version      string            `yaml:"version"`
	Env          map[string]string `yaml:"env"`
	Headers      map[string]string `yaml:"headers"`
	Capabilities map[string]any    `yaml:"capabilities"`
	Via          string            `yaml:"via"`
	Requires     []Require         `yaml:"requires"`
	Enabled      *bool             `yaml:"enabled"`
	Overridable  bool              `yaml:"overridable"`
}
```

In the same file, replace the `CLITool` struct with:

```go
// CLITool is an installable substrate binary (spec §7). Iteration 5 adds env,
// secret, and requires so a CLI tool can carry credentials and declare a
// dependency (typically a credential-file precondition).
type CLITool struct {
	VersionConstraint string                    `yaml:"versionConstraint"`
	Install           map[string]map[string]any `yaml:"install"`
	Check             map[string]any            `yaml:"check"`
	Env               map[string]string         `yaml:"env"`
	Secret            map[string]any            `yaml:"secret"`
	Requires          []Require                 `yaml:"requires"`
	Overridable       bool                      `yaml:"overridable"`
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/manifest/ -run TestManifestUnmarshalsEnvAndHeaderFields`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/manifest/types.go internal/manifest/types_test.go
git commit -m "Add url/headers to MCPServer and env/secret/requires to CLITool"
```

---

## Task 2: Transport field-set validation

A `transport: http` server must declare `url` and must not declare the stdio launch fields; a `stdio` server (the default) must not declare `url` or `headers`. The rule applies to both non-templated `mcpServers` and template-produced `mcpServer` bodies.

**Files:**
- Modify: `internal/manifest/validate.go`
- Test: `internal/manifest/validate_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/manifest/validate_test.go`:

```go
func TestValidateRejectsHTTPMCPWithoutURL(t *testing.T) {
	m := &Manifest{Version: 1, MCPServers: map[string]MCPServer{
		"s": {Transport: "http"},
	}}
	d := asDiagnostic(t, Validate(m))
	if !strings.Contains(d.Summary, "no url") {
		t.Errorf("summary = %q", d.Summary)
	}
	if d.Path != "mcpServers.s" {
		t.Errorf("path = %q, want mcpServers.s", d.Path)
	}
}

func TestValidateRejectsHTTPMCPWithStdioFields(t *testing.T) {
	m := &Manifest{Version: 1, MCPServers: map[string]MCPServer{
		"s": {Transport: "http", URL: "https://x", Command: "npx"},
	}}
	d := asDiagnostic(t, Validate(m))
	if !strings.Contains(d.Summary, "stdio-only fields") {
		t.Errorf("summary = %q", d.Summary)
	}
}

func TestValidateRejectsStdioMCPWithHeaders(t *testing.T) {
	m := &Manifest{Version: 1, MCPServers: map[string]MCPServer{
		"s": {Command: "echo", Headers: map[string]string{"X": "y"}},
	}}
	d := asDiagnostic(t, Validate(m))
	if !strings.Contains(d.Summary, "http-only fields") {
		t.Errorf("summary = %q", d.Summary)
	}
}

func TestValidateAcceptsHTTPMCPWithURLAndHeaders(t *testing.T) {
	m := &Manifest{Version: 1, MCPServers: map[string]MCPServer{
		"s": {Transport: "http", URL: "https://x", Headers: map[string]string{"X": "y"}},
	}}
	if err := Validate(m); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateRejectsHTTPTemplateWithoutURL(t *testing.T) {
	m := &Manifest{Version: 1, Templates: map[string]Template{
		"t": {Produces: Produces{MCPServer: &MCPServer{Transport: "http"}}},
	}}
	d := asDiagnostic(t, Validate(m))
	if !strings.Contains(d.Summary, "no url") {
		t.Errorf("summary = %q", d.Summary)
	}
	if d.Path != "templates.t" {
		t.Errorf("path = %q, want templates.t", d.Path)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/manifest/ -run TestValidate`
Expected: FAIL — the new tests get `nil` where a diagnostic is expected (no transport rule exists yet).

- [ ] **Step 3: Add the helper**

In `internal/manifest/validate.go`, add this function immediately before `func validateTools`:

```go
// validateMCPTransport enforces the disjoint field sets of the two MCP
// transports (spec §5.2): a transport: http server needs a url and rejects the
// stdio launch fields; a stdio server (the default) rejects the http-only url
// and headers fields. Both transports share one struct, so this is an explicit
// check, not a structural guarantee.
func validateMCPTransport(srv MCPServer, path string) *diag.Diagnostic {
	if srv.Transport == "http" {
		if srv.URL == "" {
			return &diag.Diagnostic{
				Summary: "http MCP server declares no url",
				Path:    path,
				Detail:  "A transport: http server is reached over HTTP and needs an endpoint.",
				Hint:    "Add a url field, e.g.  url: https://mcp.example.com/sse",
			}
		}
		if srv.Command != "" || len(srv.Args) > 0 || srv.Version != "" {
			return &diag.Diagnostic{
				Summary: "http MCP server declares stdio-only fields",
				Path:    path,
				Detail:  "command, args, and version apply only to transport: stdio.",
				Hint:    "Remove them, or set transport: stdio.",
			}
		}
		return nil
	}
	if srv.URL != "" || len(srv.Headers) > 0 {
		return &diag.Diagnostic{
			Summary: "stdio MCP server declares http-only fields",
			Path:    path,
			Detail:  "url and headers apply only to transport: http.",
			Hint:    "Remove them, or set transport: http.",
		}
	}
	return nil
}
```

- [ ] **Step 4: Wire the helper into `Validate` — non-templated servers**

In `internal/manifest/validate.go`, in the `m.MCPServers` loop, the non-template branch currently ends with the `packageLaunchers` check. Replace that block:

```go
		if packageLaunchers[srv.Command] && srv.Version == "" {
			return &diag.Diagnostic{
				Summary: "package-launched server must pin an exact version",
				Path:    "mcpServers." + id,
				Detail:  fmt.Sprintf("Server %q launches via %s but declares no version.", id, srv.Command),
				Hint:    `Add a version field, e.g.  version: "1.2.3"`,
			}
		}
	}
```

with:

```go
		if packageLaunchers[srv.Command] && srv.Version == "" {
			return &diag.Diagnostic{
				Summary: "package-launched server must pin an exact version",
				Path:    "mcpServers." + id,
				Detail:  fmt.Sprintf("Server %q launches via %s but declares no version.", id, srv.Command),
				Hint:    `Add a version field, e.g.  version: "1.2.3"`,
			}
		}
		if d := validateMCPTransport(srv, "mcpServers."+id); d != nil {
			return d
		}
	}
```

- [ ] **Step 5: Wire the helper into `Validate` — template-produced servers**

In the same file, in the `m.Templates` loop, the `if srv := tmpl.Produces.MCPServer; srv != nil` block currently contains only the `packageLaunchers` check. Replace that block:

```go
		if srv := tmpl.Produces.MCPServer; srv != nil {
			if packageLaunchers[srv.Command] && srv.Version == "" {
				return &diag.Diagnostic{
					Summary: "package-launched server must pin an exact version",
					Path:    "templates." + id,
					Detail:  fmt.Sprintf("Template %q produces a server launched via %s with no version.", id, srv.Command),
					Hint:    `Add a version field to the template body, e.g.  version: "1.2.3"`,
				}
			}
		}
```

with:

```go
		if srv := tmpl.Produces.MCPServer; srv != nil {
			if packageLaunchers[srv.Command] && srv.Version == "" {
				return &diag.Diagnostic{
					Summary: "package-launched server must pin an exact version",
					Path:    "templates." + id,
					Detail:  fmt.Sprintf("Template %q produces a server launched via %s with no version.", id, srv.Command),
					Hint:    `Add a version field to the template body, e.g.  version: "1.2.3"`,
				}
			}
			if d := validateMCPTransport(*srv, "templates."+id); d != nil {
				return d
			}
		}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/manifest/`
Expected: PASS — all manifest tests, including the five new ones.

- [ ] **Step 7: Commit**

```bash
git add internal/manifest/validate.go internal/manifest/validate_test.go
git commit -m "Validate MCP transport field-set coupling for http and stdio"
```

---

## Task 3: Interpolate `headers` for template-produced MCP servers

The templated path already interpolates `env`. `headers` must follow it so a template body's `${...}` header values are expanded per instance.

**Files:**
- Modify: `internal/resolve/template.go`
- Test: `internal/resolve/template_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/resolve/template_test.go`:

```go
func TestInstantiateInterpolatesHeaders(t *testing.T) {
	tmpl := manifest.Template{
		Params: map[string]manifest.Param{
			"region": {Type: "string", Required: true},
		},
		Produces: manifest.Produces{
			MCPServer: &manifest.MCPServer{
				Transport: "http",
				URL:       "https://mcp.example.com",
				Headers:   map[string]string{"X-Region": "${params.region}"},
			},
		},
	}
	inst := manifest.MCPServer{Params: map[string]any{"region": "eu-west-1"}}
	out, err := Instantiate("svc", inst, tmpl, map[string]any{})
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	if out.MCPServer.Headers["X-Region"] != "eu-west-1" {
		t.Errorf("headers = %v, want X-Region=eu-west-1", out.MCPServer.Headers)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/resolve/ -run TestInstantiateInterpolatesHeaders`
Expected: FAIL — `out.MCPServer.Headers` is nil (`headers` is never interpolated or copied), so the lookup yields `""`.

- [ ] **Step 3: Interpolate headers in `Instantiate`**

In `internal/resolve/template.go`, inside `if src := tmpl.Produces.MCPServer; src != nil {`, the `env` loop is immediately followed by `var err error`. Insert the headers loop between them. The block becomes:

```go
		srv := *src
		srv.Env = map[string]string{}
		for k, v := range src.Env {
			ev, err := Interpolate(v, scope)
			if err != nil {
				return Instance{}, err
			}
			srv.Env[k] = ev
		}
		srv.Headers = map[string]string{}
		for k, v := range src.Headers {
			hv, err := Interpolate(v, scope)
			if err != nil {
				return Instance{}, err
			}
			srv.Headers[k] = hv
		}
		var err error
		srv.Requires, err = interpolateRequires(src.Requires, scope)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/resolve/ -run TestInstantiateInterpolatesHeaders`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/resolve/template.go internal/resolve/template_test.go
git commit -m "Interpolate headers when instantiating a template MCP server"
```

---

## Task 4: Fold `url` and `headers` into the MCP content hash

A change to a templated server's `url` or `headers` must be caught as drift, so both must feed the lockfile `contentHash`.

**Files:**
- Modify: `internal/resolve/pipeline.go`
- Test: `internal/resolve/pipeline_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/resolve/pipeline_test.go`:

```go
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
		if err := RunLock(dir); err != nil {
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/resolve/ -run TestLockPipelineHashesHeadersForDrift`
Expected: FAIL — `headers` is not part of the content hash, so the two locks produce an identical `contentHash` line.

- [ ] **Step 3: Add `url` and `headers` to the content hash**

In `internal/resolve/pipeline.go`, find the `entry.ContentHash` assignment inside `if out.MCPServer != nil {`. Replace:

```go
				entry.ContentHash = lockfile.ContentHash(map[string]any{
					"command": out.MCPServer.Command, "version": out.MCPServer.Version,
					"env": toAnyMap(out.MCPServer.Env),
				})
```

with:

```go
				entry.ContentHash = lockfile.ContentHash(map[string]any{
					"command": out.MCPServer.Command, "version": out.MCPServer.Version,
					"url": out.MCPServer.URL,
					"env": toAnyMap(out.MCPServer.Env), "headers": toAnyMap(out.MCPServer.Headers),
				})
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/resolve/`
Expected: PASS — all resolve tests, including the new drift test.

- [ ] **Step 5: Commit**

```bash
git add internal/resolve/pipeline.go internal/resolve/pipeline_test.go
git commit -m "Hash MCP url and headers so a change is caught as drift"
```

---

## Task 5: Update the manifest schema reference

**Files:**
- Modify: `spec/manifest-schema.md`

- [ ] **Step 1: Add the HTTP transport section**

In `spec/manifest-schema.md`, section §5 ends with §5.1 (pinned versions). The last line of §5.1 is the `integrity` paragraph, followed by a `---` line and `## 6`. Insert this new subsection between the end of §5.1 and that `---`:

```markdown

### 5.2 HTTP transport — `url` and `headers`

> Added by Iteration 5 — closes assessment gap #2.

A `transport: http` server is reached over HTTP rather than launched as a
subprocess. It declares a `url` (required) and optional request `headers`:

```yaml
mcpServers:
  linear:
    transport: http
    url: https://mcp.linear.app/sse
    headers:
      Authorization: "Bearer ${secret.token}"
    secret:
      token: { mode: direct, ref: "op://Engineering/linear/mcp" }
```

The two transports use disjoint field sets, enforced at validation:

- `transport: http` requires `url`; `command` / `args` / `version` are rejected.
- `transport: stdio` (the default) requires neither; `url` / `headers` are
  rejected.

Header values interpolate exactly like `env` (§4.4). A header that resolves
from a secret follows the same rule as a secret-bearing `env` value — it may be
written only to gitignored client config, never a committed file (design §13).
```

- [ ] **Step 2: Document the `file-exists` `mode` check**

In §6, the paragraph after the precondition example reads:

```markdown
`check` fails loudly with `remediation` text. The tool never tries to satisfy a
precondition.
```

Replace it with:

```markdown
`check` fails loudly with `remediation` text. The tool never tries to satisfy a
precondition.

The `file-exists` check takes a `path` and an optional `mode` (an octal string
such as `"0600"`). When `mode` is set, the check also verifies the file's
permission bits, flagging an over-permissive credential file. This is how a
CLI tool's dependency on a credential file is expressed — ainfra checks the
file, and deliberately never writes it (the environment primitive stays
reference-only, §3). A `cliTool` points at such a precondition with `requires`
(§7).
```

- [ ] **Step 3: Document the new `cliTool` fields**

In §7, the section currently shows one `cliTools` example then a paragraph beginning "If no `install` adapter matches". Insert the following immediately before that paragraph:

```markdown
> Added by Iteration 5: a `cliTool` also accepts `env`, `secret`, and
> `requires`.

```yaml
cliTools:
  aws-cli:
    versionConstraint: ">=2.0"
    install:
      brew: { formula: awscli }
    env:                                # written to the Claude Code settings.json env block
      AWS_REGION: "eu-west-1"
    secret:                             # inline secret bindings, as on an mcpServer
      ssoToken: { mode: direct, ref: "op://Engineering/aws/sso" }
    requires:
      - precondition: aws-credentials   # a credential file ainfra checks, never writes
```

A `cliTool`'s `env` is delivered through a Claude Code `settings.json` env
block, so it reaches every Bash tool call in a session — where the
credential-needing CLIs run. `secret` declares inline secret bindings,
referenced from `env` as `${secret.<name>}`. `requires` declares dependency
edges (§9), typically a `file-exists` precondition for a credential file.

> CLI tool and non-templated MCP server *resolution* at lock time (env/headers
> interpolation, graph edges, lock entries) is owned by the follow-up plan for
> non-templated entries; Iteration 5 adds and validates the fields.

```

- [ ] **Step 4: Commit**

```bash
git add spec/manifest-schema.md
git commit -m "Document HTTP MCP url/headers, cliTool env/secret/requires, file-exists mode"
```

---

## Task 6: Update the gap assessment

**Files:**
- Modify: `docs/assessment-vs-real-config.md`

- [ ] **Step 1: Mark the two coverage-table rows Clean**

In `docs/assessment-vs-real-config.md` §2, replace this row:

```markdown
| HTTP MCP servers with auth `headers` | `mcpServers` | Bends — schema has no `headers` map |
```

with:

```markdown
| HTTP MCP servers with auth `headers` | `mcpServers` | Clean — `url` + `headers` added (Iteration 5) |
```

And replace this row:

```markdown
| secrets materialised to *files* | environment | Gap — env primitive does env vars, not files |
```

with:

```markdown
| secrets materialised to *files* | environment | Clean (verify-only) — ainfra checks the file via a precondition; never writes it (Iteration 5) |
```

- [ ] **Step 2: Add the Iteration 5 section**

§4 ends with the line `This converts the two biggest "Gap" rows above into "Clean."`. Immediately after that line, insert:

```markdown

## 5. Iteration 5 — what this change adds

Three schema additions, closing two more gaps:

- **`mcpServers.url` + `headers`** (manifest §5.2) — HTTP MCP servers can
  declare an endpoint and auth headers.
- **`cliTools.env` / `secret` / `requires`** (manifest §7) — CLI tools get
  environment variables (delivered via the Claude Code `settings.json` env
  block), inline secret bindings, and dependency edges.
- **`file-exists` precondition `mode`** (manifest §6) — secret-to-file is
  modelled verify-only: ainfra checks a credential file exists with the right
  permissions and never writes it, keeping the environment primitive
  reference-only.

This is a schema iteration; CLI tool and non-templated MCP *resolution* at lock
time is deferred to the follow-up plan for non-templated entries.
```

- [ ] **Step 3: Renumber the following two section headings**

Change `## 5. Gaps still open` to `## 6. Gaps still open`, and `## 6. The honest bottom line` to `## 7. The honest bottom line`.

- [ ] **Step 4: Update the open-gaps list**

In the renumbered §6 ("Gaps still open"), the list currently has six items. Replace the entire numbered list (items 1 through 6) with:

```markdown
1. **Scheduled jobs.** The 5 headless `claude -p` cron runs. A full design
   exists — a `scheduledJobs` targeted-infrastructure channel
   (`docs/superpowers/specs/2026-05-21-scheduled-jobs-design.md`). It was built
   as Iteration 4 and then reverted from `main`; it is deferred for now, not
   abandoned.
2. **Plugin `source` git + subpath.** Real marketplaces use GitHub sources.
3. **`pip` / `composer` `cliTool` adapters**, and acceptance that
   build-from-source binaries stay declare-and-check.
4. **Per-developer `rules` templating.** The real `CLAUDE.md` is rendered per
   developer; the `rules` channel is static-file-oriented.
```

Then replace the paragraph that follows the list:

```markdown
The **permission `ask` tier** was previously listed here; the `tools` channel
now models the full three-tier `allow` / `ask` / `deny` policy, so that gap is
closed.
```

with:

```markdown
**HTTP MCP `headers`** and **secret-to-file** were previously listed here;
Iteration 5 closes both (see §5). The **permission `ask` tier** was closed
earlier by the three-tier `tools` channel.
```

- [ ] **Step 5: Commit**

```bash
git add docs/assessment-vs-real-config.md
git commit -m "Record Iteration 5 and close the headers and secret-to-file gaps"
```

---

## Task 7: Exercise the new fields in the showcase example

Add an HTTP MCP server, a credential-file precondition, and CLI `env` + `requires` to the multi-database example — the same way Iteration 3 added a hook and a command.

**Files:**
- Modify: `examples/multi-database/ainfra.yaml`

- [ ] **Step 1: Add the credential-file precondition**

In `examples/multi-database/ainfra.yaml`, the `preconditions:` block currently contains only `vpn-tvt-internal`. Add a second precondition. After the `vpn-tvt-internal` block's `remediation:` line, and before the `# --- Installable substrate` comment, insert:

```yaml
  mysql-defaults-file:
    description: The mysql client reads connection defaults from ~/.my.cnf.
    check:
      type: file-exists
      path: ~/.my.cnf
      mode: "0600"
    remediation: "Create ~/.my.cnf with a [client] section, then: chmod 600 ~/.my.cnf"
```

- [ ] **Step 2: Add `env` and `requires` to the `mysql-client` CLI tool**

Replace the `mysql-client` entry under `cliTools:`:

```yaml
  mysql-client:
    versionConstraint: ">=8.0"
    install:
      brew: { formula: mysql-client }
      apt:  { package: mysql-client }
    check:
      command: "mysql --version"
      versionRegex: 'Ver (\d+\.\d+\.\d+)'
```

with:

```yaml
  mysql-client:
    versionConstraint: ">=8.0"
    install:
      brew: { formula: mysql-client }
      apt:  { package: mysql-client }
    check:
      command: "mysql --version"
      versionRegex: 'Ver (\d+\.\d+\.\d+)'
    env:
      MYSQL_HISTFILE: /dev/null         # team-wide CLI default, delivered via settings.json env
    requires:
      - precondition: mysql-defaults-file
```

- [ ] **Step 3: Add a non-templated HTTP MCP server**

The `mcpServers:` block ends with the `reporting-db` instance. After the `reporting-db` block, append:

```yaml

  # A non-templated HTTP MCP server: an endpoint plus auth headers (Iteration 5).
  linear:
    transport: http
    url: https://mcp.linear.app/sse
    headers:
      Authorization: "Bearer ${secret.token}"
    secret:
      token: { mode: direct, ref: "op://Engineering/linear/mcp-token" }
```

- [ ] **Step 4: Verify the example parses and validates**

Build the CLI and run its validate command against the example. Discover the exact invocation with `go run . --help` if needed; it is expected to be:

Run: `go run . validate examples/multi-database`
Expected: no diagnostics; clean exit. If the command instead operates on the working directory, run it from inside `examples/multi-database`.

If validation reports a diagnostic, fix the example to satisfy it before continuing — do not change the validator.

- [ ] **Step 5: Commit**

```bash
git add examples/multi-database/ainfra.yaml
git commit -m "Exercise url/headers, cliTool env/requires, and file-exists mode in the example"
```

---

## Task 8: Full verification

**Files:** none — verification only.

- [ ] **Step 1: Run the full test suite**

Run: `go test ./...`
Expected: PASS — every package, no failures.

- [ ] **Step 2: Build and vet**

Run: `go build ./... && go vet ./...`
Expected: no output, clean exit.

- [ ] **Step 3: Confirm the generated JSON Schema picks up the new fields**

The JSON Schema is generated by reflection from the loader structs. Regenerate it and confirm the new fields appear. Discover the subcommand with `go run . --help` if needed; it is expected to be `schema`:

Run: `go run . schema`
Then inspect the generated schema output (or file) and confirm it contains `url`, `headers`, `env`, `secret`, and `requires`. For example:

Run: `go run . schema | grep -E '"(url|headers|env|secret|requires)"'`
Expected: matches for each new field name.

If the schema command writes to a generated, gitignored file instead of stdout, inspect that file. The generated artifact is gitignored (see commit `d337087`) — do not commit it.

- [ ] **Step 4: Final commit (only if anything is uncommitted)**

```bash
git status
```

Expected: clean working tree — all changes committed in Tasks 1–7. If anything remains, commit it with a message describing what and why.

---

## Self-Review Notes

- **Spec coverage:** §1 url/headers → Tasks 1, 3, 4, 5, 7. §1.1 transport validation → Task 2. §2 cliTool env/secret → Tasks 1, 5, 7. §3 secret-to-file (requires + `mode`) → Tasks 1, 5, 7. §4.2 content hash → Task 4. §5 schema-change table → Task 1. §6 files → all tasks. §8 example coverage → Task 7. Assessment doc → Task 6.
- **Deferred by design (spec §7):** CLI tool / non-templated MCP lock-time resolution — no task, intentionally. `CLITool.Requires` parses and validates but is not yet graph-active; this matches the codebase's existing treatment of non-templated `MCPServer.Requires`.
- **Type consistency:** `validateMCPTransport(srv MCPServer, path string) *diag.Diagnostic` — defined Task 2 Step 3, called Task 2 Steps 4–5. `toAnyMap` — pre-existing helper in `pipeline.go`, reused in Task 4. `Interpolate` — pre-existing, reused in Task 3.
