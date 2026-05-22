# Per-developer Rules Templating Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax.

**Goal:** The `rules` channel renders `{{KEY}}` placeholders from manifest-declared, maintainer-sourced variables, so `ainfra apply` writes a per-developer `CLAUDE.md` with no other onboarding step.

**Architecture:** A new top-level `vars:` block declares each variable's *source* (`value` / `env` / `command`). `resolve` gains pure helpers to merge, resolve, and substitute vars; `RunLock` and `RenderResources` thread a `CommandRunner` so `command` vars resolve per machine. A `rules` entry with `template: true` has its content rendered.

**Tech Stack:** Go. Tests use the existing `provider` fakes (`FakeRunner`).

See `docs/superpowers/specs/2026-05-22-rules-templating-design.md`.

---

### Task 1: Manifest schema — `Var` type, `vars:` block, `Rule.Template`

**Files:**
- Modify: `internal/manifest/types.go`
- Modify: `internal/manifest/validate.go`
- Modify: `internal/manifest/types_test.go`
- Modify: `internal/manifest/validate_test.go`

- [ ] **Step 1: Write failing tests** in `types_test.go` — YAML decode of a `vars:` block:
  a scalar entry `TEAM: "Acme"` decodes to `Var{From: "value", Value: "Acme"}`; a mapping
  `X: { from: command, command: "git config user.name" }` decodes to `Var{From: "command", Command: "..."}`;
  `Y: { from: env, env: "USER" }` decodes to `Var{From: "env", Env: "USER"}`. And in `validate_test.go`:
  a `Var` with `from: bogus` fails `Validate`; `from: env` with no `env` fails; `from: command` with no
  `command` fails; a valid `vars:` block plus a `rules:` entry with `template: true` passes.

- [ ] **Step 2: Run the tests, confirm they fail.** `go test ./internal/manifest/...`

- [ ] **Step 3: Add the types** to `types.go`:
  ```go
  // Var is one template variable. It is written either as a scalar (a literal
  // value) or as a mapping declaring how the value is sourced.
  type Var struct {
      From    string `yaml:"from"`    // "value" | "env" | "command"
      Value   string `yaml:"value"`
      Env     string `yaml:"env"`
      Command string `yaml:"command"`
  }

  // UnmarshalYAML accepts a scalar (literal value) or a mapping form.
  func (v *Var) UnmarshalYAML(node *yaml.Node) error {
      if node.Kind == yaml.ScalarNode {
          v.From, v.Value = "value", node.Value
          return nil
      }
      type rawVar Var
      var r rawVar
      if err := node.Decode(&r); err != nil {
          return err
      }
      *v = Var(r)
      if v.From == "" {
          v.From = "value"
      }
      return nil
  }
  ```
  Add `Vars map[string]Var \`yaml:"vars"\`` to `Manifest`. Add `Template bool \`yaml:"template"\``
  to the `Rule` struct. Confirm `types.go` imports `gopkg.in/yaml.v3` (add if absent).

- [ ] **Step 4: Add validation** to `validate.go` `Validate`: loop `m.Vars` in sorted key order;
  reject a `From` not in `{value, env, command}`; reject `from: env` with empty `Env`; reject
  `from: command` with empty `Command`. Emit a `*diag.Diagnostic` naming the variable, matching the
  existing diagnostic style in the file.

- [ ] **Step 5: Run the tests, confirm they pass.** `go test ./internal/manifest/...`

- [ ] **Step 6: Commit.**
  ```bash
  git add internal/manifest && git commit -m "Add vars: block and Rule.template to the manifest schema"
  ```

---

### Task 2: `resolve/vars.go` — collect, resolve, substitute

**Files:**
- Create: `internal/resolve/vars.go`
- Create: `internal/resolve/vars_test.go`

- [ ] **Step 1: Write failing tests** in `vars_test.go`:
  - `substituteVars`: `"hi {{NAME}}"` + `{NAME: "Al"}` → `"hi Al"`; a repeated placeholder is
    replaced everywhere; multiple distinct keys; an undefined `{{MISSING}}` returns an error
    mentioning `MISSING`; content with no placeholders is returned unchanged; an unclosed `"{{ x"`
    is left alone (no error).
  - `collectVars`: a team-only manifest returns its vars; with team+personal, the team value wins
    for a shared key (mirrors `collectSecrets` precedence); a missing layer is skipped.
  - `resolveVars`: a `value` Var returns its literal; an `env` Var returns `os.Getenv` of its `Env`
    (set one via `t.Setenv`); a `command` Var runs via the injected runner and returns trimmed
    stdout (use `provider.NewFakeRunner()`, script `sh -c "git config user.name"`); a command whose
    `FakeResult` carries an error makes `resolveVars` return an error.

- [ ] **Step 2: Run the tests, confirm they fail.** `go test ./internal/resolve/ -run Vars`

- [ ] **Step 3: Implement `vars.go`:**
  - `collectVars(layers map[manifest.Layer]*manifest.Manifest) map[string]manifest.Var` — iterate
    `LayerTeam, LayerRepo, LayerPersonal`, first-seen-wins (same shape as `collectSecrets`).
  - `resolveVars(specs map[string]manifest.Var, runner provider.CommandRunner) (map[string]string, error)`
    — for each spec: `from: value` → `Value`; `from: env` → `os.Getenv(Env)`; `from: command` →
    `runner.Run("sh", "-c", Command)`, trim trailing whitespace from stdout; on a runner error,
    return an error naming the variable and command.
  - `substituteVars(content string, vars map[string]string) (string, error)` — match
    `{{KEY}}` with the regexp `\{\{([A-Za-z_][A-Za-z0-9_]*)\}\}`; for each match, if the key is
    absent from `vars`, return an error `references undefined variable "<KEY>"`; otherwise replace.
    Use `regexp.ReplaceAllStringFunc` with an error captured in a closure variable, or a manual
    scan — either is fine.

- [ ] **Step 4: Run the tests, confirm they pass.** `go test ./internal/resolve/ -run Vars`

- [ ] **Step 5: Commit.**
  ```bash
  git add internal/resolve/vars.go internal/resolve/vars_test.go
  git commit -m "Add vars resolution helpers (collect, resolve, substitute)"
  ```

---

### Task 3: Render templated rules in `RunLock` and `RenderResources`

**Files:**
- Modify: `internal/resolve/pipeline.go` (`RunLock`)
- Modify: `internal/resolve/render.go` (`RenderResources`)
- Modify: `cmd/ainfra/cmd_lock.go`, `cmd/ainfra/commands.go` (callers)
- Modify: `internal/resolve/pipeline_test.go`, `internal/resolve/secret_pipeline_test.go`, `internal/resolve/render_test.go` (callers)

- [ ] **Step 1: Write failing tests** in `render_test.go`: a manifest with
  `vars: { NAME: "Dev" }` and a rule `r1: { source: ./r.md, template: true }` whose source file
  contains `{{NAME}}` — `RenderResources` returns the `rules` resource with `Payload["content"]`
  equal to the substituted text (`Dev`), no literal `{{NAME}}`. A second rule `r2` with
  `template: false` (or absent) keeps `{{NAME}}` literal. A `template: true` rule whose source
  references `{{UNDEFINED}}` makes `RenderResources` return an error.

- [ ] **Step 2: Run the tests, confirm they fail** (and the signature change breaks compilation).
  `go test ./internal/resolve/ -run Render`

- [ ] **Step 3: Thread the runner.** Change signatures to
  `RunLock(dir string, runner provider.CommandRunner) error` and
  `RenderResources(dir string, runner provider.CommandRunner) (map[string][]provider.Resource, error)`.
  Inside `RenderResources`, pass `runner` to its internal `RunLock` call.
  Update production callers to pass `provider.ExecRunner{}`:
  - `cmd/ainfra/cmd_lock.go` — `resolve.RunLock(ctx.Dir, provider.ExecRunner{})`
  - `cmd/ainfra/commands.go` — `resolve.RenderResources(dir, provider.ExecRunner{})`
  Update every test caller in the three resolve test files to pass `provider.ExecRunner{}` (tests
  that exercise no `command` vars are unaffected by the choice of runner).

- [ ] **Step 4: Render templated rules.** In `render.go`'s `rules` block: when `r.Template` is
  true, build `resolved, err := resolveVars(collectVars(layers), runner)` (compute once, reuse
  across rules), then `content, err = substituteVars(content, resolved)`; propagate any error out
  of `RenderResources`. In `pipeline.go` `RunLock`'s rules loop: for a `template: true` rule, read
  its source file, render it the same way, and compute the lock `ContentHash` over the *rendered*
  content (non-templated rules keep their current hashing).

- [ ] **Step 5: Run the tests, confirm they pass; build everything.**
  `go test ./... ` and `go build ./...`

- [ ] **Step 6: Commit.**
  ```bash
  git add internal/resolve cmd/ainfra
  git commit -m "Render template: true rules from resolved vars"
  ```

---

### Task 4: Update the tvt-config manifest and verify end to end

**Files:**
- Modify: `ainfra.yaml` in the `claude-config` repo (`trein-vertraging/claude-config`)
- Modify: `docs/assessment-vs-real-config.md` (mark #5 closed)

- [ ] **Step 1: Add the `vars:` block** to `claude-config/ainfra.yaml` (team layer):
  ```yaml
  vars:
    FULL_NAME:       { from: command, command: "git config user.name" }
    EMAIL:           { from: command, command: "git config user.email" }
    GITHUB_USERNAME: { from: command, command: "gh api user --jq .login" }
  ```

- [ ] **Step 2: Mark the team rule templated.** In the `rules:` block, add `template: true` to the
  `team-claude-md` entry.

- [ ] **Step 3: Verify.** Rebuild the binary; run `ainfra validate` (expect pass), `ainfra lock`,
  and a contained `apply` with an isolated `HOME`. Confirm the written `~/.claude/CLAUDE.md` (under
  the sandbox `HOME`) contains the real git identity and no literal `{{...}}`.

- [ ] **Step 4: Update the gap report** — in `ainfra/docs/assessment-vs-real-config.md`, move #5
  out of "Gaps still open" and note that per-developer `CLAUDE.md` templating ships.

- [ ] **Step 5: Commit** — the `claude-config` manifest change in that repo; the gap-report change
  in the `ainfra` repo. No emoji, no `Co-Authored-By`.

---

## Self-review notes

- **Spec coverage:** `Var` type + `vars:` + `Rule.Template` → T1; `collectVars`/`resolveVars`/
  `substituteVars` → T2; runner threading + render integration + rendered-content hashing → T3;
  tvt-config manifest + verification → T4. Out-of-scope items (interactive capture, `init`
  scaffolding) correctly absent.
- **Runner type:** `provider.CommandRunner` is the interface; `provider.ExecRunner{}` the
  production impl; `provider.NewFakeRunner()` the test double — consistent across all tasks.
- **No circular import:** `resolve` already imports `provider` (in `render.go`); `provider` does
  not import `resolve`.
