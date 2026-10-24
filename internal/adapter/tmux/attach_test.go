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

// $TMUX names the socket of the pane's server. The process is in a pane of
// the host's server only when that socket is the one the host's server
// reports, so a pane of another server is still attached from.
func TestEnclosesIsAPaneOfTheHostsServer(t *testing.T) {
	const socket = "/tmp/tmux-1000/revier-test"
	for _, tc := range []struct {
		name, env string
		want      bool
	}{
		{"a pane of the host's server", socket + ",4242,0", true},
		{"a pane of another server", "/tmp/tmux-1000/default,77,0", false},
		{"outside tmux", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("TMUX", tc.env)
			t.Setenv("TMUX_PANE", "%3")
			h := &tmux.Host{Socket: "revier-test"}
			h.SetRunner(func(_ context.Context, args ...string) (string, error) {
				if !slices.Equal(args, []string{"display-message", "-p", "#{socket_path}"}) {
					t.Fatalf("asked tmux %q", args)
				}
				return socket + "\n", nil
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
