// Package build carries the version, commit and date that .goreleaser.yaml and
// the build task of .mise.toml stamp in by ldflags.
package build

var (
	Version = "dev"
	Commit  = "unknown"
	Date    = "unknown"
)
