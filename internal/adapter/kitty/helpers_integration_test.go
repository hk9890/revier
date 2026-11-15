//go:build integration

package kitty_test

import "sync"

// recorder collects calls; the host queries sockets concurrently.
type recorder struct {
	mu    sync.Mutex
	calls []call
}

func (r *recorder) add(socket string, args []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, call{socket, args})
}

func (r *recorder) all() []call {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]call(nil), r.calls...)
}
