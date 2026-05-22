package installer

import (
	"strings"
	"testing"
)

func TestLaunchdPlistContainsIntervalAndURL(t *testing.T) {
	out := LaunchdPlist(Params{
		Label: "com.ainfra.subscriber", BinPath: "/usr/local/bin/ainfra",
		ArtifactURL: "https://x/a", IntervalMinutes: 360, RunAtLogin: true,
	})
	for _, want := range []string{"com.ainfra.subscriber", "https://x/a", "<integer>21600</integer>", "RunAtLoad"} {
		if !strings.Contains(out, want) {
			t.Errorf("plist missing %q", want)
		}
	}
}

func TestLaunchdPlistOmitsRunAtLoadWhenFalse(t *testing.T) {
	out := LaunchdPlist(Params{Label: "x", BinPath: "/b", ArtifactURL: "https://x", IntervalMinutes: 60, RunAtLogin: false})
	if strings.Contains(out, "RunAtLoad") {
		t.Error("RunAtLoad must be absent when RunAtLogin is false")
	}
}

func TestInstallScriptReferencesURLAndPlistPath(t *testing.T) {
	p := Params{
		Label:           "com.ainfra.subscriber",
		BinPath:         "ainfra",
		ArtifactURL:     "https://example.com/artifact",
		IntervalMinutes: 60,
		RunAtLogin:      true,
	}
	plist := LaunchdPlist(p)
	script := InstallScript(p, plist)
	for _, want := range []string{"https://example.com/artifact", "Library/LaunchAgents", "launchctl load"} {
		if !strings.Contains(script, want) {
			t.Errorf("install script missing %q", want)
		}
	}
}
