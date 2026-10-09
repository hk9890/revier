package tmux

import "context"

// ParseVars is parseVars, for the parsing tests outside the package.
var ParseVars = parseVars

// SetRunner replaces tmux, so the L3 tests drive the host against a recorder
// with no tmux and no process.
func (h *Host) SetRunner(fn func(ctx context.Context, args ...string) (string, error)) {
	h.tmux = fn
}
