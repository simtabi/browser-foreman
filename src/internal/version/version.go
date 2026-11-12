// Package version holds build metadata, stamped by the Makefile with -ldflags -X.
package version

var (
	Version = "0.1.0-dev"
	Commit  = "none"
	Date    = "unknown"
)
