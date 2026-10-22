package herdr

import "context"

// The injection points the tests use to drive the host with no herdr and no
// process.

// SetCaller replaces the socket: fn answers each request with the server's
// pid and the raw result.
func (h *Host) SetCaller(fn func(ctx context.Context, method string, params any) (int, []byte, error)) {
	h.call = fn
}

// SetProcRoot replaces /proc.
func (h *Host) SetProcRoot(root string) { h.procRoot = root }

// PaneProcesses is paneProcesses, for the tests outside the package.
var PaneProcesses = paneProcesses
