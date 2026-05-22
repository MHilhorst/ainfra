# Design — cliTool resolution (sub-project #4)

## Context

Sub-project #1 authored a real `ainfra.yaml` for the tvt-config setup and
tested it. `validate`, `lock`, and `plan` all pass. But a contained
`ainfra apply` (isolated `HOME`) **fails on the first channel**:

```
Error: cliTools: "mysql-client" is not installed and no supported
install method is declared; install it manually
```

`cliTools` is the first channel in apply order (`internal/provider/orchestrator.go`
`channelOrder`), so this failure aborts the entire apply — no `.mcp.json`,
hooks, commands, rules, or plugins are written. cliTool resolution is therefore
the single blocker to a working `apply`.

## Root cause (proven by reading the code)

Two defects in the cliTools channel:

1. **Type-assertion mismatch.** `internal/resolve/render.go` builds the cliTool
   resource payload as `"install": t.Install`, where `manifest.CLITool.Install`
   is typed `map[string]map[string]any`. `internal/provider/channels/clitools.go:49`
   asserts `c.Resource.Payload["install"].(map[string]any)` — the wrong type.
   The assertion fails, `installMap` is nil, the adapter loop never runs, and
   every cliTool falls through to the declare-and-check branch.

2. **Adapters install by id, not by declared package name.** The declare-and-check
   probe runs `env.Runner.Run(id, "--version")`. For `mysql-client` the binary
   is `mysql`, so the probe fails. And even when the adapter loop is reached,
   `pkg.BrewAdapter.Install` runs `brew install <id>` — for `op` (declared as
   `brew: { cask: 1password-cli }`) that means `brew install op`, which is wrong:
   it ignores the declared `formula`/`cask`/`package` and has no `--cask` support.

## Decisions

- **Adapters receive the install spec, not a bare tool name.** The `pkg.Adapter`
  interface changes so `IsInstalled` and `Install` take the method's spec
  (`map[string]any`, e.g. `{formula: mysql-client}` or `{cask: 1password-cli}`
  or `{package: google-analytics-cli, version: "1.1.1"}`). Each adapter derives
  the real package name and flags from the spec.
- **Brew cask support.** `BrewAdapter` uses `--cask` when the spec has a `cask`
  key; otherwise the `formula` key.
- **The declare-and-check fallback uses the manifest `check.command`.** When no
  adapter matches an install method, the probe runs the `check.command` string
  from the payload (e.g. `mysql --version`) instead of `<id> --version`. If no
  `check.command` is declared, it falls back to `<id> --version` (today's
  behaviour).
- **No uninstall, no version-constraint enforcement.** Out of scope — unchanged.
  `versionConstraint` is not enforced at apply (a separate concern).

## Components

### `internal/provider/pkg/pkg.go`

New `Adapter` interface:

```go
type Adapter interface {
    Name() string
    IsInstalled(env provider.Env, spec map[string]any) (bool, error)
    Install(env provider.Env, spec map[string]any) error
}
```

- `BrewAdapter`:
  - package name + cask flag derived from spec: `cask` key -> (`cask` value, cask=true);
    else `formula` key -> (`formula` value, cask=false).
  - `IsInstalled`: `brew list --versions [--cask] <name>`.
  - `Install`: `brew install [--cask] <name>`.
  - If neither `formula` nor `cask` is present, return an error from both methods.
- `NpmAdapter`:
  - package name from spec `package` key.
  - `IsInstalled`: `npm ls -g --depth 0 <package>`.
  - `Install`: `npm install -g <package>[@<version>]` when spec has `version`,
    else `npm install -g <package>`.
  - If `package` is absent, return an error.
- `Select` is unchanged (`brew`, `npm`/`npm-g`).

### `internal/provider/channels/clitools.go`

- Read `installMap` as `map[string]map[string]any` (the real type). Be tolerant:
  if the value is instead `map[string]any` whose entries are `map[string]any`,
  coerce — a small helper keeps this robust to either decode path.
- For the first method whose `pkg.Select` matches, pass that method's spec map
  to `adapter.IsInstalled` / `adapter.Install`.
- Declare-and-check fallback: read the `check` payload (`map[string]any`); if it
  has a non-empty `command` string, split it into binary + args and run that;
  otherwise run `<id> --version`.

### `internal/resolve/render.go`

No change required — it already places `install` and `check` in the payload.
(If a type-coercion helper in `clitools.go` proves awkward, normalising
`install` to `map[string]map[string]any` here is the fallback; prefer not to.)

## Testing

TDD. Existing `pkg_test.go` and `clitools_test.go` use `provider` fakes
(`FakeRunner`). The interface change requires updating those tests.

- `pkg_test.go`: `BrewAdapter` with a `formula` spec issues `brew install <formula>`;
  with a `cask` spec issues `brew install --cask <cask>`; `IsInstalled` mirrors.
  `NpmAdapter` with `package`+`version` issues `npm install -g pkg@version`;
  without `version`, `npm install -g pkg`. Missing-key specs return an error.
- `clitools_test.go`: a cliTool with `install: {brew: {formula: X}}` and an
  uninstalled state triggers `brew install X` (not `brew install <id>`); a
  cliTool with `install: {brew: {cask: Y}}` triggers `brew install --cask Y`;
  a cliTool with no recognised adapter but a `check.command` probes that command;
  an already-installed tool is a no-op.

## Success criteria

- `go test ./...` passes.
- `ainfra apply` against the tvt-config manifest gets **past** the cliTools
  channel (it either installs/verifies each tool or fails with an *accurate*
  message — e.g. a genuinely missing tool with no brew/npm method — never the
  spurious "no supported install method is declared" for a tool that declares
  `brew`).
- A contained `apply` (isolated `HOME`) then proceeds to write `.mcp.json`,
  hooks, commands, rules, plugins, and tools.

## Out of scope

`versionConstraint` enforcement, uninstall, pip/composer/uv adapters (those
tools stay declare-and-check), and the 1Password / plugin-fetch gaps (#2, #3).
