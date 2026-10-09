// What the kitty host asks of kitten, against a recording runner. No kitty, no
// display and no recorded output, so the default layer runs it.
package kitty_test

import (
	"context"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/adapter/kitty"
	"github.com/hk9890/revier/pkg/revier"
)

// call is one recorded kitten invocation.
type call struct {
	socket string
	args   []string
}

// parents is the process tree behind every recorded listing here, child to
// parent. The pids are made up, so the host must not read them from this
// machine's /proc, where they belong to other processes.
var parents = map[int]int{
	4002: 4001, 4003: 4002,
	5002: 5001, 5003: 5002, 5021: 5020,
	6002: 6001, 6003: 6002, 6004: 6003,
	7002: 7001, 7003: 1,
}

// newHost is a Host that reads the process tree from parents.
func newHost() *kitty.Host {
	h := &kitty.Host{}
	h.SetParents(func(pid int) (int, bool) {
		ppid, ok := parents[pid]
		return ppid, ok
	})
	return h
}

func TestOpenRequiresAName(t *testing.T) {
	h := newHost()
	h.SetSockets(func() []string { return []string{"unix:@kitty-4000"} })
	h.SetRunner(func(context.Context, string, string, ...string) ([]byte, error) { return nil, nil })
	if _, err := h.Open(context.Background(), revier.Realization{Launch: []string{"x"}}); err == nil {
		t.Fatal("want an error: without a name the OS window has no identity to match")
	}
}

// A prompt goes to one window of the kitty process the instance lives in, and
// through stdin: kitty reads an argument for escapes, and a backslash in the
// prompt must arrive as a backslash.
func TestSendTextGoesThroughStdinToTheWindow(t *testing.T) {
	h := newHost()
	var got []call
	var stdin []string
	h.SetSockets(func() []string { return []string{"unix:@kitty-4000"} })
	h.SetRunner(func(_ context.Context, socket, in string, args ...string) ([]byte, error) {
		got = append(got, call{socket, args})
		stdin = append(stdin, in)
		return nil, nil
	})
	var w revier.PanelWriter = h
	ref := revier.TargetRef{Host: "kitty", ID: "@kitty-4001/2"}
	if err := w.SendText(context.Background(), ref, "7", `fix a\b`); err != nil {
		t.Fatalf("SendText: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("kitten invoked %d times, want 1: %+v", len(got), got)
	}
	want := "send-text --match id:7 --stdin"
	if args := strings.Join(got[0].args, " "); args != want || got[0].socket != "unix:@kitty-4001" {
		t.Errorf("call = %s %q, want %q on the instance's own socket unix:@kitty-4001", got[0].socket, args, want)
	}
	if stdin[0] != `fix a\b` {
		t.Errorf("stdin = %q, want the text as it is", stdin[0])
	}
}
