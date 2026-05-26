package main

import (
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/MHilhorst/ainfra/internal/provider"
)

// gitEmail returns the caller's `git config user.email`, or "" if git is not
// installed or no user.email is set. The history log tolerates an empty actor
// rather than refusing to record an apply that ran without a git identity.
func gitEmail() string {
	out, err := exec.Command("git", "config", "user.email").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// appendApplyHistory writes one history event per non-noop change in results
// to root/.ainfra/history.jsonl. A failure to write history is reported to
// stderr but does not abort the apply — history is observational, not
// load-bearing.
func appendApplyHistory(root, command, agent, manifestHash string, results []provider.ApplyResult, stderr io.Writer) {
	base := provider.HistoryEvent{
		Command:      command,
		Actor:        gitEmail(),
		Agent:        agent,
		ManifestHash: manifestHash,
	}
	events := provider.EventsFromResults(results, base)
	if len(events) == 0 {
		return
	}
	if err := provider.AppendHistory(root, events); err != nil {
		fmt.Fprintf(stderr, "warning: failed to write apply history: %v\n", err)
	}
}
