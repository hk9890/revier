// Package design carries the one design document the binary prints: the
// configuration format, which `revier assist --reference` hands to the agent
// that writes it.
package design

import _ "embed"

// Extending is extending.md as this build was made from it.
//
//go:embed extending.md
var Extending string
