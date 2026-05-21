# ainfra CLI — Developer Experience Design

## 0. Goal

Bring ainfra's CLI to the standard set by Terraform's CLI: a guided onboarding
journey, colored diff output, per-command help, and actionable error messages.
Built stdlib-only — no CLI framework, no color library — to keep ainfra's
supply-chain-trust story clean.

This document specifies the UX of every command. The implementation plan that
follows builds the slice that does not depend on the unbuilt channel provider
layer; the provider-dependent commands (`plan`, `apply`, `check`) are fully
specified here so they are built to the same standard when providers land.

## 1. Settled decisions

- **Scope:** the whole CLI UX — onboarding journey *and* command ergonomics —
  designed as one spec.
- **Dependencies:** stdlib-only. The CLI layer is hand-rolled on Go's `flag`
  package; color is ~10 lines of ANSI. No `cobra`, no color library. The only
  third-party dependency in the repo remains `gopkg.in/yaml.v3`.
- **Deliverable:** spec everything; build the unblocked slice now (see §8).

## 2. Two journeys

Terraform's `init` is consumer-side — it initializes a working directory the
user authored. ainfra differs: the manifest ships *inside* the repo a developer
clones, so there is no "initialize the directory" step. That yields two clean
journeys:

- **Consuming a team setup** (the common case): clone repo →
  `ainfra plan` → `ainfra apply` → `ainfra check` (anytime, including CI).
- **Authoring a setup**: `ainfra init` (scaffold `ainfra.yaml`) → edit →
  `ainfra lock` → commit.

`ainfra init` stays author-side. `ainfra init --personal` scaffolds a
developer's own `ainfra.personal.yaml` layer.

## 3. Command surface

| Command | Role | Buildable now? |
|---|---|---|
| `init` | Scaffold `ainfra.yaml` (`--personal`, `--force`) | Yes |
| `validate` | Fast static schema check (reuses `manifest.Validate`) | Yes |
| `lock` | Resolve manifest → write lockfiles | Yes — re-skin output |
| `version` | Print version (`--json`) | Yes |
| `plan` | Dry-run diff vs lockfile + observed state | Spec'd; pending-stub now |
| `apply` | Reconcile, with confirm prompt (`--auto-approve`) | Spec'd; pending-stub now |
| `check` | Verify env matches lockfile; report drift | Spec'd; pending-stub now |

### 3.1 Global flags

- `--chdir <dir>` — run as if started in `<dir>` (mirrors Terraform's `-chdir`;
  removes the need to `cd` into `examples/multi-database` for smoke tests).
- `--no-color` — force color off.
- `--help` / `-h` — overview, or per-command help when after a command.
- `--version` / `-v` — print version.

### 3.2 Exit codes

- `0` — success / no drift / no changes.
- `1` — error (bad manifest, I/O failure, unimplemented).
- `2` — drift detected or changes pending. Lets `check` act as a CI gate and
  supports a future `plan --detailed-exitcode`.

### 3.3 Per-command UX

**`init`** — writes a starter `ainfra.yaml` to the working directory and a
`.gitignore` entry for `ainfra.personal.*`. Refuses to overwrite an existing
file unless `--force`. `--personal` scaffolds `ainfra.personal.yaml` instead.
Ends with: `Next: edit ainfra.yaml, then run 'ainfra lock'.`

**`validate`** — loads the manifest layers and runs `manifest.Validate`. No
lockfile, no resolution — a fast schema-only check. Reports either
`Configuration is valid.` or one or more diagnostic-error blocks (§5). Exit `0`
or `1`.

**`lock`** — unchanged behavior; output re-skinned to the language in §4.
Reports resolved entry counts per channel and the files written. Ends with:
`Next: run 'ainfra plan' to preview changes.`

**`version`** — prints `ainfra <version>`. `--json` prints
`{"version":"<version>"}`.

**`plan`** — resolves the manifest, compares the resolved state against the
lockfile and the observed environment, and renders the `+`/`~`/`-` diff grouped
by channel (§4). Prints a summary line `Plan: N to add, N to change, N to
remove.` No changes → `Your environment matches the manifest. Nothing to do.`
Read-only. *Implemented now as a pending-stub* (real `--help`, a message
describing intended behavior and that the provider layer is pending, exit `1`).

**`apply`** — runs the `plan` computation, shows the diff, then prompts
`Do you want to apply these changes? Only 'yes' will be accepted: `. Any answer
other than `yes` aborts with no changes. `--auto-approve` skips the prompt.
Streams per-entry progress as each channel reconciles. *Pending-stub now.*

**`check`** — verifies the observed environment matches the lockfile and reports
drift; read-only. Exit `2` on drift so it gates CI. *Pending-stub now.*

## 4. Output language

ANSI color, auto-disabled when stdout is not a TTY, when `NO_COLOR` is set in the
environment, or when `--no-color` is passed.

- `+` green — entry will be added
- `~` yellow — entry will change
- `-` red — entry will be removed
- Section headers: bold. Secondary detail (versions, ports, source template):
  dim.

Every successful command ends with a contextual `Next:` hint, centralized in
`internal/ui` for consistency.

### 4.1 Mockups

```
$ ainfra plan

ainfra will make the following changes:

  MCP servers
  + analytics-db        from template "tunneled-db"  port 13306
  ~ billing-db          version 1.2.0 -> 1.3.0
  CLI tools
  + ssh                 >=8.0  (brew)

Plan: 2 to add, 1 to change, 0 to remove.

Next: run 'ainfra apply' to make these changes.
```

```
$ ainfra lock
ainfra: resolved 4 MCP servers, 4 background services, 1 CLI tool
        wrote ainfra.lock and ainfra.personal.lock

Next: run 'ainfra plan' to preview changes.
```

## 5. Error formatting

Errors render as a diagnostic block, not a flat string:

```
Error: package-launched server must pin an exact version

  on ainfra.yaml, mcpServers.analytics
  This server launches via npx but declares no version.
  Add one, e.g.  version: "1.2.3"
```

A diagnostic carries `{summary, file, path, detail, hint}`. `internal/ui`
renders any error: a plain `error` prints as just the summary line; a
diagnostic prints the full block. `manifest`'s validation errors are upgraded
to produce diagnostics (file = which layer, path = the dotted manifest key,
hint = the fix). Other layers can adopt the type progressively.

## 6. Help system

- `ainfra` with no args, or `ainfra --help` — overview: one-line tagline, the
  command table with summaries, and `Run "ainfra <command> --help" for detail.`
- `ainfra <command> --help` or `ainfra help <command>` — per-command help:
  summary, usage line, flag list, and a short example.
- Unknown command — error plus a `did you mean "<closest>"?` suggestion using a
  small Levenshtein distance over the registered command names.

## 7. Architecture

```
cmd/ainfra/
  main.go        thin: build the registry, apply --chdir, dispatch
  commands.go    the seven command definitions
internal/cli/
  command.go     Command struct, Registry, dispatch, per-command flag.FlagSet
  help.go        overview + per-command help rendering, did-you-mean
internal/ui/
  color.go       TTY / NO_COLOR / --no-color detection; color funcs
  render.go      section headers, +/~/- diff lines, plan summary, Next hints
  confirm.go     yes/no prompt for apply
  diag.go        Diagnostic type and rendering
internal/manifest/
  validate.go    validation errors upgraded to Diagnostic (existing file)
```

Each unit has one responsibility: `cli` knows commands, `ui` knows the terminal,
`cmd/ainfra` only wires them together. The channel/resolve/lockfile logic is
untouched. Render functions take an `io.Writer` and an explicit color flag, so
they are table-testable with color disabled.

A `Command` is a struct: `{Name, Summary, UsageLine, Example string;
SetFlags func(*flag.FlagSet); Run func(ctx) int}`. The `Registry` owns dispatch,
per-command `flag.FlagSet` construction, `--help` interception, and
unknown-command suggestion.

## 8. Implementation slice

**Built by the implementation plan now:**

1. `internal/ui` — color detection, diff/header/summary/hint rendering, confirm
   prompt, `Diagnostic` type and rendering.
2. `internal/cli` — `Command`, `Registry`, dispatch, per-command flags, help,
   did-you-mean.
3. `cmd/ainfra` — rewritten onto the registry; `--chdir` honored.
4. `ainfra init` — scaffold `ainfra.yaml` / `ainfra.personal.yaml`; `--force`.
5. `ainfra validate` — static check rendered through `ui`.
6. `ainfra lock` — output re-skinned to §4.
7. `ainfra version` — `--json`.
8. `plan` / `apply` / `check` — polished pending-stubs: real `--help`, a clear
   message of intended behavior, exit `1`.
9. `manifest` validation errors upgraded to `Diagnostic`.
10. `docs/quickstart.md` and a README usage rewrite, both worked through
    `examples/multi-database`.

**Deferred to the channel-provider follow-up plan:** the real `plan` diff
against observed state, real `apply` reconciliation, real `check` drift
detection. Their UX is specified above so they are built to this standard then.

## 9. Testing

- `internal/ui` render functions: table tests with color disabled, asserting
  exact rendered strings.
- `internal/cli` dispatch: table tests mapping `args` → exit code and output
  substrings, including `--help`, unknown command + suggestion, and `--chdir`.
- `ainfra init`: temp-dir tests asserting the file is written, that a second
  run without `--force` fails, and that `--force` overwrites.
- `ainfra validate`: a valid manifest and an invalid one (asserting the
  diagnostic block content).
- Existing `RunLock` and resolve/manifest/lockfile tests stay green. The
  re-skinned `lock` output is covered via the `ui` render-function tests, not a
  brittle full-string assertion.

## 10. Non-goals

- No `terraform fmt` equivalent (manifest formatting) — deferred, YAGNI for now.
- No interactive `init` wizard — `init` writes a static starter file.
- No shell-completion generation — would be the strongest argument for a
  framework later; out of scope here.
- No change to the channel, resolve, graph, or lockfile logic beyond rendering.
