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
