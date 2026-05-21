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
