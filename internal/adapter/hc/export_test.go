package hc

import "context"

// SocketPath is socketPath, for the tests outside the package.
var SocketPath = socketPath

// CoreEnv is coreEnv, for the tests outside the package.
var CoreEnv = coreEnv

// SetProcRoot replaces /proc.
func (h *Host) SetProcRoot(root string) { h.procRoot = root }

// Program is program, for a live test that skips where the host finds no hc.
var Program = program

// Request is one call on a session of its own, for a live test that arranges
// or ends the core through hc's API, for what the host does not do.
func (h *Host) Request(ctx context.Context, method string, params, out any) error {
	s, err := h.dial(ctx, false)
	if err != nil {
		return err
	}
	defer s.close()
	return s.call(method, params, out)
}

// CorePID is the pid of the core that answers, for a live test that reads
// the core's own environment.
func (h *Host) CorePID(ctx context.Context) (int, error) {
	s, err := h.dial(ctx, false)
	if err != nil {
		return 0, err
	}
	defer s.close()
	return s.pid, nil
}
