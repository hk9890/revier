package tmux_test

import (
	"context"
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

// tmux refuses a nested attach only when $TMUX is set and the client's
// terminal is a pane of the server. A terminal started from a pane inherits
// $TMUX and $TMUX_PANE but has a terminal of its own, and from there the
// attach works.
func TestEnclosesIsATerminalThatIsAPaneOfTheHostsServer(t *testing.T) {
	for _, tc := range []struct {
		name, env, tty string
		want           bool
	}{
		{"a pane of the host's server", "/tmp/tmux-1000/revier-test,4242,0", "/dev/pts/3", true},
		{"a terminal started from a pane", "/tmp/tmux-1000/revier-test,4242,0", "/dev/pts/9", false},
		{"outside tmux", "", "/dev/pts/3", false},
		{"no terminal", "/tmp/tmux-1000/revier-test,4242,0", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("TMUX", tc.env)
			t.Setenv("TMUX_PANE", "%3")
			h := &tmux.Host{Socket: "revier-test"}
			h.SetTTY(tc.tty)
			h.SetRunner(func(_ context.Context, args ...string) (string, error) {
				if !slices.Equal(args, []string{"list-panes", "-a", "-F", "#{pane_tty}"}) {
					t.Fatalf("asked tmux %q", args)
				}
				return "/dev/pts/2\n/dev/pts/3\n", nil
			})
			got, err := h.Encloses(context.Background())
			if err != nil || got != tc.want {
				t.Errorf("Encloses = %v, %v; want %v", got, err, tc.want)
			}
		})
	}
}

// The capabilities are optional and detected by type assertion, so the host
// has to satisfy each interface as a value the wiring holds it as.
func TestTmuxHasItsOptionalCapabilities(t *testing.T) {
	var rt revier.Runtime = &tmux.Host{}
	if _, ok := rt.(revier.Attacher); !ok {
		t.Error("tmux.Host does not implement revier.Attacher")
	}
	if _, ok := rt.(revier.Encloser); !ok {
		t.Error("tmux.Host does not implement revier.Encloser")
	}
	if _, ok := rt.(revier.PanelOpener); !ok {
		t.Error("tmux.Host does not implement revier.PanelOpener")
	}
}
