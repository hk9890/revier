package tmux

import "context"

// ParseVars is parseVars, for the parsing tests outside the package.
var ParseVars = parseVars

// KindOf is kindOf, for the classification test outside the package.
var KindOf = kindOf

// SetRunner replaces tmux, so the tests drive the host against a recorder
// with no tmux and no process.
func (h *Host) SetRunner(fn func(ctx context.Context, args ...string) (string, error)) {
	h.run = fn
}

// SetHook keeps tmux and runs before ahead of each call and after behind it,
// with what the call printed, so a live test can act between two steps.
// Either may be nil.
func (h *Host) SetHook(before func(args []string), after func(args []string, out string)) {
	h.run = func(ctx context.Context, args ...string) (string, error) {
		if before != nil {
			before(args)
		}
		out, err := h.exec(ctx, args...)
		if after != nil {
			after(args, out)
		}
		return out, err
	}
}

// SetTTY replaces the terminal on standard input, so the tests need no
// terminal.
func (h *Host) SetTTY(tty string) {
	h.tty = func() string { return tty }
}
