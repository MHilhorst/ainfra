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
