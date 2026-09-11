package ssh

import "context"

// SetRun replaces the ssh call, so a test reads what would have been run and
// answers as the remote would.
func (r *Remote) SetRun(run func(ctx context.Context, args ...string) ([]byte, error)) { r.run = run }

// Quote exposes quote.
func Quote(s string) string { return quote(s) }

// Options exposes the flags every call carries.
func Options() []string { return options }
