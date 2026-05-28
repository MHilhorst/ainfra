// Command ainfra is the config-as-code CLI for Claude Code team environments.
package main

import (
	"io"
	"os"

	"github.com/MHilhorst/ainfra/internal/cli"
	"github.com/MHilhorst/ainfra/internal/version"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run builds the command registry and dispatches args. It is separate from
// main so tests can drive it with their own streams.
func run(args []string, stdout, stderr io.Writer) int {
	reg := cli.NewRegistry(stdout, stderr, version.Version)
	reg.Add(newInitCommand())
	reg.Add(newInstallCommand())
	reg.Add(newAddCommand())
	reg.Add(newRemoveCommand())
	reg.Add(newUpdateCommand())
	reg.Add(newListCommand())
	reg.Add(newInspectCommand())
	reg.Add(newOutdatedCommand())
	reg.Add(newVersionCommand())
	// Hidden helpers — still callable but omitted from `ainfra --help`.
	reg.Add(newLockCommand())
	reg.Add(newPublishCommand())
	reg.Add(newInstallerCommand())
	reg.Add(newStalenessCheckCommand())
	return reg.Dispatch(args)
}
