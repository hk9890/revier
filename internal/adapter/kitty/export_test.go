package kitty

import "context"

// The injection points the L3 tests use to drive the host against recorded
// output and a recording runner, with no kitty installed.

func (h *Host) SetRunner(f func(ctx context.Context, socket string, args ...string) ([]byte, error)) {
	h.run = f
}

func (h *Host) SetSockets(f func() []string) { h.sockets = f }

func (h *Host) SetStarter(f func(ctx context.Context, args ...string) error) { h.start = f }
