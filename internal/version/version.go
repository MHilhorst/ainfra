// Package version exposes the build version of the aistack CLI.
package version

// Version is the semantic version of this build. Overridden at release time
// via -ldflags "-X github.com/MHilhorst/aistack/internal/version.Version=...".
var Version = "0.0.0-dev"
