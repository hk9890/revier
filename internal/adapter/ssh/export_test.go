package ssh

import "context"

// SetRunner replaces the ssh call, so a test reads what would have been run and
// answers as the remote would.
func (r *Remote) SetRunner(run func(ctx context.Context, args ...string) (stdout, stderr []byte, err error)) {
	r.run = run
}

// Quote exposes quote.
func Quote(s string) string { return quote(s) }

// Login exposes login.
func Login(line string) string { return login(line) }

// Quiet exposes quiet.
func Quiet(line string) string { return quiet(line) }

// Options exposes the flags every call carries.
func Options() []string { return options }

// Classify exposes classify, with a context a test does not have to make.
func Classify(host string, args []string, stderr string, err error) error {
	return classify(context.Background(), host, args, stderr, err)
}
