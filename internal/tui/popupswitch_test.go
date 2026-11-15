package tui_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/popupswitch"
)

// The popup takes the second press of the desktop's key over its socket, a
// real one here: the press switches the list as the switch key pressed in
// the popup does, and the popup waits on for the next press, which switches
// the list again (decisions.md D182).
func TestThePopupSwitchesItsListOnAPressOverItsSocket(t *testing.T) {
	_, _, c, projects := world(t, 3)
	path := filepath.Join(t.TempDir(), "popup.sock")
	ln, err := popupswitch.Listen(path)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil).WithPopup().WithSwitchSocket(ln), 140, 20)
	if bar := barLine(m); !strings.Contains(bar, "agents ") {
		t.Fatalf("bar = %q, want the switch to the agents on it", bar)
	}

	if err := popupswitch.Switch(t.Context(), path); err != nil {
		t.Fatalf("Switch: %v", err)
	}
	m, next := deliver(m, m.AwaitSwitch())
	if bar := barLine(m); !strings.Contains(bar, "projects ") {
		t.Fatalf("bar = %q after a press over the socket, want the agent list", bar)
	}

	m = switched(m)
	if err := popupswitch.Switch(t.Context(), path); err != nil {
		t.Fatalf("second Switch: %v", err)
	}
	m = run(m, next)
	if bar := barLine(m); !strings.Contains(bar, "projects ") {
		t.Errorf("bar = %q after the second press over the socket, want the agent list again", bar)
	}
}

// A surface that is no popup has no socket, and waits on none.
func TestASurfaceWithNoSocketWaitsForNoPress(t *testing.T) {
	_, _, c, projects := world(t, 1)
	if cmd := refreshed(t, c, projects, stateWith(t, nil), nil).AwaitSwitch(); cmd != nil {
		t.Error("a surface with no socket waits for a press")
	}
}
