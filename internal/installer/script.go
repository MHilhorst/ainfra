package installer

import (
	"fmt"
	"strings"
)

// InstallScript returns a macOS .command bash script that, when double-clicked
// by a subscriber, installs the launchd agent and runs an initial apply.
func InstallScript(p Params, plist string) string {
	plistPath := fmt.Sprintf("$HOME/Library/LaunchAgents/%s.plist", p.Label)

	var b strings.Builder
	b.WriteString("#!/bin/bash\n")
	b.WriteString("set -e\n\n")

	b.WriteString("# Locate the ainfra binary.\n")
	b.WriteString("AINFRA=$(command -v ainfra 2>/dev/null || echo \"$HOME/.ainfra/bin/ainfra\")\n")
	b.WriteString("if [ ! -x \"$AINFRA\" ]; then\n")
	b.WriteString("  echo \"error: ainfra not found on PATH and not at $HOME/.ainfra/bin/ainfra\" >&2\n")
	b.WriteString("  echo \"Install ainfra first: https://github.com/MHilhorst/ainfra\" >&2\n")
	b.WriteString("  exit 1\n")
	b.WriteString("fi\n\n")

	b.WriteString("# Create local bin directory.\n")
	b.WriteString("mkdir -p \"$HOME/.ainfra/bin\"\n\n")

	b.WriteString("# Write the launchd plist.\n")
	b.WriteString("mkdir -p \"$HOME/Library/LaunchAgents\"\n")
	fmt.Fprintf(&b, "cat > %q << 'AINFRA_PLIST_EOF'\n", plistPath)
	b.WriteString(plist)
	b.WriteString("AINFRA_PLIST_EOF\n\n")

	b.WriteString("# Load the launchd agent (unload first to allow re-runs).\n")
	fmt.Fprintf(&b, "launchctl unload %q 2>/dev/null || true\n", plistPath)
	fmt.Fprintf(&b, "launchctl load %q\n\n", plistPath)

	b.WriteString("# Run the first apply immediately.\n")
	fmt.Fprintf(&b, "\"$AINFRA\" apply --from %q --yes\n\n", p.ArtifactURL)

	b.WriteString("echo \"ainfra subscriber installed and up to date.\"\n")
	return b.String()
}
