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
