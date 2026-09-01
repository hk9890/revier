package gnome

import "github.com/hk9890/revier/pkg/revier"

// Decode and DecodeFocused expose the parsers to the L3 tests, which run
// against recorded wctl output rather than a live GNOME session.
func (h *Host) Decode(raw []byte) ([]revier.Instance, error) { return h.decode(raw) }

func (h *Host) DecodeFocused(raw []byte) (revier.TargetRef, error) { return h.decodeFocused(raw) }
