// What the kitty host asks of kitten, against a recording runner. No kitty, no
// display and no recorded output, so the default layer runs it.
package kitty_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/hk9890/revier/pkg/revier"
)

// A realization with no name is refused before kitten is asked: an Open that
// got as far as a launch fails as well, on the reply.
func TestOpenRequiresAName(t *testing.T) {
	h := newHost()
	var got []call
	h.SetSockets(func() []string { return []string{"unix:@kitty-4000"} })
	h.SetRunner(func(_ context.Context, socket, _ string, args ...string) ([]byte, error) {
		got = append(got, call{socket, args})
		return nil, nil
	})
	if _, err := h.Open(context.Background(), revier.Realization{Launch: []string{"x"}}); err == nil {
		t.Fatal("want an error: without a name the OS window has no identity to match")
	}
	if len(got) != 0 {
		t.Errorf("kitten invoked %d times, want none: %+v", len(got), got)
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

// A window kitten no longer matches has closed: its read says the panel is
// gone, and any other failure stays a failure (decisions.md D172).
func TestReadPanelOfAClosedWindowSaysThePanelIsGone(t *testing.T) {
	h := newHost()
	h.SetSockets(func() []string { return []string{"unix:@kitty-4000"} })
	var fail error
	h.SetRunner(func(context.Context, string, string, ...string) ([]byte, error) { return nil, fail })
	ref := revier.TargetRef{Host: "kitty", ID: "@kitty-4000/2"}

	fail = errors.New("exit status 1: Error: No matching windows for expression: id:7")
	if _, err := h.ReadPanel(t.Context(), ref, "7", false); !errors.Is(err, revier.ErrPanelGone) || !strings.Contains(err.Error(), "id:7") {
		t.Errorf("ReadPanel of a closed window = %v, want ErrPanelGone with kitten's reason", err)
	}
	fail = errors.New("exit status 1: Error: timed out")
	if _, err := h.ReadPanel(t.Context(), ref, "7", false); err == nil || errors.Is(err, revier.ErrPanelGone) {
		t.Errorf("ReadPanel that timed out = %v, want a failure that is not ErrPanelGone", err)
	}
}
