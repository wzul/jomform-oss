// Package version exposes build information for jomform-api.
package version

// Values are set at build time via -ldflags.
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)