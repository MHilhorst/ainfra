// Package installer generates the one-time installer and the scheduled job a
// subscriber machine uses to stay in sync with a published artifact.
// See docs/superpowers/specs/2026-05-22-subscriber-mode-design.md §6.
package installer

import (
	"fmt"
	"strings"
)

// Params drive launchd plist and install-script generation.
type Params struct {
	Label           string
	BinPath         string
	ArtifactURL     string
	IntervalMinutes int
	RunAtLogin      bool
}

// LaunchdPlist renders a launchd LaunchAgent plist that runs
// `ainfra apply --from <url> --yes` on an interval (StartInterval is in
// seconds) and, when RunAtLogin is set, also at login.
func LaunchdPlist(p Params) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">` + "\n")
	b.WriteString(`<plist version="1.0">` + "\n<dict>\n")
	fmt.Fprintf(&b, "  <key>Label</key><string>%s</string>\n", p.Label)
	b.WriteString("  <key>ProgramArguments</key>\n  <array>\n")
	for _, arg := range []string{p.BinPath, "apply", "--from", p.ArtifactURL, "--yes"} {
		fmt.Fprintf(&b, "    <string>%s</string>\n", arg)
	}
	b.WriteString("  </array>\n")
	if p.RunAtLogin {
		b.WriteString("  <key>RunAtLoad</key><true/>\n")
	}
	fmt.Fprintf(&b, "  <key>StartInterval</key><integer>%d</integer>\n", p.IntervalMinutes*60)
	b.WriteString("</dict>\n</plist>\n")
	return b.String()
}
