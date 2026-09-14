package tmux_test

import (
	"slices"
	"testing"

	"github.com/hk9890/revier/internal/adapter/tmux"
	"github.com/hk9890/revier/pkg/revier"
)

// The attach goes to the server this host speaks to and targets the session
// itself, which is how `revier open --attach` ends in the workspace it opened,
// or matched, and in no other.
func TestAttachCommandTargetsTheSessionOnTheHostsServer(t *testing.T) {
	h := &tmux.Host{Socket: "revier-test"}
	argv, err := h.AttachCommand(revier.TargetRef{Host: "tmux", ID: "4242/$7", Title: "session:demo"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"tmux", "-u", "-L", "revier-test", "attach-session", "-t", "$7"}
	if !slices.Equal(argv, want) {
		t.Errorf("argv = %v, want %v", argv, want)
	}
}

func TestAttachCommandUsesTheDefaultServer(t *testing.T) {
	argv, err := (&tmux.Host{}).AttachCommand(revier.TargetRef{Host: "tmux", ID: "4242/$1"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"tmux", "-u", "attach-session", "-t", "$1"}
	if !slices.Equal(argv, want) {
		t.Errorf("argv = %v, want %v", argv, want)
	}
}

func TestAttachCommandRefusesAnotherHostsRef(t *testing.T) {
	if _, err := (&tmux.Host{}).AttachCommand(revier.TargetRef{Host: "kitty", ID: "1"}); err == nil {
		t.Error("want a refusal for a ref that is not a tmux session")
	}
}

// The capabilities are optional and detected by type assertion, so the host
// has to satisfy each interface as a value the wiring holds it as.
func TestTmuxHasItsOptionalCapabilities(t *testing.T) {
	var rt revier.Runtime = &tmux.Host{}
	if _, ok := rt.(revier.Attacher); !ok {
		t.Error("tmux.Host does not implement revier.Attacher")
	}
	if _, ok := rt.(revier.PanelOpener); !ok {
		t.Error("tmux.Host does not implement revier.PanelOpener")
	}
}
