# ainfra CLI Developer Experience Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Bring ainfra's CLI to Terraform's standard — guided onboarding, colored diff output, per-command help, actionable errors — built stdlib-only.

**Architecture:** Two new internal packages. `internal/ui` owns all terminal rendering (color, diff lines, headers, hints, confirm prompt, error rendering). `internal/cli` is a hand-rolled command framework (registry, per-command `flag.FlagSet`, dispatch, help, did-you-mean) on Go's stdlib `flag` package. A new `internal/diag` package holds the structured `Diagnostic` error type, produced by `manifest` and rendered by `ui`. `cmd/ainfra` is rewritten onto the registry. The channel/resolve/graph/lockfile logic is untouched.

**Tech Stack:** Go 1.25, stdlib only (`flag`, `io`, `os`, `strings`, `bufio`, `crypto`-free). The only third-party dependency in the repo stays `gopkg.in/yaml.v3`. `go test` for tests.

**Spec:** `docs/superpowers/specs/2026-05-21-ainfra-cli-developer-experience-design.md`.

**Refinement of the spec:** Spec §7 placed the `Diagnostic` type in `internal/ui/diag.go`. This plan instead puts the *type* in its own `internal/diag` package so the domain package `manifest` does not have to import a presentation package; `ui` still owns the *rendering*. `cmd/ainfra` splits its command definitions across `commands.go` and per-command files (`cmd_init.go`, `cmd_validate.go`, `cmd_lock.go`) rather than one `commands.go`, so files that change together live together.

---

## File Structure

| File | Responsibility |
|------|----------------|
| `internal/diag/diag.go` | The `Diagnostic` structured-error type. Produced by `manifest`, rendered by `ui`. |
| `internal/ui/color.go` | TTY / `NO_COLOR` / `--no-color` detection; the `Colorizer` and its color funcs. |
| `internal/ui/diag.go` | `RenderError` — renders a `*diag.Diagnostic` as a block, any other error as one line. |
| `internal/ui/render.go` | `Section`, `DiffLine`, `PlanSummary`, `Next` — the plan-diff and guidance primitives. |
| `internal/ui/confirm.go` | `Confirm` — the yes/no prompt for `apply`. |
| `internal/cli/command.go` | `Context`, `Command`, `Registry`, dispatch, per-command flag parsing, `--chdir`. |
| `internal/cli/help.go` | Overview + per-command help rendering; `did you mean?` suggestion. |
| `internal/manifest/validate.go` | `Validate` rewritten to return diagnostics; new `ValidateAll` across layers. |
| `cmd/ainfra/main.go` | Thin: build the registry, register commands, dispatch. |
| `cmd/ainfra/commands.go` | `version` command + `plan`/`apply`/`check` pending-stubs. |
| `cmd/ainfra/cmd_lock.go` | The `lock` command (replaces the old `lock.go`). |
| `cmd/ainfra/cmd_init.go` | The `init` command. |
| `cmd/ainfra/cmd_validate.go` | The `validate` command. |
| `docs/quickstart.md` | The Terraform-style "Get Started" guide. |
| `README.md` | Usage section rewritten around the real journey. |

---

## Task 1: The `ui` color layer

**Files:**
- Create: `internal/ui/color.go`
- Test: `internal/ui/color_test.go`

- [ ] **Step 1: Write the failing test**

```go
package ui

import (
	"bytes"
	"testing"
)

func TestColorizerDisabledByDefaultForNonTerminal(t *testing.T) {
	// A bytes.Buffer is not a terminal, so color must be off.
	c := NewColorizer(&bytes.Buffer{}, false)
	if got := c.Green("+"); got != "+" {
		t.Errorf("disabled Green(%q) = %q, want %q", "+", got, "+")
	}
}

func TestColorizerForceOff(t *testing.T) {
	c := NewColorizer(&bytes.Buffer{}, true)
	if got := c.Red("x"); got != "x" {
		t.Errorf("force-off Red = %q, want %q", got, "x")
	}
}

func TestColorizerEnabledWraps(t *testing.T) {
	c := Colorizer{enabled: true}
	got := c.Green("+")
	want := "\033[32m+\033[0m"
	if got != want {
		t.Errorf("enabled Green(+) = %q, want %q", got, want)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/`
Expected: FAIL — `undefined: NewColorizer`.

- [ ] **Step 3: Write the implementation**

```go
// Package ui owns every byte ainfra writes to a terminal: color decisions,
// the plan-diff primitives, the confirm prompt, and error rendering. Nothing
// outside this package emits ANSI codes.
package ui

import (
	"io"
	"os"
)

// ANSI escape codes. Kept here so this is the only file that knows them.
const (
	ansiReset  = "\033[0m"
	ansiBold   = "\033[1m"
	ansiDim    = "\033[2m"
	ansiRed    = "\033[31m"
	ansiGreen  = "\033[32m"
	ansiYellow = "\033[33m"
)

// Colorizer wraps strings in ANSI codes, or not, depending on enabled.
// Its zero value is a safe no-color Colorizer.
type Colorizer struct {
	enabled bool
}

// NewColorizer decides whether color is on for w. Color is enabled only when
// forceOff is false, NO_COLOR is unset, and w is a character device (a TTY).
func NewColorizer(w io.Writer, forceOff bool) Colorizer {
	if forceOff {
		return Colorizer{}
	}
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		return Colorizer{}
	}
	return Colorizer{enabled: isTerminal(w)}
}

// isTerminal reports whether w is a character device. Non-*os.File writers
// (buffers, pipes) are never terminals.
func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

func (c Colorizer) wrap(code, s string) string {
	if !c.enabled {
		return s
	}
	return code + s + ansiReset
}

// Bold, Dim, Red, Green, Yellow return s wrapped in the named style when color
// is enabled, and s unchanged when it is not.
func (c Colorizer) Bold(s string) string   { return c.wrap(ansiBold, s) }
func (c Colorizer) Dim(s string) string    { return c.wrap(ansiDim, s) }
func (c Colorizer) Red(s string) string    { return c.wrap(ansiRed, s) }
func (c Colorizer) Green(s string) string  { return c.wrap(ansiGreen, s) }
func (c Colorizer) Yellow(s string) string { return c.wrap(ansiYellow, s) }
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/ui/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/color.go internal/ui/color_test.go
git commit -m "Add ui color layer with TTY and NO_COLOR detection"
```

---

## Task 2: The `diag` structured-error type

**Files:**
- Create: `internal/diag/diag.go`
- Test: `internal/diag/diag_test.go`

- [ ] **Step 1: Write the failing test**

```go
package diag

import "testing"

func TestDiagnosticErrorReturnsSummary(t *testing.T) {
	d := &Diagnostic{Summary: "package must pin a version"}
	if d.Error() != "package must pin a version" {
		t.Errorf("Error() = %q, want the summary", d.Error())
	}
}

func TestDiagnosticSatisfiesErrorInterface(t *testing.T) {
	var err error = &Diagnostic{Summary: "x"}
	if err.Error() != "x" {
		t.Errorf("Diagnostic does not flow as an error: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/diag/`
Expected: FAIL — `undefined: Diagnostic`.

- [ ] **Step 3: Write the implementation**

```go
// Package diag defines a structured, renderable error: a human summary plus
// the location and fix-it hint a developer needs. Domain packages such as
// manifest produce a Diagnostic; internal/ui renders it as a block.
package diag

// Diagnostic is a structured error. It implements the error interface, so it
// flows through ordinary error returns; ui.RenderError gives it the full
// block treatment (location, detail, hint). Every field except Summary is
// optional.
type Diagnostic struct {
	Summary string // one-line description of what is wrong (required)
	File    string // file the problem is in, e.g. "ainfra.yaml"
	Path    string // dotted location within the file, e.g. "mcpServers.x"
	Detail  string // a sentence or two of explanation
	Hint    string // a concrete suggested fix
}

// Error returns the summary, so a *Diagnostic satisfies the error interface.
func (d *Diagnostic) Error() string { return d.Summary }
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/diag/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/diag/diag.go internal/diag/diag_test.go
git commit -m "Add diag package with the structured Diagnostic error type"
```

---

## Task 3: Error rendering in `ui`

**Files:**
- Create: `internal/ui/diag.go`
- Test: `internal/ui/diag_test.go`

- [ ] **Step 1: Write the failing test**

```go
package ui

import (
	"bytes"
	"errors"
	"testing"

	"github.com/MHilhorst/ainfra/internal/diag"
)

func TestRenderErrorPlainErrorIsOneLine(t *testing.T) {
	var b bytes.Buffer
	RenderError(&b, Colorizer{}, errors.New("boom"))
	if got, want := b.String(), "Error: boom\n"; got != want {
		t.Errorf("RenderError plain = %q, want %q", got, want)
	}
}

func TestRenderErrorDiagnosticIsABlock(t *testing.T) {
	var b bytes.Buffer
	RenderError(&b, Colorizer{}, &diag.Diagnostic{
		Summary: "package-launched server must pin an exact version",
		File:    "ainfra.yaml",
		Path:    "mcpServers.analytics",
		Detail:  "This server launches via npx but declares no version.",
		Hint:    `Add one, e.g.  version: "1.2.3"`,
	})
	want := "Error: package-launched server must pin an exact version\n" +
		"\n" +
		"  on ainfra.yaml, mcpServers.analytics\n" +
		"  This server launches via npx but declares no version.\n" +
		`  Add one, e.g.  version: "1.2.3"` + "\n"
	if got := b.String(); got != want {
		t.Errorf("RenderError diagnostic =\n%q\nwant\n%q", got, want)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run TestRenderError`
Expected: FAIL — `undefined: RenderError`.

- [ ] **Step 3: Write the implementation**

```go
package ui

import (
	"fmt"
	"io"

	"github.com/MHilhorst/ainfra/internal/diag"
)

// RenderError writes err to w. A *diag.Diagnostic prints as a block — summary,
// then a blank line, then location, detail, and hint. Any other error prints
// as a single "Error: <message>" line.
func RenderError(w io.Writer, c Colorizer, err error) {
	d, ok := err.(*diag.Diagnostic)
	if !ok {
		fmt.Fprintln(w, c.Red("Error:"), err.Error())
		return
	}
	fmt.Fprintln(w, c.Red("Error:"), c.Bold(d.Summary))
	if d.File != "" || d.Path != "" || d.Detail != "" || d.Hint != "" {
		fmt.Fprintln(w)
	}
	if loc := location(d); loc != "" {
		fmt.Fprintln(w, "  on "+loc)
	}
	if d.Detail != "" {
		fmt.Fprintln(w, "  "+d.Detail)
	}
	if d.Hint != "" {
		fmt.Fprintln(w, "  "+c.Dim(d.Hint))
	}
}

// location joins a diagnostic's File and Path into one "file, path" string,
// omitting whichever is absent.
func location(d *diag.Diagnostic) string {
	switch {
	case d.File != "" && d.Path != "":
		return d.File + ", " + d.Path
	case d.File != "":
		return d.File
	default:
		return d.Path
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/ui/ -run TestRenderError`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/diag.go internal/ui/diag_test.go
git commit -m "Render diagnostics as blocks and plain errors as one line"
```

---

## Task 4: The plan-diff render primitives

**Files:**
- Create: `internal/ui/render.go`
- Test: `internal/ui/render_test.go`

- [ ] **Step 1: Write the failing test**

```go
package ui

import (
	"bytes"
	"testing"
)

func TestSectionWritesIndentedHeader(t *testing.T) {
	var b bytes.Buffer
	Section(&b, Colorizer{}, "MCP servers")
	if got, want := b.String(), "  MCP servers\n"; got != want {
		t.Errorf("Section = %q, want %q", got, want)
	}
}

func TestDiffLineAddWithDetail(t *testing.T) {
	var b bytes.Buffer
	DiffLine(&b, Colorizer{}, OpAdd, "analytics-db", "port 13306")
	want := "  + analytics-db        port 13306\n"
	if got := b.String(); got != want {
		t.Errorf("DiffLine = %q, want %q", got, want)
	}
}

func TestDiffLineRemoveWithoutDetail(t *testing.T) {
	var b bytes.Buffer
	DiffLine(&b, Colorizer{}, OpRemove, "old-srv", "")
	if got, want := b.String(), "  - old-srv\n"; got != want {
		t.Errorf("DiffLine = %q, want %q", got, want)
	}
}

func TestPlanSummary(t *testing.T) {
	var b bytes.Buffer
	PlanSummary(&b, 2, 1, 0)
	want := "Plan: 2 to add, 1 to change, 0 to remove.\n"
	if got := b.String(); got != want {
		t.Errorf("PlanSummary = %q, want %q", got, want)
	}
}

func TestNextWritesBlankLineThenHint(t *testing.T) {
	var b bytes.Buffer
	Next(&b, Colorizer{}, "run 'ainfra apply' to make these changes.")
	want := "\nNext: run 'ainfra apply' to make these changes.\n"
	if got := b.String(); got != want {
		t.Errorf("Next = %q, want %q", got, want)
	}
}
```

Note: in `TestDiffLineAddWithDetail` the expected string has `analytics-db` (12 chars) padded to 20, i.e. 8 trailing spaces, then a single space, then `port 13306`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run 'TestSection|TestDiffLine|TestPlanSummary|TestNext'`
Expected: FAIL — `undefined: Section`.

- [ ] **Step 3: Write the implementation**

```go
package ui

import (
	"fmt"
	"io"
	"strings"
)

// Diff operation symbols passed to DiffLine.
const (
	OpAdd    = '+'
	OpChange = '~'
	OpRemove = '-'
)

// nameColumn is the column width entry names are padded to in a diff line, so
// the dim secondary detail lines up across rows.
const nameColumn = 20

// Section writes a bold, two-space-indented channel header.
func Section(w io.Writer, c Colorizer, title string) {
	fmt.Fprintln(w, "  "+c.Bold(title))
}

// DiffLine writes one change row: a colored op symbol (+ add, ~ change,
// - remove), the entry name padded to nameColumn, and dim secondary detail.
// An empty detail prints just the symbol and name.
func DiffLine(w io.Writer, c Colorizer, op byte, name, detail string) {
	sym := string(op)
	switch op {
	case OpAdd:
		sym = c.Green("+")
	case OpChange:
		sym = c.Yellow("~")
	case OpRemove:
		sym = c.Red("-")
	}
	if detail == "" {
		fmt.Fprintf(w, "  %s %s\n", sym, name)
		return
	}
	pad := ""
	if n := nameColumn - len(name); n > 0 {
		pad = strings.Repeat(" ", n)
	}
	fmt.Fprintf(w, "  %s %s%s %s\n", sym, name, pad, c.Dim(detail))
}

// PlanSummary writes the "Plan: N to add, N to change, N to remove." line.
func PlanSummary(w io.Writer, add, change, remove int) {
	fmt.Fprintf(w, "Plan: %d to add, %d to change, %d to remove.\n", add, change, remove)
}

// Next writes a blank line, then a bold "Next:" prefix and a guidance string.
// Every command ends its successful output with one of these.
func Next(w io.Writer, c Colorizer, text string) {
	fmt.Fprintln(w)
	fmt.Fprintln(w, c.Bold("Next:"), text)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/ui/ -run 'TestSection|TestDiffLine|TestPlanSummary|TestNext'`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/render.go internal/ui/render_test.go
git commit -m "Add plan-diff render primitives: sections, diff lines, hints"
```

---

## Task 5: The confirm prompt

**Files:**
- Create: `internal/ui/confirm.go`
- Test: `internal/ui/confirm_test.go`

- [ ] **Step 1: Write the failing test**

```go
package ui

import (
	"bytes"
	"strings"
	"testing"
)

func TestConfirmAcceptsExactlyYes(t *testing.T) {
	var out bytes.Buffer
	ok, err := Confirm(strings.NewReader("yes\n"), &out, "Apply? ")
	if err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	if !ok {
		t.Error("Confirm(yes) = false, want true")
	}
	if !strings.Contains(out.String(), "Apply? ") {
		t.Errorf("prompt not written: %q", out.String())
	}
}

func TestConfirmRejectsAnythingElse(t *testing.T) {
	for _, in := range []string{"y\n", "no\n", "YES\n", "\n", ""} {
		ok, err := Confirm(strings.NewReader(in), &bytes.Buffer{}, "Apply? ")
		if err != nil {
			t.Fatalf("Confirm(%q): %v", in, err)
		}
		if ok {
			t.Errorf("Confirm(%q) = true, want false", in)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run TestConfirm`
Expected: FAIL — `undefined: Confirm`.

- [ ] **Step 3: Write the implementation**

```go
package ui

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// Confirm writes prompt to w, reads one line from r, and returns true only if
// the trimmed line is exactly "yes". EOF (no line) counts as a decline. This
// is the gate apply uses before reconciling.
func Confirm(r io.Reader, w io.Writer, prompt string) (bool, error) {
	fmt.Fprint(w, prompt)
	sc := bufio.NewScanner(r)
	if !sc.Scan() {
		return false, sc.Err()
	}
	return strings.TrimSpace(sc.Text()) == "yes", nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/ui/ -run TestConfirm`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/confirm.go internal/ui/confirm_test.go
git commit -m "Add the apply confirm prompt"
```

---

## Task 6: The `cli` command registry and help

**Files:**
- Create: `internal/cli/command.go`
- Create: `internal/cli/help.go`
- Test: `internal/cli/command_test.go`
- Test: `internal/cli/help_test.go`

This task builds the command types, the registry, and the help rendering — everything except `Dispatch`, which is Task 7. The package compiles after this task; the help methods are exercised by tests until `Dispatch` calls them.

- [ ] **Step 1: Write the failing test for the registry**

Create `internal/cli/command_test.go`:

```go
package cli

import (
	"bytes"
	"testing"
)

func newTestCommand(name string) *Command {
	return &Command{
		Name:      name,
		Summary:   "summary of " + name,
		UsageLine: "ainfra " + name,
		Run:       func(ctx Context) int { return 0 },
	}
}

func TestRegistryAddAndLookup(t *testing.T) {
	r := NewRegistry(&bytes.Buffer{}, &bytes.Buffer{}, "0.0.0-test")
	r.Add(newTestCommand("lock"))
	if r.lookup("lock") == nil {
		t.Error("lookup(lock) = nil after Add")
	}
	if r.lookup("absent") != nil {
		t.Error("lookup(absent) should be nil")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/`
Expected: FAIL — `undefined: NewRegistry`.

- [ ] **Step 3: Write `internal/cli/command.go` (types and registry only)**

```go
// Package cli is ainfra's hand-rolled command framework: a registry of
// commands, per-command flag parsing, dispatch, help, and a did-you-mean
// suggestion. It depends only on the standard library and internal/ui.
package cli

import (
	"flag"
	"io"
)

// Context is what a command's Run receives.
type Context struct {
	Args    []string  // positional args left after the command's flags
	Stdout  io.Writer // where normal output goes
	Stderr  io.Writer // where errors go
	NoColor bool      // resolved --no-color (from either flag position)
	Dir     string    // working directory, with --chdir applied
}

// Command is one ainfra subcommand.
type Command struct {
	Name      string                // the word typed after "ainfra"
	Summary   string                // one line, shown in the overview
	UsageLine string                // e.g. "ainfra init [--personal] [--force]"
	Example   string                // optional, shown in per-command help
	SetFlags  func(fs *flag.FlagSet) // registers command-specific flags (optional)
	Run       func(ctx Context) int  // returns the process exit code
}

// Registry holds the registered commands and dispatches to them.
type Registry struct {
	commands []*Command
	stdout   io.Writer
	stderr   io.Writer
	version  string
}

// NewRegistry returns a Registry writing to the given streams.
func NewRegistry(stdout, stderr io.Writer, version string) *Registry {
	return &Registry{stdout: stdout, stderr: stderr, version: version}
}

// Add registers a command. Registration order is the order shown in the
// overview.
func (r *Registry) Add(c *Command) { r.commands = append(r.commands, c) }

// lookup returns the command with the given name, or nil.
func (r *Registry) lookup(name string) *Command {
	for _, c := range r.commands {
		if c.Name == name {
			return c
		}
	}
	return nil
}
```

- [ ] **Step 4: Run the registry test to verify it passes**

Run: `go test ./internal/cli/ -run TestRegistry`
Expected: PASS.

- [ ] **Step 5: Write the failing test for help**

Create `internal/cli/help_test.go`:

```go
package cli

import (
	"bytes"
	"flag"
	"strings"
	"testing"
)

func TestPrintOverviewListsCommands(t *testing.T) {
	var out bytes.Buffer
	r := NewRegistry(&out, &bytes.Buffer{}, "0.0.0-test")
	r.Add(newTestCommand("init"))
	r.Add(newTestCommand("lock"))
	r.printOverview()
	s := out.String()
	for _, want := range []string{"Usage:", "init", "lock", "summary of init", "--chdir"} {
		if !strings.Contains(s, want) {
			t.Errorf("overview missing %q\n---\n%s", want, s)
		}
	}
}

func TestPrintCommandHelpShowsFlags(t *testing.T) {
	var out bytes.Buffer
	r := NewRegistry(&out, &bytes.Buffer{}, "0.0.0-test")
	cmd := &Command{
		Name: "init", Summary: "scaffold a manifest",
		UsageLine: "ainfra init [--force]",
		Example:   "ainfra init",
		SetFlags:  func(fs *flag.FlagSet) { fs.Bool("force", false, "overwrite an existing file") },
	}
	r.printCommandHelp(cmd)
	s := out.String()
	for _, want := range []string{"ainfra init [--force]", "--force", "overwrite an existing file", "Example:"} {
		if !strings.Contains(s, want) {
			t.Errorf("command help missing %q\n---\n%s", want, s)
		}
	}
}

func TestPrintUnknownSuggestsClosest(t *testing.T) {
	var errOut bytes.Buffer
	r := NewRegistry(&bytes.Buffer{}, &errOut, "0.0.0-test")
	r.Add(newTestCommand("lock"))
	r.printUnknown(r.lookup, "lok")
	s := errOut.String()
	if !strings.Contains(s, `unknown command "lok"`) {
		t.Errorf("missing unknown-command line: %q", s)
	}
	if !strings.Contains(s, `Did you mean "lock"?`) {
		t.Errorf("missing suggestion: %q", s)
	}
}

func TestLevenshtein(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"lock", "lock", 0},
		{"lok", "lock", 1},
		{"", "abc", 3},
		{"plan", "apply", 4},
	}
	for _, c := range cases {
		if got := levenshtein(c.a, c.b); got != c.want {
			t.Errorf("levenshtein(%q,%q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}
```

Note: `printUnknown`'s first parameter is unused in the test signature above only to keep the call simple — see Step 6 for its real signature; replace the test's `r.printUnknown(r.lookup, "lok")` call once Step 6 fixes the signature. **Correction:** to avoid that, Step 6 defines `printUnknown(name string)` with no extra parameter; update the test call to `r.printUnknown("lok")` before running.

- [ ] **Step 6: Write `internal/cli/help.go`**

```go
package cli

import (
	"flag"
	"fmt"
	"io"

	"github.com/MHilhorst/ainfra/internal/ui"
)

// printOverview writes the no-command overview: tagline, usage, the command
// table, and the global flags.
func (r *Registry) printOverview() {
	c := ui.NewColorizer(r.stdout, false)
	fmt.Fprintln(r.stdout, "ainfra — config-as-code for Claude Code team environments")
	fmt.Fprintln(r.stdout)
	fmt.Fprintln(r.stdout, c.Bold("Usage:"))
	fmt.Fprintln(r.stdout, "  ainfra <command> [flags]")
	fmt.Fprintln(r.stdout)
	fmt.Fprintln(r.stdout, c.Bold("Commands:"))
	for _, cmd := range r.commands {
		fmt.Fprintf(r.stdout, "  %-10s %s\n", cmd.Name, cmd.Summary)
	}
	fmt.Fprintln(r.stdout)
	fmt.Fprintln(r.stdout, c.Bold("Global flags:"))
	fmt.Fprintln(r.stdout, "  --chdir <dir>   Run as if started in <dir>")
	fmt.Fprintln(r.stdout, "  --no-color      Disable colored output")
	fmt.Fprintln(r.stdout)
	fmt.Fprintln(r.stdout, `Run "ainfra <command> --help" for command-specific detail.`)
}

// printCommandHelp writes per-command help: summary, usage, flags, example.
func (r *Registry) printCommandHelp(cmd *Command) {
	c := ui.NewColorizer(r.stdout, false)
	fmt.Fprintf(r.stdout, "ainfra %s — %s\n", cmd.Name, cmd.Summary)
	fmt.Fprintln(r.stdout)
	fmt.Fprintln(r.stdout, c.Bold("Usage:"))
	fmt.Fprintln(r.stdout, "  "+cmd.UsageLine)
	if cmd.SetFlags != nil {
		fs := flag.NewFlagSet(cmd.Name, flag.ContinueOnError)
		cmd.SetFlags(fs)
		shown := false
		fs.VisitAll(func(f *flag.Flag) {
			if !shown {
				fmt.Fprintln(r.stdout)
				fmt.Fprintln(r.stdout, c.Bold("Flags:"))
				shown = true
			}
			fmt.Fprintf(r.stdout, "  --%-12s %s\n", f.Name, f.Usage)
		})
	}
	if cmd.Example != "" {
		fmt.Fprintln(r.stdout)
		fmt.Fprintln(r.stdout, c.Bold("Example:"))
		fmt.Fprintln(r.stdout, "  "+cmd.Example)
	}
}

// printUnknown writes an unknown-command error to stderr, with a did-you-mean
// suggestion when a registered command is within edit distance 2.
func (r *Registry) printUnknown(name string) {
	c := ui.NewColorizer(r.stderr, false)
	fmt.Fprintf(r.stderr, "%s unknown command %q\n", c.Red("ainfra:"), name)
	if s := r.closest(name); s != "" {
		fmt.Fprintln(r.stderr)
		fmt.Fprintf(r.stderr, "Did you mean %q?\n", s)
	}
	fmt.Fprintln(r.stderr)
	fmt.Fprintln(r.stderr, `Run "ainfra --help" to see all commands.`)
}

// closest returns the registered command name nearest to name by edit
// distance, or "" if none is within distance 2.
func (r *Registry) closest(name string) string {
	best, bestDist := "", 3
	for _, cmd := range r.commands {
		if d := levenshtein(name, cmd.Name); d < bestDist {
			best, bestDist = cmd.Name, d
		}
	}
	return best
}

// levenshtein returns the edit distance between a and b.
func levenshtein(a, b string) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		curr := make([]int, len(b)+1)
		curr[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min3(prev[j]+1, curr[j-1]+1, prev[j-1]+cost)
		}
		prev = curr
	}
	return prev[len(b)]
}

func min3(a, b, c int) int {
	m := a
	if b < m {
		m = b
	}
	if c < m {
		m = c
	}
	return m
}

// runHelp implements the `ainfra help [command]` form. Defined here; wired by
// Dispatch in command.go.
func (r *Registry) runHelp(args []string) int {
	if len(args) == 0 {
		r.printOverview()
		return 0
	}
	cmd := r.lookup(args[0])
	if cmd == nil {
		r.printUnknown(args[0])
		return 2
	}
	r.printCommandHelp(cmd)
	return 0
}

var _ io.Writer // keeps the io import if a future edit drops its only use
```

Remove the trailing `var _ io.Writer` line and the `io` import if `go vet` flags `io` as unused — it is only present so the import block is stable; if the build complains, delete both.

- [ ] **Step 7: Update the help test call**

In `help_test.go`, change `r.printUnknown(r.lookup, "lok")` to `r.printUnknown("lok")`.

- [ ] **Step 8: Run all `cli` tests to verify they pass**

Run: `go test ./internal/cli/`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/cli/command.go internal/cli/help.go internal/cli/command_test.go internal/cli/help_test.go
git commit -m "Add cli command registry and help rendering"
```

---

## Task 7: `cli` dispatch and global flags

**Files:**
- Modify: `internal/cli/command.go` (add `Dispatch` and `resolveDir`)
- Test: `internal/cli/dispatch_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/cli/dispatch_test.go`:

```go
package cli

import (
	"bytes"
	"flag"
	"strings"
	"testing"
)

func dispatchRegistry(out, errOut *bytes.Buffer) *Registry {
	r := NewRegistry(out, errOut, "0.0.0-test")
	r.Add(&Command{
		Name: "lock", Summary: "resolve and lock", UsageLine: "ainfra lock",
		Run: func(ctx Context) int {
			ctx.Stdout.Write([]byte("locked in " + ctx.Dir + "\n"))
			return 0
		},
	})
	echo := &Command{Name: "echo", Summary: "echo a flag", UsageLine: "ainfra echo"}
	var loud bool
	echo.SetFlags = func(fs *flag.FlagSet) { fs.BoolVar(&loud, "loud", false, "shout") }
	echo.Run = func(ctx Context) int {
		if loud {
			ctx.Stdout.Write([]byte("LOUD\n"))
		} else {
			ctx.Stdout.Write([]byte("quiet\n"))
		}
		return 0
	}
	r.Add(echo)
	return r
}

func TestDispatchRunsCommand(t *testing.T) {
	var out, errOut bytes.Buffer
	code := dispatchRegistry(&out, &errOut).Dispatch([]string{"lock"})
	if code != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%q", code, errOut.String())
	}
	if !strings.Contains(out.String(), "locked in") {
		t.Errorf("lock did not run: %q", out.String())
	}
}

func TestDispatchNoArgsPrintsOverview(t *testing.T) {
	var out bytes.Buffer
	code := dispatchRegistry(&out, &bytes.Buffer{}).Dispatch(nil)
	if code != 0 || !strings.Contains(out.String(), "Usage:") {
		t.Errorf("no-args dispatch: code=%d out=%q", code, out.String())
	}
}

func TestDispatchUnknownCommandExits2(t *testing.T) {
	var errOut bytes.Buffer
	code := dispatchRegistry(&bytes.Buffer{}, &errOut).Dispatch([]string{"lok"})
	if code != 2 {
		t.Errorf("exit = %d, want 2", code)
	}
	if !strings.Contains(errOut.String(), `Did you mean "lock"?`) {
		t.Errorf("no suggestion: %q", errOut.String())
	}
}

func TestDispatchVersionFlag(t *testing.T) {
	var out bytes.Buffer
	code := dispatchRegistry(&out, &bytes.Buffer{}).Dispatch([]string{"--version"})
	if code != 0 || !strings.Contains(out.String(), "ainfra 0.0.0-test") {
		t.Errorf("--version: code=%d out=%q", code, out.String())
	}
}

func TestDispatchHelpForCommand(t *testing.T) {
	var out bytes.Buffer
	code := dispatchRegistry(&out, &bytes.Buffer{}).Dispatch([]string{"lock", "--help"})
	if code != 0 || !strings.Contains(out.String(), "ainfra lock") {
		t.Errorf("lock --help: code=%d out=%q", code, out.String())
	}
}

func TestDispatchPerCommandFlag(t *testing.T) {
	var out bytes.Buffer
	code := dispatchRegistry(&out, &bytes.Buffer{}).Dispatch([]string{"echo", "--loud"})
	if code != 0 || !strings.Contains(out.String(), "LOUD") {
		t.Errorf("echo --loud: code=%d out=%q", code, out.String())
	}
}

func TestDispatchChdirIsPassedToContext(t *testing.T) {
	var out bytes.Buffer
	code := dispatchRegistry(&out, &bytes.Buffer{}).Dispatch([]string{"--chdir", "/tmp/x", "lock"})
	if code != 0 || !strings.Contains(out.String(), "locked in /tmp/x") {
		t.Errorf("--chdir: code=%d out=%q", code, out.String())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run TestDispatch`
Expected: FAIL — `r.Dispatch undefined`.

- [ ] **Step 3: Add `Dispatch` and `resolveDir` to `internal/cli/command.go`**

Add these imports to the existing `import` block in `command.go` (it currently imports `flag` and `io`):

```go
import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/MHilhorst/ainfra/internal/ui"
)
```

Append to `command.go`:

```go
// Dispatch parses args (the process args after the program name), selects and
// runs a command, and returns the process exit code.
//
// Global flags (--chdir, --no-color) may appear before the command name;
// --no-color is also accepted after it. --help/-h and --version/-v are
// recognized as leading shortcuts.
func (r *Registry) Dispatch(args []string) int {
	// Leading --help/--version shortcuts, before any command name.
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			break
		}
		switch a {
		case "-h", "--help":
			r.printOverview()
			return 0
		case "-v", "--version":
			fmt.Fprintf(r.stdout, "ainfra %s\n", r.version)
			return 0
		}
	}

	// Global flags that precede the command name.
	global := flag.NewFlagSet("ainfra", flag.ContinueOnError)
	global.SetOutput(io.Discard)
	noColor := global.Bool("no-color", false, "disable colored output")
	chdir := global.String("chdir", "", "run as if started in this directory")
	if err := global.Parse(args); err != nil {
		ui.RenderError(r.stderr, ui.NewColorizer(r.stderr, false), err)
		return 1
	}
	rest := global.Args()
	if len(rest) == 0 {
		r.printOverview()
		return 0
	}

	cmdName, cmdArgs := rest[0], rest[1:]
	if cmdName == "help" {
		return r.runHelp(cmdArgs)
	}

	cmd := r.lookup(cmdName)
	if cmd == nil {
		r.printUnknown(cmdName)
		return 2
	}

	// Per-command flag set. --help and --no-color are accepted on every
	// command; --no-color here merges with the global one.
	fs := flag.NewFlagSet(cmd.Name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	if cmd.SetFlags != nil {
		cmd.SetFlags(fs)
	}
	helpWanted := fs.Bool("help", false, "show help for this command")
	localNoColor := fs.Bool("no-color", false, "disable colored output")
	if err := fs.Parse(cmdArgs); err != nil {
		cz := ui.NewColorizer(r.stderr, *noColor)
		ui.RenderError(r.stderr, cz, fmt.Errorf("%s: %v", cmd.Name, err))
		return 1
	}
	if *helpWanted {
		r.printCommandHelp(cmd)
		return 0
	}

	dir := *chdir
	if dir == "" {
		wd, err := os.Getwd()
		if err != nil {
			ui.RenderError(r.stderr, ui.NewColorizer(r.stderr, *noColor || *localNoColor), err)
			return 1
		}
		dir = wd
	}

	return cmd.Run(Context{
		Args:    fs.Args(),
		Stdout:  r.stdout,
		Stderr:  r.stderr,
		NoColor: *noColor || *localNoColor,
		Dir:     dir,
	})
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cli/`
Expected: PASS (all `cli` tests).

- [ ] **Step 5: Commit**

```bash
git add internal/cli/command.go internal/cli/dispatch_test.go
git commit -m "Add cli dispatch with global flags and per-command parsing"
```

---

## Task 8: Manifest validation produces diagnostics

**Files:**
- Modify: `internal/manifest/validate.go`
- Modify: `internal/manifest/validate_test.go`

`Validate` is rewritten to return `*diag.Diagnostic` values. A new `ValidateAll` validates every layer with a cross-layer template map, setting each diagnostic's `File`. Existing `RunLock` is untouched (it keeps its own inline validation; that is out of scope per the spec).

- [ ] **Step 1: Write the failing test**

Replace the entire contents of `internal/manifest/validate_test.go` with:

```go
package manifest

import (
	"strings"
	"testing"

	"github.com/MHilhorst/ainfra/internal/diag"
)

func asDiagnostic(t *testing.T, err error) *diag.Diagnostic {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	d, ok := err.(*diag.Diagnostic)
	if !ok {
		t.Fatalf("error is %T, want *diag.Diagnostic: %v", err, err)
	}
	return d
}

func TestValidateRejectsFloatingMCPVersion(t *testing.T) {
	m := &Manifest{Version: 1, MCPServers: map[string]MCPServer{
		"s": {Command: "npx", Args: []string{"-y", "pkg@latest"}},
	}}
	d := asDiagnostic(t, Validate(m))
	if !strings.Contains(d.Summary, "pin an exact version") {
		t.Errorf("summary = %q", d.Summary)
	}
	if d.Path != "mcpServers.s" {
		t.Errorf("path = %q, want mcpServers.s", d.Path)
	}
	if d.Hint == "" {
		t.Error("expected a hint")
	}
}

func TestValidateAcceptsPinnedMCPVersion(t *testing.T) {
	m := &Manifest{Version: 1, MCPServers: map[string]MCPServer{
		"s": {Command: "npx", Args: []string{"-y", "pkg"}, Version: "1.2.3"},
	}}
	if err := Validate(m); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateRejectsUnknownTemplate(t *testing.T) {
	m := &Manifest{Version: 1, MCPServers: map[string]MCPServer{
		"s": {Template: "missing"},
	}}
	d := asDiagnostic(t, Validate(m))
	if !strings.Contains(d.Summary, "unknown template") {
		t.Errorf("summary = %q", d.Summary)
	}
}

func TestValidateRejectsUnknownHookEvent(t *testing.T) {
	m := &Manifest{Version: 1, Hooks: map[string]Hook{
		"h": {Event: "OnEverything", Command: "echo x"},
	}}
	d := asDiagnostic(t, Validate(m))
	if !strings.Contains(d.Summary, "event") {
		t.Errorf("summary = %q", d.Summary)
	}
}

func TestValidateRejectsHookWithoutCommand(t *testing.T) {
	m := &Manifest{Version: 1, Hooks: map[string]Hook{
		"h": {Event: "SessionStart"},
	}}
	d := asDiagnostic(t, Validate(m))
	if !strings.Contains(d.Summary, "command") {
		t.Errorf("summary = %q", d.Summary)
	}
}

func TestValidateRejectsCommandWithoutSource(t *testing.T) {
	m := &Manifest{Version: 1, Commands: map[string]Command{
		"c": {Description: "no source"},
	}}
	d := asDiagnostic(t, Validate(m))
	if !strings.Contains(d.Summary, "source") {
		t.Errorf("summary = %q", d.Summary)
	}
}

func TestValidateAcceptsValidHooksAndCommands(t *testing.T) {
	m := &Manifest{Version: 1,
		Hooks: map[string]Hook{
			"h": {Event: "PreToolUse", Matcher: "Bash", Command: "echo guard"},
		},
		Commands: map[string]Command{
			"c": {Source: "./commands/c.md", Description: "a command"},
		},
	}
	if err := Validate(m); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateAllSetsFileFromLayer(t *testing.T) {
	layers := map[Layer]*Manifest{
		LayerRepo: {Version: 1},
		LayerPersonal: {Version: 1, MCPServers: map[string]MCPServer{
			"bad": {Command: "npx"},
		}},
	}
	d := asDiagnostic(t, ValidateAll(layers))
	if d.File != "ainfra.personal.yaml" {
		t.Errorf("file = %q, want ainfra.personal.yaml", d.File)
	}
}

func TestValidateAllResolvesCrossLayerTemplate(t *testing.T) {
	// The personal layer uses a template defined only in the repo layer.
	layers := map[Layer]*Manifest{
		LayerRepo: {Version: 1, Templates: map[string]Template{"t": {}}},
		LayerPersonal: {Version: 1, MCPServers: map[string]MCPServer{
			"mine": {Template: "t"},
		}},
	}
	if err := ValidateAll(layers); err != nil {
		t.Fatalf("cross-layer template should validate: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/manifest/ -run TestValidate`
Expected: FAIL — `undefined: diag` / `undefined: ValidateAll`.

- [ ] **Step 3: Rewrite `internal/manifest/validate.go`**

Replace the entire file with:

```go
package manifest

import (
	"fmt"
	"maps"
	"slices"

	"github.com/MHilhorst/ainfra/internal/diag"
)

// packageLaunchers are commands that launch a server from a package registry;
// such servers must pin an exact version (spec §5.1).
var packageLaunchers = map[string]bool{"npx": true, "uvx": true, "pipx": true}

// hookEvents are the Claude Code lifecycle events a hook may bind to (spec §11).
var hookEvents = map[string]bool{
	"SessionStart": true, "SessionEnd": true, "UserPromptSubmit": true,
	"PreToolUse": true, "PostToolUse": true, "Notification": true,
	"Stop": true, "SubagentStop": true, "PreCompact": true,
}

// Validate runs static checks on a single manifest layer. It returns the first
// problem found as a *diag.Diagnostic; entries are checked in sorted-key order
// so that first problem is deterministic. The diagnostic's File is left empty
// — ValidateAll fills it from the layer.
func Validate(m *Manifest) error {
	for _, id := range slices.Sorted(maps.Keys(m.MCPServers)) {
		srv := m.MCPServers[id]
		if srv.Template != "" {
			if _, ok := m.Templates[srv.Template]; !ok {
				return &diag.Diagnostic{
					Summary: fmt.Sprintf("unknown template %q", srv.Template),
					Path:    "mcpServers." + id,
					Detail:  fmt.Sprintf("Server %q references template %q, which is not defined.", id, srv.Template),
					Hint:    "Define it under templates:, or correct the name.",
				}
			}
			continue
		}
		if packageLaunchers[srv.Command] && srv.Version == "" {
			return &diag.Diagnostic{
				Summary: "package-launched server must pin an exact version",
				Path:    "mcpServers." + id,
				Detail:  fmt.Sprintf("Server %q launches via %s but declares no version.", id, srv.Command),
				Hint:    `Add a version field, e.g.  version: "1.2.3"`,
			}
		}
	}
	for _, id := range slices.Sorted(maps.Keys(m.Templates)) {
		tmpl := m.Templates[id]
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
	}
	for _, id := range slices.Sorted(maps.Keys(m.Hooks)) {
		h := m.Hooks[id]
		if !hookEvents[h.Event] {
			return &diag.Diagnostic{
				Summary: fmt.Sprintf("unknown or missing hook event %q", h.Event),
				Path:    "hooks." + id,
				Detail:  "A hook must bind to a Claude Code lifecycle event.",
				Hint:    "Valid events: SessionStart, SessionEnd, UserPromptSubmit, PreToolUse, PostToolUse, Notification, Stop, SubagentStop, PreCompact.",
			}
		}
		if h.Command == "" {
			return &diag.Diagnostic{
				Summary: "hook declares no command",
				Path:    "hooks." + id,
				Detail:  fmt.Sprintf("Hook %q binds to %s but has nothing to run.", id, h.Event),
				Hint:    "Add a command field.",
			}
		}
	}
	for _, id := range slices.Sorted(maps.Keys(m.Commands)) {
		if m.Commands[id].Source == "" {
			return &diag.Diagnostic{
				Summary: "command declares no source",
				Path:    "commands." + id,
				Detail:  fmt.Sprintf("Command %q has no source file.", id),
				Hint:    "Add a source field pointing at the command's .md file.",
			}
		}
	}
	return nil
}

// ValidateAll validates every present layer. It builds a cross-layer template
// map first, so a lower layer may reference a template defined in a higher
// one, then tags each diagnostic with the offending layer's file name.
func ValidateAll(layers map[Layer]*Manifest) error {
	order := []Layer{LayerTeam, LayerRepo, LayerPersonal}
	allTemplates := map[string]Template{}
	for _, ln := range order {
		if m, ok := layers[ln]; ok {
			for name, tmpl := range m.Templates {
				if _, exists := allTemplates[name]; !exists {
					allTemplates[name] = tmpl
				}
			}
		}
	}
	fileFor := map[Layer]string{
		LayerRepo:     "ainfra.yaml",
		LayerPersonal: "ainfra.personal.yaml",
		LayerTeam:     "(team layer)",
	}
	for _, ln := range order {
		m, ok := layers[ln]
		if !ok {
			continue
		}
		toValidate := m
		if len(m.Templates) < len(allTemplates) {
			copied := *m
			copied.Templates = allTemplates
			toValidate = &copied
		}
		if err := Validate(toValidate); err != nil {
			if d, ok := err.(*diag.Diagnostic); ok && d.File == "" {
				d.File = fileFor[ln]
			}
			return err
		}
	}
	return nil
}
```

- [ ] **Step 4: Run the manifest tests to verify they pass**

Run: `go test ./internal/manifest/`
Expected: PASS (all manifest tests, including the untouched load/types tests).

- [ ] **Step 5: Run the full suite to confirm `RunLock` still passes**

Run: `go test ./...`
Expected: PASS. `internal/resolve` still passes — `RunLock` calls `Validate`, which now returns a `*diag.Diagnostic`; since `RunLock` only checks `err != nil` and returns it, behavior is unchanged.

- [ ] **Step 6: Commit**

```bash
git add internal/manifest/validate.go internal/manifest/validate_test.go
git commit -m "Make manifest validation produce structured diagnostics"
```

---

## Task 9: Rewrite the CLI onto the registry

**Files:**
- Modify: `cmd/ainfra/main.go` (full rewrite)
- Create: `cmd/ainfra/commands.go`
- Create: `cmd/ainfra/cmd_lock.go`
- Delete: `cmd/ainfra/lock.go`
- Test: `cmd/ainfra/main_test.go`

- [ ] **Step 1: Write the failing test**

Create `cmd/ainfra/main_test.go`:

```go
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunVersion(t *testing.T) {
	var out bytes.Buffer
	code := run([]string{"version"}, &out, &bytes.Buffer{})
	if code != 0 || !strings.Contains(out.String(), "ainfra ") {
		t.Errorf("version: code=%d out=%q", code, out.String())
	}
}

func TestRunNoArgsShowsOverview(t *testing.T) {
	var out bytes.Buffer
	code := run(nil, &out, &bytes.Buffer{})
	if code != 0 || !strings.Contains(out.String(), "Commands:") {
		t.Errorf("overview: code=%d out=%q", code, out.String())
	}
}

func TestRunUnknownCommand(t *testing.T) {
	var errOut bytes.Buffer
	code := run([]string{"bogus"}, &bytes.Buffer{}, &errOut)
	if code != 2 || !strings.Contains(errOut.String(), "unknown command") {
		t.Errorf("unknown: code=%d err=%q", code, errOut.String())
	}
}

func TestRunPlanIsPendingStub(t *testing.T) {
	var errOut bytes.Buffer
	code := run([]string{"plan"}, &bytes.Buffer{}, &errOut)
	if code != 1 || !strings.Contains(errOut.String(), "not available yet") {
		t.Errorf("plan stub: code=%d err=%q", code, errOut.String())
	}
}

func TestRunLockOnMinimalManifest(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte("version: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	code := run([]string{"--chdir", dir, "lock"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("lock: code=%d err=%q", code, errOut.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "ainfra.lock")); err != nil {
		t.Errorf("ainfra.lock not written: %v", err)
	}
	if !strings.Contains(out.String(), "Next:") {
		t.Errorf("lock output missing Next hint: %q", out.String())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/ainfra/`
Expected: FAIL — `undefined: run` (the current `main.go` has `run` but a different signature; this will be a compile error against the new test).

- [ ] **Step 3: Delete the old lock file**

Run: `git rm cmd/ainfra/lock.go`
Expected: `cmd/ainfra/lock.go` removed.

- [ ] **Step 4: Rewrite `cmd/ainfra/main.go`**

Replace the entire file with:

```go
// Command ainfra is the config-as-code CLI for Claude Code team environments.
package main

import (
	"io"
	"os"

	"github.com/MHilhorst/ainfra/internal/cli"
	"github.com/MHilhorst/ainfra/internal/version"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run builds the command registry and dispatches args. It is separate from
// main so tests can drive it with their own streams.
func run(args []string, stdout, stderr io.Writer) int {
	reg := cli.NewRegistry(stdout, stderr, version.Version)
	reg.Add(newLockCommand())
	reg.Add(newPlanCommand())
	reg.Add(newApplyCommand())
	reg.Add(newCheckCommand())
	reg.Add(newVersionCommand())
	return reg.Dispatch(args)
}
```

- [ ] **Step 5: Create `cmd/ainfra/commands.go`**

```go
package main

import (
	"flag"
	"fmt"

	"github.com/MHilhorst/ainfra/internal/cli"
	"github.com/MHilhorst/ainfra/internal/ui"
	"github.com/MHilhorst/ainfra/internal/version"
)

// newVersionCommand prints the build version, optionally as JSON.
func newVersionCommand() *cli.Command {
	var asJSON bool
	return &cli.Command{
		Name:      "version",
		Summary:   "Print the ainfra version",
		UsageLine: "ainfra version [--json]",
		Example:   "ainfra version --json",
		SetFlags:  func(fs *flag.FlagSet) { fs.BoolVar(&asJSON, "json", false, "print as JSON") },
		Run: func(ctx cli.Context) int {
			if asJSON {
				fmt.Fprintf(ctx.Stdout, "{\"version\":%q}\n", version.Version)
			} else {
				fmt.Fprintf(ctx.Stdout, "ainfra %s\n", version.Version)
			}
			return 0
		},
	}
}

// newPendingCommand builds a command whose real behavior depends on the
// channel provider layer (the next build phase). It prints a clear message
// and exits 1, but still gets real --help via the registry.
func newPendingCommand(name, summary, describes string) *cli.Command {
	return &cli.Command{
		Name:      name,
		Summary:   summary,
		UsageLine: "ainfra " + name,
		Run: func(ctx cli.Context) int {
			c := ui.NewColorizer(ctx.Stderr, ctx.NoColor)
			fmt.Fprintln(ctx.Stderr, c.Bold("ainfra "+name), "is not available yet.")
			fmt.Fprintln(ctx.Stderr)
			fmt.Fprintln(ctx.Stderr, "  "+describes)
			fmt.Fprintln(ctx.Stderr, "  "+c.Dim("It depends on the channel provider layer — the next build phase."))
			return 1
		},
	}
}

func newPlanCommand() *cli.Command {
	return newPendingCommand("plan",
		"Show the diff between desired and observed state",
		"plan will resolve the manifest and show the +/~/- changes ainfra would make.")
}

func newApplyCommand() *cli.Command {
	return newPendingCommand("apply",
		"Reconcile the environment to match the manifest",
		"apply will show the plan, ask for confirmation, then reconcile each channel.")
}

func newCheckCommand() *cli.Command {
	return newPendingCommand("check",
		"Verify the environment matches the lockfile; report drift",
		"check will compare the observed environment against ainfra.lock and report drift.")
}
```

- [ ] **Step 6: Create `cmd/ainfra/cmd_lock.go`**

```go
package main

import (
	"fmt"
	"path/filepath"

	"github.com/MHilhorst/ainfra/internal/cli"
	"github.com/MHilhorst/ainfra/internal/lockfile"
	"github.com/MHilhorst/ainfra/internal/resolve"
	"github.com/MHilhorst/ainfra/internal/ui"
)

// newLockCommand resolves the manifest and writes ainfra.lock and
// ainfra.personal.lock.
func newLockCommand() *cli.Command {
	return &cli.Command{
		Name:      "lock",
		Summary:   "Resolve the manifest and write ainfra.lock",
		UsageLine: "ainfra lock",
		Example:   "ainfra lock",
		Run:       runLock,
	}
}

func runLock(ctx cli.Context) int {
	if err := resolve.RunLock(ctx.Dir); err != nil {
		ui.RenderError(ctx.Stderr, ui.NewColorizer(ctx.Stderr, ctx.NoColor), err)
		return 1
	}
	c := ui.NewColorizer(ctx.Stdout, ctx.NoColor)
	committed, _ := lockfile.Read(filepath.Join(ctx.Dir, "ainfra.lock"))
	personal, _ := lockfile.Read(filepath.Join(ctx.Dir, "ainfra.personal.lock"))
	fmt.Fprintln(ctx.Stdout, "ainfra: resolved "+lockSummary(committed, personal))
	fmt.Fprintln(ctx.Stdout, "        wrote ainfra.lock and ainfra.personal.lock")
	ui.Next(ctx.Stdout, c, "run 'ainfra plan' to preview changes.")
	return 0
}

// lockSummary describes the entry counts across both lock files, listing only
// the channels that have entries.
func lockSummary(committed, personal *lockfile.Lock) string {
	count := func(pick func(*lockfile.Lock) map[string]lockfile.Entry) int {
		return len(pick(committed)) + len(pick(personal))
	}
	type channel struct {
		label string
		n     int
	}
	channels := []channel{
		{"MCP servers", count(func(l *lockfile.Lock) map[string]lockfile.Entry { return l.Entries.MCPServers })},
		{"background services", count(func(l *lockfile.Lock) map[string]lockfile.Entry { return l.Entries.BackgroundServices })},
		{"hooks", count(func(l *lockfile.Lock) map[string]lockfile.Entry { return l.Entries.Hooks })},
		{"commands", count(func(l *lockfile.Lock) map[string]lockfile.Entry { return l.Entries.Commands })},
		{"CLI tools", count(func(l *lockfile.Lock) map[string]lockfile.Entry { return l.Entries.CLITools })},
	}
	parts := []string{}
	for _, ch := range channels {
		if ch.n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", ch.n, ch.label))
		}
	}
	if len(parts) == 0 {
		return "an empty manifest (no entries)"
	}
	out := parts[0]
	for _, p := range parts[1:] {
		out += ", " + p
	}
	return out
}
```

- [ ] **Step 7: Run the build and the cmd tests**

Run: `go build ./... && go test ./cmd/ainfra/`
Expected: build succeeds; all `cmd/ainfra` tests PASS.

- [ ] **Step 8: Commit**

```bash
git add cmd/ainfra/main.go cmd/ainfra/commands.go cmd/ainfra/cmd_lock.go cmd/ainfra/main_test.go
git commit -m "Rewrite the CLI onto the command registry"
```

---

## Task 10: The `init` command

**Files:**
- Create: `cmd/ainfra/cmd_init.go`
- Modify: `cmd/ainfra/main.go` (register the command)
- Test: `cmd/ainfra/cmd_init_test.go`

- [ ] **Step 1: Write the failing test**

Create `cmd/ainfra/cmd_init_test.go`:

```go
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitWritesManifestAndGitignore(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer
	code := run([]string{"--chdir", dir, "init"}, &out, &bytes.Buffer{})
	if code != 0 {
		t.Fatalf("init: code=%d", code)
	}
	data, err := os.ReadFile(filepath.Join(dir, "ainfra.yaml"))
	if err != nil {
		t.Fatalf("ainfra.yaml not written: %v", err)
	}
	if !strings.Contains(string(data), "version: 1") {
		t.Errorf("manifest missing version: %q", data)
	}
	gi, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil || !strings.Contains(string(gi), "ainfra.personal.*") {
		t.Errorf(".gitignore missing personal entry: %v / %q", err, gi)
	}
}

func TestInitRefusesToOverwriteWithoutForce(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte("version: 1\n"), 0o644)
	var errOut bytes.Buffer
	code := run([]string{"--chdir", dir, "init"}, &bytes.Buffer{}, &errOut)
	if code != 1 || !strings.Contains(errOut.String(), "already exists") {
		t.Errorf("expected refusal: code=%d err=%q", code, errOut.String())
	}
}

func TestInitForceOverwrites(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte("OLD\n"), 0o644)
	code := run([]string{"--chdir", dir, "init", "--force"}, &bytes.Buffer{}, &bytes.Buffer{})
	if code != 0 {
		t.Fatalf("init --force: code=%d", code)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "ainfra.yaml"))
	if strings.Contains(string(data), "OLD") {
		t.Error("init --force did not overwrite")
	}
}

func TestInitPersonalWritesPersonalLayer(t *testing.T) {
	dir := t.TempDir()
	code := run([]string{"--chdir", dir, "init", "--personal"}, &bytes.Buffer{}, &bytes.Buffer{})
	if code != 0 {
		t.Fatalf("init --personal: code=%d", code)
	}
	if _, err := os.Stat(filepath.Join(dir, "ainfra.personal.yaml")); err != nil {
		t.Errorf("ainfra.personal.yaml not written: %v", err)
	}
}

func TestInitGitignoreIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("ainfra.personal.*\n"), 0o644)
	run([]string{"--chdir", dir, "init"}, &bytes.Buffer{}, &bytes.Buffer{})
	gi, _ := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if strings.Count(string(gi), "ainfra.personal.*") != 1 {
		t.Errorf(".gitignore entry duplicated: %q", gi)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/ainfra/ -run TestInit`
Expected: FAIL — `undefined: newInitCommand` (compile error; `run` does not yet register it).

- [ ] **Step 3: Create `cmd/ainfra/cmd_init.go`**

```go
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/MHilhorst/ainfra/internal/cli"
	"github.com/MHilhorst/ainfra/internal/diag"
	"github.com/MHilhorst/ainfra/internal/ui"
)

// starterManifest is the ainfra.yaml a fresh `ainfra init` writes.
const starterManifest = `version: 1

# ainfra manifest — your team's Claude Code setup as config-as-code.
# Schema: spec/manifest-schema.md   Guide: docs/quickstart.md

# CLI tools the other channels depend on.
cliTools: {}

# MCP servers to land in each developer's .mcp.json.
mcpServers: {}

# Hooks, commands, skills, plugins, and CLAUDE.md rules go here too —
# see spec/manifest-schema.md for the full schema.
`

// starterPersonal is the ainfra.personal.yaml that `ainfra init --personal`
// writes. The personal layer is git-ignored and never affects teammates.
const starterPersonal = `version: 1

# Your personal ainfra layer — overrides and additions just for you.
# This file is git-ignored; it never affects teammates.

mcpServers: {}
`

// newInitCommand scaffolds an ainfra.yaml (or ainfra.personal.yaml).
func newInitCommand() *cli.Command {
	var personal, force bool
	return &cli.Command{
		Name:      "init",
		Summary:   "Scaffold an ainfra.yaml in the current repo",
		UsageLine: "ainfra init [--personal] [--force]",
		Example:   "ainfra init",
		SetFlags: func(fs *flag.FlagSet) {
			fs.BoolVar(&personal, "personal", false, "scaffold ainfra.personal.yaml instead")
			fs.BoolVar(&force, "force", false, "overwrite an existing file")
		},
		Run: func(ctx cli.Context) int { return runInit(ctx, personal, force) },
	}
}

func runInit(ctx cli.Context, personal, force bool) int {
	name, content := "ainfra.yaml", starterManifest
	if personal {
		name, content = "ainfra.personal.yaml", starterPersonal
	}
	path := filepath.Join(ctx.Dir, name)
	errColor := ui.NewColorizer(ctx.Stderr, ctx.NoColor)

	if !force {
		if _, err := os.Stat(path); err == nil {
			ui.RenderError(ctx.Stderr, errColor, &diag.Diagnostic{
				Summary: name + " already exists",
				Hint:    "Pass --force to overwrite it.",
			})
			return 1
		}
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}
	if err := ensureGitignore(ctx.Dir); err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}

	c := ui.NewColorizer(ctx.Stdout, ctx.NoColor)
	fmt.Fprintln(ctx.Stdout, "ainfra: created "+name)
	ui.Next(ctx.Stdout, c, "edit "+name+", then run 'ainfra lock'.")
	return 0
}

// gitignoreEntry is the pattern init keeps in .gitignore so a developer's
// personal layer is never committed.
const gitignoreEntry = "ainfra.personal.*"

// ensureGitignore appends gitignoreEntry to .gitignore (creating the file if
// absent) unless it is already present. It is idempotent.
func ensureGitignore(dir string) error {
	path := filepath.Join(dir, ".gitignore")
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == gitignoreEntry {
			return nil
		}
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	prefix := ""
	if len(data) > 0 && !strings.HasSuffix(string(data), "\n") {
		prefix = "\n"
	}
	_, err = f.WriteString(prefix + gitignoreEntry + "\n")
	return err
}
```

- [ ] **Step 4: Register the command in `cmd/ainfra/main.go`**

In `run`, add the `init` registration as the first `reg.Add` call. Change:

```go
	reg := cli.NewRegistry(stdout, stderr, version.Version)
	reg.Add(newLockCommand())
```

to:

```go
	reg := cli.NewRegistry(stdout, stderr, version.Version)
	reg.Add(newInitCommand())
	reg.Add(newLockCommand())
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./cmd/ainfra/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add cmd/ainfra/cmd_init.go cmd/ainfra/main.go cmd/ainfra/cmd_init_test.go
git commit -m "Add the ainfra init command"
```

---

## Task 11: The `validate` command

**Files:**
- Create: `cmd/ainfra/cmd_validate.go`
- Modify: `cmd/ainfra/main.go` (register the command)
- Test: `cmd/ainfra/cmd_validate_test.go`

- [ ] **Step 1: Write the failing test**

Create `cmd/ainfra/cmd_validate_test.go`:

```go
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateAcceptsAValidManifest(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte("version: 1\n"), 0o644)
	var out bytes.Buffer
	code := run([]string{"--chdir", dir, "validate"}, &out, &bytes.Buffer{})
	if code != 0 || !strings.Contains(out.String(), "valid") {
		t.Errorf("validate valid: code=%d out=%q", code, out.String())
	}
}

func TestValidateReportsADiagnosticBlock(t *testing.T) {
	dir := t.TempDir()
	bad := "version: 1\nmcpServers:\n  s:\n    command: npx\n"
	os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(bad), 0o644)
	var errOut bytes.Buffer
	code := run([]string{"--chdir", dir, "validate"}, &bytes.Buffer{}, &errOut)
	if code != 1 {
		t.Fatalf("validate invalid: code=%d, want 1", code)
	}
	s := errOut.String()
	for _, want := range []string{"Error:", "pin an exact version", "ainfra.yaml, mcpServers.s"} {
		if !strings.Contains(s, want) {
			t.Errorf("diagnostic missing %q\n---\n%s", want, s)
		}
	}
}

func TestValidateReportsMissingManifest(t *testing.T) {
	dir := t.TempDir()
	var errOut bytes.Buffer
	code := run([]string{"--chdir", dir, "validate"}, &bytes.Buffer{}, &errOut)
	if code != 1 || !strings.Contains(errOut.String(), "Error:") {
		t.Errorf("missing manifest: code=%d err=%q", code, errOut.String())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/ainfra/ -run TestValidate`
Expected: FAIL — `undefined: newValidateCommand`.

- [ ] **Step 3: Create `cmd/ainfra/cmd_validate.go`**

```go
package main

import (
	"fmt"

	"github.com/MHilhorst/ainfra/internal/cli"
	"github.com/MHilhorst/ainfra/internal/manifest"
	"github.com/MHilhorst/ainfra/internal/ui"
)

// newValidateCommand runs the manifest's static checks without resolving it
// or writing a lockfile.
func newValidateCommand() *cli.Command {
	return &cli.Command{
		Name:      "validate",
		Summary:   "Check the manifest for errors without resolving it",
		UsageLine: "ainfra validate",
		Example:   "ainfra validate",
		Run:       runValidate,
	}
}

func runValidate(ctx cli.Context) int {
	errColor := ui.NewColorizer(ctx.Stderr, ctx.NoColor)
	layers, err := manifest.LoadLayers(ctx.Dir)
	if err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}
	if err := manifest.ValidateAll(layers); err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}
	c := ui.NewColorizer(ctx.Stdout, ctx.NoColor)
	fmt.Fprintln(ctx.Stdout, c.Green("Configuration is valid."))
	return 0
}
```

- [ ] **Step 4: Register the command in `cmd/ainfra/main.go`**

In `run`, add the `validate` registration immediately after `init`. Change:

```go
	reg.Add(newInitCommand())
	reg.Add(newLockCommand())
```

to:

```go
	reg.Add(newInitCommand())
	reg.Add(newValidateCommand())
	reg.Add(newLockCommand())
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./cmd/ainfra/`
Expected: PASS.

- [ ] **Step 6: Run the full suite and build**

Run: `go build ./... && go test ./...`
Expected: build succeeds; every package PASSes.

- [ ] **Step 7: Commit**

```bash
git add cmd/ainfra/cmd_validate.go cmd/ainfra/main.go cmd/ainfra/cmd_validate_test.go
git commit -m "Add the ainfra validate command"
```

---

## Task 12: Quick-start guide and README

**Files:**
- Create: `docs/quickstart.md`
- Modify: `README.md`

- [ ] **Step 1: Create `docs/quickstart.md`**

```markdown
# ainfra Quick Start

ainfra defines a team's Claude Code setup as config-as-code and reconciles it,
with a lockfile, onto any developer's machine. This guide walks both paths:
joining a team that already uses ainfra, and authoring a setup from scratch.

## Install

```sh
go build -o ainfra ./cmd/ainfra
# move ./ainfra onto your PATH, or run it in place
```

Check it works:

```sh
ainfra version
```

## Consuming a team setup

You cloned a repo that already contains an `ainfra.yaml`. There is no
"initialize" step — the manifest ships in the repo.

```sh
ainfra plan     # preview what would change on your machine
ainfra apply    # reconcile your machine to the manifest
ainfra check    # verify nothing has drifted (safe to run anytime, incl. CI)
```

`plan` is always safe — it changes nothing. `apply` asks for confirmation
before it touches anything. `check` exits non-zero when it finds drift, so it
works as a CI gate.

> `plan`, `apply`, and `check` are specified but not yet built — they depend on
> the channel provider layer, the next build phase. Running them today prints a
> short notice. `lock`, `init`, and `validate` work now.

## Authoring a setup

Starting a new team setup:

```sh
ainfra init        # scaffold an ainfra.yaml
# edit ainfra.yaml — add cliTools, mcpServers, hooks, commands
ainfra validate    # static-check the manifest
ainfra lock        # resolve it and write ainfra.lock
git add ainfra.yaml ainfra.lock && git commit
```

`ainfra.lock` is committed; it pins exact versions and content hashes so every
teammate resolves identically.

### Your personal layer

Anything that is just yours — a personal MCP server, a local override — goes in
a personal layer that is never committed:

```sh
ainfra init --personal   # scaffold ainfra.personal.yaml (git-ignored)
```

## Worked example

`examples/multi-database/` is a complete manifest: four databases reached
through SSH tunnels, expressed as one template instantiated four times. Resolve
it:

```sh
ainfra --chdir examples/multi-database lock
```

The regenerated `ainfra.lock` has four MCP servers with distinct,
tool-allocated tunnel ports.

## Command reference

| Command | What it does |
|---|---|
| `ainfra init` | Scaffold an `ainfra.yaml` (`--personal`, `--force`) |
| `ainfra validate` | Static-check the manifest without resolving it |
| `ainfra lock` | Resolve the manifest and write `ainfra.lock` |
| `ainfra plan` | Preview the diff between desired and observed state |
| `ainfra apply` | Reconcile the environment to the manifest |
| `ainfra check` | Verify the environment matches the lockfile |
| `ainfra version` | Print the ainfra version |

Global flags: `--chdir <dir>` runs as if started elsewhere; `--no-color`
disables colored output. `ainfra <command> --help` prints per-command detail.
```

- [ ] **Step 2: Update `README.md`**

In `README.md`, replace the final `## Build` section (currently lines 54–59, the heading and the fenced block under it) with:

```markdown
## Quick start

```sh
go build -o ainfra ./cmd/ainfra
ainfra version
```

A developer joining a team runs `ainfra plan` then `ainfra apply`. Someone
authoring a setup runs `ainfra init`, edits `ainfra.yaml`, then `ainfra lock`.
See [docs/quickstart.md](docs/quickstart.md) for the full walkthrough and the
`examples/multi-database/` worked example.

Run `ainfra --help` for the command list, or `ainfra <command> --help` for
per-command detail.
```

- [ ] **Step 3: Verify the build still works and run the example smoke test**

Run: `go build ./... && ./ainfra --chdir examples/multi-database lock`
Expected: build succeeds; the command prints a `resolved ... MCP servers ...`
summary and a `Next:` hint, exit 0.

- [ ] **Step 4: Confirm no unintended example lockfile changes**

Run: `git diff --stat examples/multi-database/ainfra.lock`
Expected: either no change, or only the `generatedAt` timestamp differs. If
only the timestamp changed, restore it: `git checkout examples/multi-database/ainfra.lock`.

- [ ] **Step 5: Commit**

```bash
git add docs/quickstart.md README.md
git commit -m "Add quick-start guide and rewrite README usage"
```

---

## Self-Review

**Spec coverage:**

- §2 two journeys — `docs/quickstart.md` documents both; `init --personal` (Task 10) supports the author's personal layer.
- §3 command surface — `init` (Task 10), `validate` (Task 11), `lock` re-skinned (Task 9), `version` with `--json` (Task 9), `plan`/`apply`/`check` pending-stubs (Task 9).
- §3.1 global flags — `--chdir`, `--no-color`, `--help`, `--version` in `cli.Dispatch` (Task 7).
- §3.2 exit codes — `0` success, `1` error/pending, `2` unknown command; the `2`-for-drift case lands with the provider follow-up (`check` is a stub now).
- §4 output language — `internal/ui` color + render primitives (Tasks 1, 4); `Next` hints used by `lock` (Task 9) and `init` (Task 10).
- §5 error formatting — `diag.Diagnostic` (Task 2), `ui.RenderError` (Task 3), `manifest` upgraded (Task 8).
- §6 help system — overview, per-command help, did-you-mean (Tasks 6, 7).
- §7 architecture — `internal/diag`, `internal/ui`, `internal/cli`, `cmd/ainfra` files all created as listed (with the documented refinement: `Diagnostic` type in its own package).
- §8 implementation slice — every numbered item maps to a task; provider-dependent behavior explicitly deferred.
- §9 testing — table tests for `ui` and `cli`, temp-dir tests for `init`/`validate`, existing suites kept green (Task 8 Step 5, Task 11 Step 6).

**Placeholder scan:** Every code step contains complete, compilable Go or Markdown. No `TODO`, no "similar to Task N", no shorthand types.

**Type consistency:** `cli.Context` fields (`Args`, `Stdout`, `Stderr`, `NoColor`, `Dir`) are defined in Task 6 and used unchanged in Tasks 9–11. `cli.Command` fields (`Name`, `Summary`, `UsageLine`, `Example`, `SetFlags`, `Run`) are consistent across Tasks 6, 9, 10, 11. `ui.Colorizer`, `ui.NewColorizer`, `ui.RenderError`, `ui.Section`, `ui.DiffLine`, `ui.PlanSummary`, `ui.Next`, `ui.Confirm` signatures defined in Tasks 1–5 match every call site. `diag.Diagnostic` fields (`Summary`, `File`, `Path`, `Detail`, `Hint`) are consistent across Tasks 2, 3, 8, 10. `manifest.Validate` and `manifest.ValidateAll` signatures match their callers in Task 11.

**Note on `ui.Section`/`ui.DiffLine`/`ui.PlanSummary`/`ui.Confirm`:** these are built and tested in Tasks 4–5 but not yet called by a command — they are the primitives the real `plan`/`apply` will use when the provider layer lands. They are in scope now because the spec (§8) lists the full `internal/ui` package as built-now, and building them against tests now means `plan`/`apply` are assembled, not designed, later.
