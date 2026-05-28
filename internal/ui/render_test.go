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
	want := "Plan: 2 to add, 1 to update, 0 to remove.\n"
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
