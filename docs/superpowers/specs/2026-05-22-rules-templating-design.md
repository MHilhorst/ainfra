# Design — Per-developer rules templating (sub-project #5)

## Context

`ainfra`'s `rules` channel copies a source context file (`CLAUDE.md`) verbatim.
The tvt-config team `CLAUDE.md` is a *template* carrying `{{FULL_NAME}}`,
`{{EMAIL}}`, `{{GITHUB_USERNAME}}` placeholders that `tvt setup` fills in per
developer via an interactive wizard. Applied through `ainfra` today, those
placeholders land in `~/.claude/CLAUDE.md` literally.

Sub-project #5 gives the `rules` channel variable substitution. The design
goal, refined during brainstorming: a teammate runs **`ainfra apply` and
nothing else** — no `init --personal`, no hand-edited identity, no prompts —
and their `CLAUDE.md` renders with their real identity. ainfra stays
declarative.

## The key idea: maintainer-declared variable sources

Most "personal" template values are not manual facts — they are *derivable*
from the developer's existing environment (`git config user.name`,
`git config user.email`, `gh api user`). So the manifest does not ask every
developer to type values; the **maintainer declares, once in the committed team
manifest, how each variable is sourced**. Every developer's `apply` then
resolves them locally.

## Decisions

- **A general `vars:` block.** New top-level `vars: map[string]Var` — an open
  key→value map, layer-merged (personal overrides team). No fixed schema: the
  variable set is whatever the team's templates use.
- **Each `vars` entry declares its source.** A `Var` is written either as a
  plain string (literal shorthand) or as a mapping with a `from`:
  - `from: value` (or the bare-string shorthand) — a literal.
  - `from: env`, `env: <NAME>` — read environment variable `NAME`.
  - `from: command`, `command: <shell command>` — run the command, use its
    trimmed stdout.
  The maintainer picks per variable. Example:
  ```yaml
  vars:
    FULL_NAME:        { from: command, command: "git config user.name" }
    EMAIL:            { from: command, command: "git config user.email" }
    GITHUB_USERNAME:  { from: command, command: "gh api user --jq .login" }
    TEAM_NAME:        "Trein-Vertraging"        # literal shorthand
  ```
- **`apply` is the single onboarding command.** `command`/`env` vars resolve on
  each developer's machine from their own git/gh config. No `init --personal`
  step, no prompt — `ainfra` runs declared commands, it does not interrogate
  the user.
- **Opt-in per rule.** A `rules:` entry gains `template: bool`. With
  `template: true`, every `{{KEY}}` in the rule's source content is substituted
  from the merged, resolved `vars`. Without it the file is copied verbatim
  (today's behavior) — a rule that legitimately contains `{{...}}` is never
  mangled.
- **Undefined variable is an error.** A templated rule referencing `{{KEY}}`
  with no `vars` entry fails at resolve time with a diagnostic naming the rule
  and the variable.
- **`{{KEY}}` syntax**, no inner whitespace — matches the live tvt template.
- **The personal layer is unaffected.** `ainfra.personal.yaml` stays purely for
  per-developer *secrets*. A var that genuinely cannot be derived can still be
  overridden there as a literal, but that is the exception, not the onboarding
  path.

## Components

### Manifest (`internal/manifest`)
- `Var` type with a custom `UnmarshalYAML`: a scalar node becomes
  `Var{From: "value", Value: <scalar>}`; a mapping node decodes `from`,
  `value`, `env`, `command`.
- `Manifest.Vars map[string]Var \`yaml:"vars"\``.
- `Rule.Template bool \`yaml:"template"\``.
- `Validate`: a `Var` with an unknown `from`, or missing the field its `from`
  requires (`env` for `from: env`, `command` for `from: command`), is an error.

### Resolve (`internal/resolve`)
- `collectVars(layers)` — merges `vars` across team→repo→personal (mirrors
  `collectSecrets`/`collectTemplates`).
- `resolveVars(specs map[string]Var, runner) (map[string]string, error)` —
  evaluates each `Var`: literal as-is, `env` via `os.Getenv`, `command` via the
  injected `CommandRunner` (trimmed stdout). A command that fails is an error.
- `substituteVars(content string, vars map[string]string) (string, error)` —
  replaces `{{KEY}}`; an undefined key is an error. Isolated and unit-testable.
- `render.go`: for a `template: true` rule, resolve+substitute its content;
  place the rendered text in the payload. `RunLock` (`pipeline.go`): a templated
  rule's `ContentHash` is computed over the *rendered* content, so a changed
  variable value changes the hash and `apply` re-renders.
- **Runner threading:** `command` vars require a `CommandRunner`. `RunLock` and
  `RenderResources` each gain a `runner provider.CommandRunner` parameter;
  callers pass `provider.ExecRunner{}` (production) or a fake (tests). This is
  the one cross-cutting signature change.

### Lockfile
- A templated rule's hash depends on resolved `command`/`env` vars, which are
  per-machine — so a templated rule's lock entry routes to the personal lock
  (gitignored). Correct: a per-developer `CLAUDE.md` is personal state.

## Data flow

```
ainfra.yaml          vars: { FULL_NAME: {from: command, command: "git config user.name"}, ... }
                     rules.team-claude-md: { source, template: true }
   -> collectVars(layers) -> resolveVars(..., runner)   # runs git/gh per machine
        from: command -> run command, trim stdout
        from: env     -> os.Getenv
        from: value   -> literal
   -> read rule source -> substituteVars(content, resolved)
        undefined {{KEY}} -> error
   -> payload.content / lock ContentHash = rendered text
   -> rules channel Apply writes the rendered ~/.claude/CLAUDE.md
```

## Error handling

- Undefined `{{KEY}}` in a templated rule → `RenderResources`/`RunLock` returns
  `rule "<id>" references undefined variable "<KEY>"`.
- A `from: command` whose command exits non-zero → error naming the variable
  and the command's stderr.
- A non-templated rule is never scanned for placeholders — verbatim copy.
- A `vars` entry no template uses is harmless.

## Testing

- `Var.UnmarshalYAML`: scalar form, each mapping form, unknown `from`.
- `substituteVars`: basic, repeated placeholder, multiple keys, undefined key
  errors, no-placeholder content unchanged, an unclosed `{{` left alone.
- `resolveVars`: literal, `env` (set/unset), `command` via a fake runner,
  command-failure errors.
- `collectVars`: team-only, personal-overrides-team, missing layers.
- `RenderResources`: a `template: true` rule renders with resolved vars; a
  `template: false` rule keeps `{{...}}` literal; an undefined var errors.

## Out of scope

- Interactive identity capture — ainfra is declarative.
- The `CLAUDE.local.md` personal section — a separate personal-layer `rules:`
  entry; no special handling.
- Conditionals/loops in templates — `{{KEY}}` substitution only.
- `init --personal` var scaffolding — unnecessary now that var sources are
  maintainer-declared and auto-resolving.

## Success criteria

- A `rules` entry with `template: true` renders `{{KEY}}` from resolved `vars`;
  `apply` writes a `CLAUDE.md` with real values, no literal `{{...}}`.
- A teammate runs only `ainfra apply` — no other onboarding step — and gets a
  correctly personalized `CLAUDE.md`.
- An undefined variable, or a failed `command` var, fails `lock` with a clear
  diagnostic.
- The tvt-config team `ainfra.yaml` carries the three identity vars as
  `from: command` and `rules.team-claude-md` as `template: true`.
