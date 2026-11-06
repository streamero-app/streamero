// Package version holds build information injected via -ldflags.
package version

var (
	// Version is the semantic version of the build ("dev" for local builds).
	Version = "dev"
	// Commit is the git commit the binary was built from.
	Commit = "unknown"
)

// APIVersion is the version of the public HTTP API.
const APIVersion = "v1"
