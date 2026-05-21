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
