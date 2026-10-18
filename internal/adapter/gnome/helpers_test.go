package gnome_test

import (
	"context"
	"strings"

	"github.com/hk9890/revier/internal/adapter/gnome"
)

// wctl stands in for the wctl binary: it records the arguments of every
// call, joined, and answers with what reply returns for them. With no reply
// every call succeeds and prints nothing.
type wctl struct {
	calls []string
	reply func(args string) ([]byte, error)
}

// host is a GNOME host that runs w in place of wctl.
func (w *wctl) host() *gnome.Host {
	h := &gnome.Host{}
	h.SetRunner(func(_ context.Context, args ...string) ([]byte, error) {
		joined := strings.Join(args, " ")
		w.calls = append(w.calls, joined)
		if w.reply == nil {
			return nil, nil
		}
		return w.reply(joined)
	})
	return h
}
