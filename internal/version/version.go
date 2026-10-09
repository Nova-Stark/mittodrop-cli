package version

import "fmt"

// Values injected at link-time via ldflags (-X mittodrop/internal/version.Version=...)
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

// Info returns formatted version string.
func Info() string {
	return fmt.Sprintf("mittodrop version %s (commit %s, built %s)", Version, Commit, Date)
}
