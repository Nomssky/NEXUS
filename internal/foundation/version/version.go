// Package version exposes build identity for the NEXUS process.
//
// Values may be overridden at build time via -ldflags, e.g.:
//
//	go build -ldflags "-X github.com/Nomssky/NEXUS/internal/foundation/version.Version=1.0.0"
package version

// Version is the semantic version of the build.
var Version = "0.0.0-dev"

// Commit is the VCS revision the binary was built from.
var Commit = "unknown"
