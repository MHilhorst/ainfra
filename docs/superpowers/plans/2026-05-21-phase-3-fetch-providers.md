# Phase 3 — Plan 4: Fetch/Install Providers (skills, plugins)

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development. Steps use `- [ ]`.

**Goal:** Implement the skills and plugins channel providers. Skills fetch a bundle of files from a source and land them in `.claude/skills/<id>/`. Plugins record the desired plugin set in an ainfra-managed file.

**Architecture:** Fetching is isolated behind a `Fetcher` interface so providers stay unit-testable. `LocalFetcher` copies a local-path source from disk; remote (`git+`, `npm:`) sources return a clear actionable error in this increment (documented limitation — remote fetch is a follow-up). `FakeFetcher` serves canned bundles in tests.

**Tech Stack:** Go, `encoding/json`, standard `testing`.

---

### Task 1: The Fetcher interface and implementations

**Files:** Create `internal/provider/fetch/fetch.go`, `fetch_test.go`.

A `Fetcher` turns a source reference into a bundle: a map of relative path -> file content.

- [ ] **Step 1 — failing tests** (package `fetch`): `LocalFetcher.Fetch` of a local directory source returns every file under it keyed by path-relative-to-the-source; `Fetch` of a `git+https://...` or `npm:...` source returns a non-nil error whose message names the unsupported scheme; `FakeFetcher` returns its canned bundle.

- [ ] **Step 2 — run, see fail.**

- [ ] **Step 3 — implement `fetch.go`:**

```go
// Package fetch retrieves channel-entry bundles (skills, plugins) from their
// declared sources.
package fetch

// Bundle is a fetched set of files: relative path -> content.
type Bundle map[string][]byte

// Fetcher retrieves the bundle for a source reference at a pinned version.
type Fetcher interface {
	Fetch(source, version string) (Bundle, error)
}
```

  - `LocalFetcher struct{ Root string }` — `Fetch`: if `source` does not start with a remote scheme (`git+`, `npm:`), resolve it against `Root` (`filepath.Join(Root, source)` when relative), walk the directory with `filepath.WalkDir`, and return every regular file keyed by its path relative to the resolved source dir. A remote-scheme source returns `fmt.Errorf("fetch: remote source %q is not supported in this build; use a local path", source)`.
  - `FakeFetcher struct{ Bundles map[string]Bundle; Err error }` — `Fetch`: if `Err != nil` return it; else return `Bundles[source]` (error if absent).

- [ ] **Step 4 — run, see pass.**
- [ ] **Step 5 — commit** `Add the Fetcher interface for channel bundles`.

---

### Task 2: Wire a Fetcher into Env

**Files:** Modify `internal/provider/env.go`; update `internal/provider/env_test.go` or fakes as needed.

- [ ] **Step 1 — failing test:** assert `provider.Env` has a `Fetch` field of an interface type with a `Fetch(source, version string) (map[string][]byte, error)`-shaped method, and that a zero `Env` is still usable by existing providers.

- [ ] **Step 2 — run, see fail.**

- [ ] **Step 3 — implement.** To avoid an import cycle (`fetch` must not import `provider`; `provider` may import `fetch`), add to `env.go`: `import ".../internal/provider/fetch"` and a field `Fetch fetch.Fetcher` on `Env`. (`fetch.Bundle` is `map[string][]byte`.) Confirm `go build ./...` shows no cycle.

- [ ] **Step 4 — run, see pass.**
- [ ] **Step 5 — commit** `Add a Fetcher to the provider environment`.

---

### Task 3: Skills provider

**Files:** Create `internal/provider/channels/skills.go`, `skills_test.go`.

Skills live in `<root>/.claude/skills/<id>/` — a directory of files ainfra fully owns. `Resource.Payload` keys: `source` (string), `version` (string).

- [ ] **Step 1 — failing tests** (package `channels`): `Channel()` returns `"skills"`; `Observe` returns an id for each subdirectory of `<root>/.claude/skills/` (missing dir → none, no error); `Apply` Create fetches the bundle via `env.Fetch` and writes every bundle file under `.claude/skills/<id>/`; `Apply` Delete removes the skill directory and its files; `Apply` honors `env.DryRun`; `Apply` returns the fetch error if `env.Fetch.Fetch` fails. Use `provider.NewMemFilesystem()` and a `fetch.FakeFetcher` in `env.Fetch`.

- [ ] **Step 2 — run, see fail.**

- [ ] **Step 3 — implement `skills.go`.** Package `channels`, type `Skills struct{}`. `Channel()` = `"skills"`. Skills dir = `filepath.Join(env.Root, ".claude", "skills")`. `Observe`: `env.FS.ReadDir` the skills dir; for each entry that is a directory (the `MemFilesystem`/`OSFilesystem` `ReadDir` returns names — to tell dirs from files, treat any name without a `.` extension, OR add directory awareness; simplest: a skill is present if `<skillsdir>/<name>/` has at least one file — use `env.FS.ReadDir` on the candidate path and treat a successful non-empty read as "present"). For each present skill return a `provider.Resource{ID: name, Channel: "skills"}`. `Apply`: for Create/Update — read `source`/`version` from `Change.Resource.Payload`, call `env.Fetch.Fetch(source, version)`, and for each `path -> content` in the bundle `fsmerge.WriteOwnedFile(env.FS, filepath.Join(skillDir, path), content)`; for Delete — remove every file currently under the skill dir (`env.FS.ReadDir` + `env.FS.Remove`). Honor `env.DryRun`. If `env.Fetch` is nil, return a clear error.

- [ ] **Step 4 — run, see pass.**
- [ ] **Step 5 — commit** `Add the skills channel provider`.

---

### Task 4: Plugins provider

**Files:** Create `internal/provider/channels/plugins.go`, `plugins_test.go`.

In this increment the plugins provider records the desired plugin set in an ainfra-managed file `<root>/.claude/ainfra/plugins.json` (a JSON object of `id -> {source, version}`). Triggering the Claude Code marketplace installer is a documented follow-up.

`Resource.Payload` keys: `source` (string), `version` (string).

- [ ] **Step 1 — failing tests:** `Channel()` returns `"plugins"`; `Observe` returns an id for each key in `plugins.json` (missing file → none, no error); `Apply` Create adds the plugin's `{source,version}` object under its id, preserving foreign ids; `Apply` Delete removes only that id; `env.DryRun` writes nothing.

- [ ] **Step 2 — run, see fail.**

- [ ] **Step 3 — implement `plugins.go`.** Package `channels`, type `Plugins struct{}`. `Channel()` = `"plugins"`. Path = `filepath.Join(env.Root, ".claude", "ainfra", "plugins.json")`. The file's shape is a flat JSON object `{ "<id>": {"source": "...", "version": "..."} }` (no nesting key). Since `fsmerge.MergeJSONKeys` expects a top-level key, EITHER use a top-level key `"plugins"` (file shape `{"plugins": {...}}`) and `MergeJSONKeys(env.FS, path, "plugins", desired, ownedKeys)` — DO THIS, it reuses the tested helper. `Observe` reads the file and returns a resource per key under `plugins`. `Apply` builds `desired` (id -> `{source,version}` map) for create/update and `ownedKeys` for all non-noop changes, then `MergeJSONKeys`. Skip the merge entirely when `ownedKeys` is empty. Add a one-line comment: actually installing the plugin via the Claude Code marketplace is a follow-up; this provider records the declared plugin set. Honor `env.DryRun`.

- [ ] **Step 4 — run, see pass.**
- [ ] **Step 5 — commit** `Add the plugins channel provider`.

---

### Task 5: Verification

- [ ] **Step 1:** `go build ./... && go test ./...` — all green.
- [ ] **Step 2:** `go vet ./internal/provider/...` — clean.
- [ ] **Step 3:** confirm no import cycle: `fetch` imports only stdlib; `provider` imports `fetch`; `channels` imports `provider`, `fsmerge`, `fetch`.
- [ ] **Step 4:** commit any vet fix.

---

## Self-Review

**Spec coverage:** skills provider (spec §3.4 channel 2) — Task 3; plugins provider (channel 3) — Task 4; the `Fetcher` abstraction supports both — Tasks 1-2. Remote-source fetch is a documented limitation (Task 1) — a follow-up adds git/npm fetchers.

**Type consistency:** `Fetcher`/`Bundle`/`LocalFetcher`/`FakeFetcher` in package `fetch`; `Env.Fetch` added in Task 2; `Skills`/`Plugins` in package `channels`. Both providers honor `env.DryRun` and skip empty work, consistent with the Plan 3 providers.

**Out of scope:** cliTools, preconditions, background services (Plan 5); command wiring + manifest rendering + e2e (Plan 6); remote (git/npm) fetching (follow-up).
