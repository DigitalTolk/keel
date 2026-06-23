// Package version exposes build metadata, populated at link time via -ldflags.
package version

import "fmt"

// These are overridden at build time:
//
//	-ldflags "-X github.com/DigitalTolk/keel/internal/version.Version=v1.2.3 ..."
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

// String returns a human-readable version line.
func String() string {
	return fmt.Sprintf("keel %s (commit %s, built %s)", Version, Commit, Date)
}
