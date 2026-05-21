# ainfra documentation suite — Design

## Goal

Capture the *why* of ainfra in durable, contributor-facing documentation. The
repo already has `docs/design.md` (the decided *what*), `docs/validation.md`
(the validation gate), and schema specs. It has no document that explains the
problem worth solving, the market bet, the reusable decision heuristics, or the
vocabulary. New contributors must reverse-engineer the philosophy from terse
design decisions.

This adds four documents that make the philosophy explicit.

## Scope decision: design.md is left untouched

`docs/design.md` sections 0–11 are explicitly marked stable. The new docs do
**not** move prose out of it or rewrite it. design.md remains the canonical
*decided what*; the new docs are the *why* and *how to think*. They cross-link
into design.md sections for specifics rather than restating them. Overlap is
kept minimal by tone — design.md states terse decisions, the new docs give the
reasoning behind them.

## Deliverables

Four new files plus one README edit:

| File | Role |
|------|------|
| `docs/philosophy.md` | The enduring *why* |
| `docs/principles.md` | Reusable decision heuristics |
| `docs/glossary.md` | Defined vocabulary |
| `CONTRIBUTING.md` | Contributor guide (repo root, GitHub convention) |
| `README.md` | Add a short "Documentation" section linking all docs |

Placement of the three `docs/` files matches the existing
`docs/design.md` / `docs/validation.md` convention. `CONTRIBUTING.md` sits at
the repo root because GitHub surfaces it there.

## Document outlines

### docs/philosophy.md

The enduring *why*. Narrative prose, not a decision list.

- **The problem worth solving** — Claude Code config is scattered across
  separate mechanisms with separate scopes (MCP servers, skills, plugins,
  `CLAUDE.md`, tool permissions, CLI binaries). No single source of truth, no
  lockfile. "Works on my machine" is unverifiable; drift goes unnoticed; a
  server or skill safe yesterday can change silently (rug-pull).
- **The bet** — declarative, cross-channel reconciliation with a lockfile is
  the one unowned cell in the market. That is the product.
- **Why not a gateway** — runtime governance (MCP gateways) is a saturated,
  funded category on the official MCP roadmap. ainfra does not build one.
- **The Terraform mental model** — declarative manifest, `plan` before `apply`,
  a lockfile separating desired from observed state, every channel a provider
  behind a common interface. Why this shape fits Claude Code config.
- **The ownership boundary** — what ainfra owns (declarative reconciliation +
  lockfile) vs. what it consumes as pluggable backends (gateways, secrets
  managers, package managers). It owns no runtime.
- **Why v1 is Distribute + lockfile, Govern deferred** — the lockfile alone
  closes reproducibility *and* rug-pull/drift detection. Vetting/approval/
  rollback is a later product on the same lockfile, not a v1 prerequisite.
- **What ainfra deliberately is not** — narrative treatment of the non-goals
  (no gateway runtime, no credential brokering, no secret storage, no process
  supervision, no Managed Agents).

Cross-links: design.md §0, §2, §9.

### docs/principles.md

Reusable decision heuristics — the rules a contributor applies when an answer
is not already written down. Each principle: a one-line statement, *why* it
holds, and *how it shows up* in the current design.

- **Consume, don't own** — backends are pluggable adapters; ainfra never owns a
  runtime. (Shows up: gateway adapter, secrets resolver, package-manager
  adapters.)
- **Declare and check, don't supervise** — for background services, ainfra
  declares, checks, and generates the service definition; it does not run,
  restart, or own the daemon. (Shows up: §7 background-service node type.)
- **References, not secrets** — the manifest holds pointers (`op://…`, Vault,
  SOPS); ainfra never stores, encrypts, or syncs a secret value. (Shows up: §5
  environment modes.)
- **Layered, not flat** — org/team, repo, and personal layers merge into one
  resolved state; a flat manifest can express neither org policy nor "just
  mine." (Shows up: §2 layered topology, §3 Option C.)
- **Schema before code** — the schema is the product hypothesis; it is
  validated on paper against five scenarios before implementation. (Shows up:
  the validation gate.)
- **Templates over copy-paste** — a shape is defined once and instantiated N
  times; the tool computes resolved fields (ports, paths) so collisions are
  structurally impossible. (Shows up: §8 template/instance/resolved fields.)
- **YAGNI and defer** — ship the smallest thing that closes the core problem;
  defer the rest. (Shows up: Govern deferred to product #2.)
- **Best-effort honesty** — where a guarantee cannot be made, name the limit
  explicitly rather than implying strength. (Shows up: the CLI-tooling
  reproducibility caveat — `check` flags drift but cannot guarantee
  byte-identical binaries.)

Cross-links: design.md §2–§8, §11.

### docs/glossary.md

Alphabetical, one short entry per term. Terms:

background service, brokered mode, channel, direct mode, drift, instance,
layer, lockfile, manifest, Option C, precondition, provider, reconciliation,
rug-pull, substrate, template, tool-owned resolved field, validation gate.

Each entry is 1–3 sentences and, where useful, points to the design.md section
that defines the term in context.

### CONTRIBUTING.md

Contributor guide.

- **Build phases and current state** — the Phase 0–5 sequence and where the
  project is now (mirrors the README status table; links to it rather than
  duplicating the table content).
- **The validation gate** — schema-on-paper before implementation code; how to
  run a scenario against the schema; what "the schema is iterated, not the
  code" means in practice.
- **Evaluating a proposed feature** — a short checklist: Does it own a runtime?
  Does it hit a stated non-goal? Does it belong in a deferred product (Govern)?
  Which principle decides it?
- **Repository layout** — `cmd/`, `internal/`, `spec/`, `examples/`, `docs/`.
- **Build and test** — `go build ./...`, `go test ./...`, `go run ./cmd/ainfra`.
- **Design decisions** — how decisions are recorded (design.md §12 "Resolved
  decisions" style); open items go in that section before they are settled.

Cross-links: design.md §10–§12, validation.md, README status table.

### README.md edit

Add a "Documentation" section near the end listing all docs with one-line
descriptions: design.md, philosophy.md, principles.md, glossary.md,
validation.md, the two schema specs, and CONTRIBUTING.md. No other README
changes.

## Content sourcing

All content is derived from existing repo material — `README.md`,
`docs/design.md`, `docs/validation.md`, `spec/*.md`, and `examples/`. No new
design decisions are introduced. If writing surfaces a genuine gap or
contradiction in the existing design, it is flagged to the user, not silently
resolved.

## Consistency rules

- Terminology matches design.md exactly (channel, layer, template, instance,
  brokered, precondition, substrate, Option C).
- No emoji in any document.
- Section references use the design.md numbering (§0–§12).
- Each new doc opens with a one-line statement of its purpose and audience.

## Non-goals

- Not rewriting or trimming `docs/design.md`.
- Not introducing new design decisions or changing scope.
- Not documenting the Go implementation packages in `internal/` (code is not
  yet stable; that is a later docs effort).
- No generated API/reference docs.

## Success criteria

- A new contributor can read `docs/philosophy.md` and explain, unprompted, why
  ainfra is not a gateway and what it owns vs. consumes.
- `docs/principles.md` gives a contributor a heuristic to decide a question
  that is not explicitly answered in design.md.
- Every term used in design.md without definition has a `docs/glossary.md`
  entry.
- `CONTRIBUTING.md` lets a contributor evaluate whether a proposed feature
  belongs in ainfra without asking.
- The four docs are internally consistent with each other and with design.md;
  README links them all.
