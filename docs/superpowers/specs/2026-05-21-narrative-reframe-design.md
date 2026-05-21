# Narrative reframe — outcome-first positioning

## Problem

The user-facing docs lead with the *mechanism* — "Terraform-style CLI",
"lockfile", "reconciliation" — instead of the *outcome*. A reader meets
version-locking before they know why they would care. Nobody wants a lockfile;
they want their teammates' AI tooling to behave like theirs.

## Decision

Reframe the opening of every user-facing doc around a three-beat arc:

1. **The ache** — your teammates' AI development setup drifts from yours the
   moment they install it, with no way to see the gap.
2. **What ainfra does** — define the team's AI tooling once as config-as-code;
   every developer reproduces it identically with one command.
3. **The punchline** — locking, demoted. The lockfile makes "in sync"
   *verifiable* rather than wishful, and catches drift. It is the proof, not
   the pitch.

Two constraints:

- **Tool-agnostic prose.** Narrative copy says "AI tooling" / "AI development
  setup", not "Claude Code". Technical channel descriptions (`.mcp.json`,
  `SKILL.md`, etc.) stay accurate and unchanged — the implementation goes
  agnostic later.
- **"Terraform-style" stays** as a supporting descriptor lower in the text,
  never as the headline.

Headline: **Keep your whole dev team's AI tooling in sync.**

## Scope

Opening sections only. No command, schema, table, or structural changes.

| Doc | Change |
|-----|--------|
| `README.md` | Replace title line, "The problem", and "What this is — and is not" with the outcome-first narrative. Everything below untouched. |
| `docs/design.md` | Rewrite §0 "What this is" to lead with team-sync. Keep the market-position paragraph. |
| `docs/quickstart.md` | Rewrite the one-sentence intro to the sync framing. Walkthrough paths unchanged. |

## Out of scope

Actual tool-agnostic implementation; technical channel descriptions; any code,
schema, or CLI behaviour.
