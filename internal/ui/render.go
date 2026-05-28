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
	pad := " "
	if n := nameColumn - len(name); n > 0 {
		pad = strings.Repeat(" ", n)
	}
	fmt.Fprintf(w, "  %s %s%s%s\n", sym, name, pad, c.Dim(detail))
}

// PlanSummary writes the "Plan: N to add, N to update, N to remove." line.
func PlanSummary(w io.Writer, add, change, remove int) {
	fmt.Fprintf(w, "Plan: %d to add, %d to update, %d to remove.\n", add, change, remove)
}

// Next writes a blank line, then a bold "Next:" prefix and a guidance string.
// Every command ends its successful output with one of these.
func Next(w io.Writer, c Colorizer, text string) {
	fmt.Fprintln(w)
	fmt.Fprintln(w, c.Bold("Next:"), text)
}
