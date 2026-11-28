// Package build carries the version, commit and date that a build stamps in
// by ldflags: .goreleaser.yaml for a release, .mise.toml and the makefile for
// a local build.
package build

var (
	Version = "dev"
	Commit  = "unknown"
	Date    = "unknown"
)
