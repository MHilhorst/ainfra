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
	reg.Add(newListCommand())
	reg.Add(newOutdatedCommand())
	reg.Add(newVersionCommand())
	// Hidden / deprecated aliases — kept callable through 0.x for backward compat.
	reg.Add(newValidateCommand())
	reg.Add(newSchemaCommand())
	reg.Add(newLockCommand())
	reg.Add(newPublishCommand())
	reg.Add(newInstallerCommand())
	reg.Add(newPlanCommand())
	reg.Add(newApplyCommand())
	reg.Add(newCheckCommand())
	reg.Add(newExecCommand())
	reg.Add(newSyncCommand())
	reg.Add(newHistoryCommand())
	return reg.Dispatch(args)
}
