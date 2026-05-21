// Package version exposes the build version of the ainfra CLI.
package version

// Version is the semantic version of this build. Overridden at release time
// via -ldflags "-X github.com/MHilhorst/ainfra/internal/version.Version=...".
var Version = "0.0.0-dev"
