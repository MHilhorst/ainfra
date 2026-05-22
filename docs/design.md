# ainfra — Design

This is the canonical, decided design. Sections 0–13 are stable; §12 records the
decisions that were once open and are now resolved. Section 14 covers the
agent-targeting axis added during Phase 3. The manifest *syntax* (Phase 1) is
proven by the [validation gate](validation.md), not by this document.

## 0. What this is

ainfra keeps a whole dev team's AI tooling in sync. The moment each developer
installs the team's AI setup, it starts to drift (config quietly falling out of
sync with what was declared) — different MCP servers, skills, hooks, and rules
files, with no way to see the gap. ainfra makes that setup config-as-code:
instead of each person configuring tools by hand, the team writes the setup down
in one file, layers it, and reconciles (brings each machine's config in line
with the manifest — `ainfra.yaml`, the file describing the team's setup) it onto
every developer's machine. The lockfile — `ainfra.lock`, the auto-generated file
that pins exact versions — also stores a content hash of each resolved version,
so "in sync" is verifiable and drift is caught.

Mechanically it works like Terraform: a declarative manifest, a `plan` step
before `apply`, and a lockfile that separates desired state (what you want) from
observed state (what is actually on the machine). What it reconciles onto a
machine is the native config the AI tools already read — `.mcp.json`, `.claude/`
bundles, `CLAUDE.md`. So ainfra is never a runtime that anything depends on:
remove it and the files it wrote keep working.

The market position is decided. Runtime governance (MCP gateways) is a
saturated, funded category on the official MCP roadmap — **do not build a
gateway**. The empty, unowned space is declarative, cross-channel reconciliation
with a lockfile. That is the product. The tool *consumes* gateways, secrets
managers, and package managers as pluggable backends; it owns none of their
runtimes.

## 1. Scope — eight channels, two cross-cutting primitives

A channel is one category of AI-tooling config — MCP servers, hooks, and so on.
There are eight configurable channels:

1. **MCP servers** — `.mcp.json` connections
2. **Skills** — `SKILL.md` bundles materialized into `.claude/skills/`. ainfra reconciles *externally-sourced* skills (pinned, hashed); skills a repo commits to its own `.claude/skills/` arrive with `git clone` and are out of scope.
3. **Plugins** — installable bundles
4. **CLAUDE.md / rules** — static context files
5. **Tools / toolsets** — which built-ins are enabled, permission policies
6. **CLI tooling** — binaries the other channels depend on; the underlying tools the other channels rest on, not a peer channel (§6)
7. **Hooks** — automation bound to Claude Code lifecycle events
8. **Commands** — slash commands

> Channels 7-8 were added in Iteration 3, after assessing the schema against a
> real team config repo. See [assessment-vs-real-config.md](assessment-vs-real-config.md).
> A ninth, targeted-infrastructure channel (scheduled jobs) was designed and
> briefly built in Iteration 4, then reverted from `main`; the design is kept
> at `superpowers/specs/2026-05-21-scheduled-jobs-design.md`.

Two building blocks that touch every channel:

- **Environment** — secret/config values, in three modes (§5)
- **Dependency graph** — `requires:` edges between channels and what they need (§7)

A third axis cuts across all eight channels: the **target agent** (§14). The
channel list above is what Claude Code can render; another agent renders only
some of them, and each entry can use capability-gating (marking which entries
apply to which AI agent).

## 2. Locked architectural decisions

- **Layered structure** — org/team, repo, and personal layers (a layer is one
  source of config that gets merged with the others), combined into one resolved
  state. A flat manifest can express neither org policy nor "this is just mine."
- **Terraform-shaped** — declarative manifest; `plan` before `apply`; a lockfile
  separates desired from observed state; every channel is a provider (the
  pluggable component that handles one channel) behind a common interface.
- **Local CLI** — the developer runs it; no daemon, no required CI.
- **v1 = Distribute + lockfile; Govern deferred.** The lockfile alone closes
  reproducibility *and* detection of rug-pulls (a dependency changing silently
  upstream after you adopted it) and drift. Vetting, approval, and rollback
  workflows are product #2, built later on the same lockfile.

## 3. Conflict resolution — Option C

When the personal and team layers define the same entry, **the team layer wins
by default. A team entry may carry `overridable: true` to allow a personal
override.** This follows Anthropic's enterprise > personal > project ordering as
the default, plus one deliberate opt-in departure — reconciling "follow
Anthropic" with "flexibility for devs." Phase 4 implements this rule.

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
   (`op://…`, Doppler, Vault, SOPS), never a value. The pointer is resolved at
   apply or session time by calling out to the team's secrets manager through a
   pluggable resolver.
3. **`brokered`** — a gateway holds the credential; no per-dev secret exists.

The shared-vs-personal distinction falls out of mode 2. A shared secret is one
vault item the whole team reads; a personal secret is each dev's own item at the
same logical path.

**Non-goal:** the tool never stores, encrypts, or syncs secret values —
references only.

## 6. CLI tooling — substrate, install-owning

CLI tooling produces no config artifact of its own; it is the underlying tools
that channels 1–5 rest on. **The tool installs CLI tools directly** through
package-manager adapters (`brew`, `apt`, `npm -g`, `uv`, `cargo`,
direct-download), all behind a common contract (`is-installed?`, `install`,
`installed-version`). The manifest declares a tool *abstractly* — just a name
and a version constraint — and each platform adapter satisfies that declaration
for its own OS. For tools no adapter can install, the tool falls back to
declare-and-check and reports an actionable error.

**Reproducibility caveat:** Skills, Plugins, MCP packages, and CLAUDE.md pin to
an exact version plus a content hash — strong reproducibility. CLI tooling pins
the declared constraint and records the resolved installed version, but
`brew` and `apt` do not always allow exact pinning. So CLI reproducibility is
**best-effort**: `check` flags a mismatch, but byte-identical binaries are not
guaranteed.

## 7. The dependency graph — the connective layer

MCP servers are solo-first: they declare *what they connect to* but never *what
must be true for that connection to work*. Infrastructure (VPN, SSH tunnels) is
configured separately, in its own layer. Neither side owns the dependency
between them. That undeclared cross-layer dependency is exactly what the tool
exists to make machine-readable.

Every channel entry can declare `requires: [...]`. There are three node types:

- **CLI tool** — installable (§6).
- **Background service** — a long-running process (for example, an SSH tunnel)
  that a channel needs running for its whole lifetime. The tool *declares,
  checks, and generates the service definition* — the start/stop scripts and
  Claude Code `hooks` wiring. It does **not** supervise, restart, or own the
  daemon lifecycle.
- **Precondition** — something the tool can only check, never set up itself (for
  example, VPN reachability or an SSH key being present). It is declared,
  checked, and fails loudly when not met.

## 8. Modularity — templates

The tool must **not** hardcode knowledge of specific MCP servers. It knows
generic capabilities, and an MCP server is *described* as a bundle of them.

Templates are first-class. A template is a reusable shape: an MCP server's shape
is defined once and used to create many instances (an instance is one concrete
use of a template, with its own values). The multi-database case is the
validating stress test — four databases must be four instances of one template,
not four copy-pasted blocks.

The manifest must cleanly separate three things:

- **Template** — the shared shape.
- **Instance** — the per-use differences (host, db name, ssh user).
- **Tool-owned resolved fields** — ports, generated script paths, rendered hook
  blocks. No human declares these; the tool computes them. Port collisions
  become structurally impossible because no human ever types a port.

A template is also the unit of *reuse across teams*. Because a team/org layer
(§2) may itself declare `templates:`, an org can publish a template library once
— say, `mysql-over-ssh-tunnel` or a standard hook shape — and every repo that
`extends:` that layer creates instances from it without copying it. The shared
shape lives in one place; a repo supplies only its instances.

## 9. Non-goals

No gateway runtime. No credential brokering / token holding. No secret storage
(references + resolver only). No Managed Agents. No Govern workflow in v1. No
process supervision of background services. No OS-level configuration beyond
what a normal package install needs.

## 10. Build phases

All five phases are implemented — see the
[README Status section](../README.md#status).

- **Phase 0 — Foundation.** This repo and design.
- **Phase 1 — Manifest schema (`ainfra.yaml`).** See [spec](../spec/manifest-schema.md).
- **Phase 2 — Lockfile schema (`ainfra.lock`).** See [spec](../spec/lockfile-schema.md).
- **Phase 3 — Channel provider interface.** One contract that every channel
  implements: `resolve() → plan() → apply() → check()`. The agent-targeting
  axis (§14) landed within this phase.
- **Phase 4 — Resolution & precedence engine.** Merges the three layers under
  the Option-C rule.
- **Phase 5 — CLI surface.** `init`, `plan`, `apply`, `check`, `lock`.

## 11. The validation gate

Before any implementation code is written, the Phase 1 and 2 schemas are tested
on paper against five scenarios. See [validation.md](validation.md). If the
schemas cannot express all five cleanly, the schema is revised — not the code.

## 12. Resolved decisions

1. **Background-service install boundary.** Resolved as stated in §7: the tool
   declares, checks, and *generates* the service definition, and does **not**
   run or own the daemon.
2. **Naming.** Resolved: the project is named **`ainfra`** — CLI binary,
   module, and the `ainfra.yaml` / `ainfra.lock` artifacts.

## 13. Config-as-code failure modes ainfra designs against

Config-as-code is a well-trodden category, and its tools have failed in
repeatable ways. Each row below names a known failure mode, where it bit earlier
tooling, and the design decision that rules it out here. These are constraints,
not aspirations — they are enforced by code and tests.

| Failure mode | Where it bit earlier | ainfra's defense |
|--------------|----------------------|------------------|
| **Permissive parsing** — a misspelled key is silently dropped, so config that never applied looks applied | Ansible ignored unknown keys; Kubernetes had to retrofit strict decoding (`--validate=strict`, server-side apply) | The loader decodes with `KnownFields(true)`: an unknown key is a hard error with a hint, never a silent drop. |
| **Schema/docs drift** — a hand-maintained schema or doc slowly diverges from what the tool actually accepts | Common wherever reference docs are written by hand | The JSON Schema is *generated by reflection from the Go structs the loader uses* (`ainfra schema`). It cannot drift from the parser. |
| **No lockfile / floating versions** — "works on my machine"; a dependency changes with no diff | npm, Bundler, and Terraform all added lockfiles only after this hurt | `ainfra.lock` records resolved versions and content hashes; package-launched MCP servers must pin an exact version (§4 validation gate). |
| **Surprising merge precedence** — overrides resolve in ways the author cannot predict | Helm value merging and Kustomize patch ordering are routinely cited | Precedence is one explicit table (Option C, §3); singleton-list channels union-merge (combine entries from every layer) with a documented `deny > ask > allow` rule; iteration order is sorted deterministically. |
| **Secrets in committed config** — credentials leak through `values.yaml`, `.tfvars`, `.env` | Endemic across the category | The environment primitive stores *references only* (§5); the lockfile is layered, so personal config never lands in a committed file. |
| **Undetected post-apply drift** — config is applied once, then the environment changes unnoticed | Any apply-once tool without a verify step | `check` recomputes content hashes against the lockfile and reports drift; its exit code is clean for CI use. |
| **Apply without preview** — a change is reconciled before anyone sees its effect | The mistake Terraform's `plan` exists to prevent | `plan` is a required, side-effect-free step before `apply`. |
| **Schema too rigid** — teams cannot express their case, so they fork or copy-paste | Brittle, hardcoded config tools | Templates and layers (§8), plus generic capability toggles (§8 — no hardcoded server knowledge), keep the schema extensible without forking. |

## 14. Target agent — a chooseable axis

ainfra's name and channel vocabulary come from Claude Code, but its engine does
not. Layering, precedence, content hashing, template instantiation, and the
dependency graph are pure mechanism with no agent knowledge. Agent-specific
knowledge lives only at the edges. That makes the target agent a *chooseable
axis* rather than a hardcoded assumption — decided as part of Phase 3, the cheap
window before the channel provider layer was written.

There are two separate concerns:

- **Channel providers** own channel *semantics* — resolving sources, versions,
  and content hashes; merge rules; dependency edges. This is **target-neutral**:
  an MCP server resolves to the same package and hash no matter which agent is
  targeted.
- **Renderers** own agent *I/O* — where an artifact lives on disk, its file
  format, and how to read current state. A renderer is the agent-specific
  component that writes one agent's files; there is one renderer per agent.

A manifest names its target with the scalar (single-value) `agent` field —
`claude-code`, the default, or `codex`. Because it is a scalar, Option C's
`overridable` mechanism does not apply: the highest-authority layer that
declares a non-empty `agent` wins. Not every channel exists for every agent —
Codex has no skills, plugins, hooks, built-in toggles, or slash commands. Any
entry may carry an `agents:` list to scope it to specific agents. An entry in a
channel the resolved agent cannot render is a hard validation error *unless* its
`agents:` list gates it away. The `ainfra.lock` file stays target-neutral — it
pins inputs, and each renderer derives its own agent's artifacts from the same
locked state.

**Current state.** The `agent` field, capability registry, and gating
validation are implemented, and both the Claude Code and Codex provider sets
are built. The full design is
[multi-agent-renderers-design.md](superpowers/specs/2026-05-21-multi-agent-renderers-design.md).
