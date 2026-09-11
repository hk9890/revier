package tmux_test

import (
	"slices"
	"testing"

	"github.com/hk9890/revier/internal/adapter/tmux"
	"github.com/hk9890/revier/pkg/revier"
)

// The attach goes to the server this host speaks to, selects the window and
// attaches its session, which is how `revier open --attach` ends in the
// window it opened.
func TestAttachCommandSelectsTheWindowOnTheHostsServer(t *testing.T) {
	h := &tmux.Host{Socket: "revier-test", Session: "work"}
	argv, err := h.AttachCommand(revier.TargetRef{Host: "tmux", ID: "4242/@7", Title: "session:demo"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"tmux", "-u", "-L", "revier-test", "select-window", "-t", "@7", ";", "attach-session", "-t", "work"}
	if !slices.Equal(argv, want) {
		t.Errorf("argv = %v, want %v", argv, want)
	}
}

func TestAttachCommandUsesTheDefaultServerAndSession(t *testing.T) {
	argv, err := (&tmux.Host{}).AttachCommand(revier.TargetRef{Host: "tmux", ID: "4242/@1"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"tmux", "-u", "select-window", "-t", "@1", ";", "attach-session", "-t", "revier"}
	if !slices.Equal(argv, want) {
		t.Errorf("argv = %v, want %v", argv, want)
	}
}

func TestAttachCommandRefusesAnotherHostsRef(t *testing.T) {
	if _, err := (&tmux.Host{}).AttachCommand(revier.TargetRef{Host: "kitty", ID: "1"}); err == nil {
		t.Error("want a refusal for a ref that is not a tmux window")
	}
}

// The capability is optional and detected by type assertion, so the host
// has to satisfy the interface as a value the wiring holds it as.
func TestTmuxIsAnAttacher(t *testing.T) {
	var rt revier.Runtime = &tmux.Host{}
	if _, ok := rt.(revier.Attacher); !ok {
		t.Error("tmux.Host does not implement revier.Attacher")
	}
}
