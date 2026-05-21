# ainfra — Design

This is the canonical, decided design. Sections 0–11 are stable. Section 12
lists the two open items. The manifest *syntax* (Phase 1) is proven by the
[validation gate](validation.md), not by this document.

## 0. What this is

ainfra keeps a whole dev team's AI tooling in sync. A team's AI development
setup drifts the moment each developer installs it — different MCP servers,
skills, hooks, and rules files, with no way to see the gap. ainfra makes that
setup config-as-code: defined once, layered, and reconciled onto every
developer's machine. A lockfile pins and hashes every resolved version, so
"in sync" is verifiable and drift is caught.

Mechanically it is a Terraform-style CLI: a declarative manifest, `plan` before
`apply`, a lockfile separating desired from observed state.

The market position is decided: runtime governance (MCP gateways) is a
saturated, funded category on the official MCP roadmap — **do not build a
gateway**. The empty, unowned cell is declarative, cross-channel reconciliation
with a lockfile. That is the product. The tool *consumes* gateways, secrets
managers, and package managers as pluggable backends; it owns none of their
runtimes.

## 1. Scope — eight channels, two cross-cutting primitives

Eight configurable channels:

1. **MCP servers** — `.mcp.json` connections
2. **Skills** — `SKILL.md` bundles materialized into `.claude/skills/`. ainfra reconciles *externally-sourced* skills (pinned, hashed); skills a repo commits to its own `.claude/skills/` arrive with `git clone` and are out of scope.
3. **Plugins** — installable bundles
4. **CLAUDE.md / rules** — static context files
5. **Tools / toolsets** — which built-ins are enabled, permission policies
6. **CLI tooling** — binaries the other channels depend on; a *substrate*, not a peer (§6)
7. **Hooks** — automation bound to Claude Code lifecycle events
8. **Commands** — slash commands

> Channels 7-8 were added in Iteration 3, after assessing the schema against a
> real team config repo. See [assessment-vs-real-config.md](assessment-vs-real-config.md).
> A ninth, targeted-infrastructure channel (scheduled jobs) was designed and
> briefly built in Iteration 4, then reverted from `main`; the design is kept
> at `superpowers/specs/2026-05-21-scheduled-jobs-design.md`.

Two cross-cutting primitives, touching every channel:

- **Environment** — secret/config values, in three modes (§5)
- **Dependency graph** — `requires:` edges between channels and what they need (§7)

## 2. Locked architectural decisions

- **Layered topology** — org/team + repo + personal layers, merged into one
  resolved state. A flat manifest can express neither org policy nor "this is
  just mine."
- **Terraform-shaped** — declarative manifest; `plan` before `apply`; a lockfile
  separates desired from observed state; every channel is a provider behind a
  common interface.
- **Local CLI** — the developer runs it; no daemon, no required CI.
- **v1 = Distribute + lockfile; Govern deferred.** The lockfile alone closes
  reproducibility *and* rug-pull/drift detection. Vetting/approval/rollback
  workflows are product #2, built later on the same lockfile.

## 3. Conflict resolution — Option C

When personal and team layers define the same entry: **the team layer wins by
default; a team entry may carry `overridable: true` to sanction a personal
override.** This is Anthropic's enterprise > personal > project ordering as the
default, plus a deliberate opt-in departure — reconciling "follow Anthropic"
with "flexibility for devs." Phase 4 is the mechanical expression of this rule.

## 4. The MCP channel is a layered target

- **Client-side config** — what lands in each dev's `.mcp.json`. Always in
  scope. The core.
- **Gateway-side config** — server registrations, groups, allow-lists on a
  gateway. *Optional*, behind a pluggable adapter (Terraform-provider style).
- **Gateway runtime** — never in scope. Consumed, never owned.

Direct mode (no gateway) is the baseline every install must support. The
gateway path is an upgrade, never a prerequisite.

## 5. The environment primitive — three credential modes

1. **`direct` + literal value** — the dev sets the env var themselves. The
   always-works baseline.
2. **`direct` + secret-manager reference** — the manifest holds a *pointer*
   (`op://…`, Doppler, Vault, SOPS), never a value; resolved at apply/session
   time by shelling out to the team's secrets manager via a pluggable resolver.
3. **`brokered`** — a gateway holds the credential; no per-dev secret exists.

Shared-vs-personal falls out of mode 2: a shared secret is one vault item the
team reads; a personal secret is each dev's own item at the same logical path.

**Non-goal:** the tool never stores, encrypts, or syncs secret values —
references only.

## 6. CLI tooling — substrate, install-owning

CLI tooling produces no config artifact of its own; it is a substrate channels
1–5 rest on. **The tool installs CLI tools directly** via package-manager
adapters (`brew`, `apt`, `npm -g`, `uv`, `cargo`, direct-download) behind a
common contract (`is-installed?`, `install`, `installed-version`). The manifest
declares a tool *abstractly* (name + version constraint); each platform adapter
satisfies it per-OS. Tools no adapter can install fall back to declare-and-check
with an actionable error.

**Reproducibility caveat:** Skills, Plugins, MCP packages, and CLAUDE.md pin to
an exact version + content hash — strong reproducibility. CLI tooling pins the
declared constraint and records the resolved installed version, but `brew`/`apt`
do not always allow exact pinning — so CLI reproducibility is **best-effort**:
`check` flags a mismatch, but byte-identical binaries are not guaranteed.

## 7. The dependency graph — the connective layer

MCP servers are solo-first and declare *what they connect to* but never *what
must be true for that connection to work*. Infrastructure (VPN, SSH tunnels) is
legitimately layered. Neither owns the dependency between them. That undeclared
cross-layer dependency is what the tool exists to make machine-readable.

Every channel entry can declare `requires: [...]`. Node types:

- **CLI tool** — installable (§6).
- **Background service** — a persistent process (e.g. an SSH tunnel) a channel
  needs running for its lifetime. The tool *declares, checks, and generates the
  service definition* (start/stop scripts + Claude Code `hooks` wiring). It does
  **not** supervise, restart, or own the daemon lifecycle.
- **Precondition** — something the tool can only verify, never provision (VPN
  reachability, an SSH key's presence). Declared, checked, fails loudly.

## 8. Modularity — templates

The tool must **not** hardcode knowledge of specific MCP servers. It knows
generic capabilities; an MCP server is *described* as a bundle of them.

Templates are first-class. An MCP server shape is defined once and instantiated
N times. The multi-database case is the validating stress test: four databases
must be four instances of one template — not four copy-pasted blocks.

Three things the manifest must cleanly separate:

- **Template** — the shared shape.
- **Instance** — the per-use differences (host, db name, ssh user).
- **Tool-owned resolved fields** — ports, generated script paths, rendered hook
  blocks. Declared by no human; computed by the tool. Port collision becomes
  structurally impossible because no human types a port.

A template is also the unit of *reuse across teams*. Because a team/org layer
(§2) may itself declare `templates:`, an org publishes a template library once
— `mysql-over-ssh-tunnel`, a standard hook shape — and every repo that
`extends:` that layer instantiates it without copying it. The shared shape
lives in one place; a repo supplies only its instances.

## 9. Non-goals

No gateway runtime. No credential brokering / token holding. No secret storage
(references + resolver only). No Managed Agents. No Govern workflow in v1. No
process supervision of background services. No OS-level configuration beyond
what a normal package install needs.

## 10. Build phases

- **Phase 0 — Foundation.** This repo and design.
- **Phase 1 — Manifest schema (`ainfra.yaml`).** See [spec](../spec/manifest-schema.md).
- **Phase 2 — Lockfile schema (`ainfra.lock`).** See [spec](../spec/lockfile-schema.md).
- **Phase 3 — Channel provider interface.** One contract every channel
  implements: `resolve() → plan() → apply() → check()`.
- **Phase 4 — Resolution & precedence engine.** Merges the three layers under
  the Option-C rule.
- **Phase 5 — CLI surface.** `init`, `plan`, `apply`, `check`, `lock`.

## 11. The validation gate

Before implementation code, the Phase 1 + 2 schemas are run on paper against
five scenarios. See [validation.md](validation.md). If the schemas cannot
express all five cleanly, the schema is iterated — not the code.

## 12. Resolved decisions

1. **Background-service install boundary.** Resolved as stated in §7: the tool
   declares, checks, and *generates* the service definition, and does **not**
   run or own the daemon.
2. **Naming.** Resolved: the project is named **`ainfra`** — CLI binary,
   module, and the `ainfra.yaml` / `ainfra.lock` artifacts.

## 13. Config-as-code failure modes ainfra designs against

Config-as-code is a well-trodden category, and its tools have failed in
repeatable ways. Each row below is a known failure mode, where it bit earlier
tooling, and the design decision that forecloses it here. These are
constraints, not aspirations — they are enforced by code and tests.

| Failure mode | Where it bit earlier | ainfra's defense |
|--------------|----------------------|------------------|
| **Permissive parsing** — a misspelled key is silently dropped, so config that never applied looks applied | Ansible ignored unknown keys; Kubernetes had to retrofit strict decoding (`--validate=strict`, server-side apply) | The loader decodes with `KnownFields(true)`: an unknown key is a hard error with a hint, never a silent drop. |
| **Schema/docs drift** — a hand-maintained schema or doc slowly diverges from what the tool accepts | Common wherever reference docs are written by hand | The JSON Schema is *generated by reflection from the Go structs the loader uses* (`ainfra schema`). It cannot drift from the parser. |
| **No lockfile / floating versions** — "works on my machine"; a dependency changes with no diff | npm, Bundler, and Terraform all added lockfiles only after this hurt | `ainfra.lock` records resolved versions and content hashes; package-launched MCP servers must pin an exact version (§4 validation gate). |
| **Surprising merge precedence** — overrides resolve in ways the author cannot predict | Helm value merging and Kustomize patch ordering are routinely cited | Precedence is one explicit table (Option C, §3); singleton list channels union-merge with a documented `deny > ask > allow` rule; iteration is deterministically sorted. |
| **Secrets in committed config** — credentials leak through `values.yaml`, `.tfvars`, `.env` | Endemic across the category | The environment primitive stores *references only* (§5); the lockfile is layered so personal config never lands in a committed file. |
| **Undetected post-apply drift** — config is applied once, the environment then mutates unnoticed | Any apply-once tool without a verify step | `check` recomputes content hashes against the lockfile and reports drift; it is exit-code-clean for CI. |
| **Apply without preview** — a change is reconciled before anyone sees its effect | The mistake Terraform's `plan` exists to prevent | `plan` is a required, side-effect-free step before `apply`. |
| **Schema too rigid** — teams cannot express their case, so they fork or copy-paste | Brittle, hardcoded config tools | Templates + layers (§8) and generic capability toggles (§8 — no hardcoded server knowledge) keep the schema extensible without forking. |
