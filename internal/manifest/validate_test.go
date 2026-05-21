package manifest

import (
	"strings"
	"testing"
)

func TestValidateRejectsFloatingMCPVersion(t *testing.T) {
	m := &Manifest{Version: 1, MCPServers: map[string]MCPServer{
		"s": {Command: "npx", Args: []string{"-y", "pkg@latest"}},
	}}
	err := Validate(m)
	if err == nil || !strings.Contains(err.Error(), "pin an exact version") {
		t.Fatalf("want pinned-version error, got %v", err)
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
	err := Validate(m)
	if err == nil || !strings.Contains(err.Error(), "unknown template") {
		t.Fatalf("want unknown-template error, got %v", err)
	}
}

func TestValidateRejectsUnknownHookEvent(t *testing.T) {
	m := &Manifest{Version: 1, Hooks: map[string]Hook{
		"h": {Event: "OnEverything", Command: "echo x"},
	}}
	err := Validate(m)
	if err == nil || !strings.Contains(err.Error(), "event") {
		t.Fatalf("want hook-event error, got %v", err)
	}
}

func TestValidateRejectsHookWithoutCommand(t *testing.T) {
	m := &Manifest{Version: 1, Hooks: map[string]Hook{
		"h": {Event: "SessionStart"},
	}}
	err := Validate(m)
	if err == nil || !strings.Contains(err.Error(), "command") {
		t.Fatalf("want missing-command error, got %v", err)
	}
}

func TestValidateRejectsCommandWithoutSource(t *testing.T) {
	m := &Manifest{Version: 1, Commands: map[string]Command{
		"c": {Description: "no source"},
	}}
	err := Validate(m)
	if err == nil || !strings.Contains(err.Error(), "source") {
		t.Fatalf("want missing-source error, got %v", err)
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

func TestValidateRejectsScheduledJobWithoutSchedule(t *testing.T) {
	m := &Manifest{Version: 1, Targets: []string{"hub"},
		ScheduledJobs: map[string]ScheduledJob{
			"j": {Command: "echo x", RunsOn: []string{"hub"}},
		}}
	err := Validate(m)
	if err == nil || !strings.Contains(err.Error(), "schedule") {
		t.Fatalf("want schedule error, got %v", err)
	}
}

func TestValidateRejectsScheduledJobWithoutRunsOn(t *testing.T) {
	m := &Manifest{Version: 1, Targets: []string{"hub"},
		ScheduledJobs: map[string]ScheduledJob{
			"j": {Schedule: "0 6 * * *", Command: "echo x"},
		}}
	err := Validate(m)
	if err == nil || !strings.Contains(err.Error(), "runsOn") {
		t.Fatalf("want runsOn error, got %v", err)
	}
}

func TestValidateRejectsRunsOnOutsideVocabulary(t *testing.T) {
	m := &Manifest{Version: 1, Targets: []string{"hub"},
		ScheduledJobs: map[string]ScheduledJob{
			"j": {Schedule: "0 6 * * *", Command: "echo x", RunsOn: []string{"mars"}},
		}}
	err := Validate(m)
	if err == nil || !strings.Contains(err.Error(), "vocabulary") {
		t.Fatalf("want vocabulary error, got %v", err)
	}
}

func TestValidateRejectsHostTargetOutsideVocabulary(t *testing.T) {
	m := &Manifest{Version: 1, Targets: []string{"hub"},
		Host: Host{Targets: []string{"mars"}}}
	err := Validate(m)
	if err == nil || !strings.Contains(err.Error(), "vocabulary") {
		t.Fatalf("want vocabulary error, got %v", err)
	}
}

func TestValidateAcceptsValidScheduledJob(t *testing.T) {
	m := &Manifest{Version: 1, Targets: []string{"hub", "laptop"},
		Host: Host{Targets: []string{"hub"}},
		ScheduledJobs: map[string]ScheduledJob{
			"j": {Schedule: "0 6 * * *", Command: "echo x", RunsOn: []string{"hub"}},
		}}
	if err := Validate(m); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
