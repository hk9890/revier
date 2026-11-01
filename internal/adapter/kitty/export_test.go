package kitty

import "context"

// The injection points the L3 tests use to drive the host against recorded
// output and a recording runner, with no kitty installed.

func (h *Host) SetRunner(f func(ctx context.Context, socket, stdin string, args ...string) ([]byte, error)) {
	h.run = f
}

func (h *Host) SetSockets(f func() []string) { h.sockets = f }

func (h *Host) SetStarter(f func(ctx context.Context, args ...string) error) { h.start = f }

// StartProcess runs the real start of the workspace kitty, for the L4 test
// that puts scripts named kitty and systemd-run first on PATH.
func (h *Host) StartProcess(ctx context.Context, args ...string) error {
	return h.startProcess(ctx, args...)
}

func (h *Host) SetOwnSocket(f func() string)           { h.ownSocket = f }
func (h *Host) SetParents(f func(pid int) (int, bool)) { h.parent = f }
