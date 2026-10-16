// Package userguide carries the pages of the user guide that the binary
// prints: the configuration reference, which `revier assist --reference` hands
// to the agent that writes the configuration.
package userguide

import _ "embed"

// Configuration, Status and Probes are the reference pages as this build was
// made from them.
var (
	//go:embed configuration.md
	Configuration string
	//go:embed extending/status.md
	Status string
	//go:embed extending/probes.md
	Probes string
)
