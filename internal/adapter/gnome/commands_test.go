// The commands the GNOME host sends, against a recorder in place of wctl. No
// GNOME session, no wctl binary and no recorded output, so the default layer
// runs it.
package gnome_test

import (
	"context"
	"slices"
	"testing"

	"github.com/hk9890/revier/pkg/revier"
)

// Place builds the command the extension expects, and asks wctl for no wait
// on the frame: a placement holds a launch and every step of a restore for as
// long as wctl waits.
func TestPlaceBuildsTheCommand(t *testing.T) {
	w := &wctl{}
	err := w.host().Place(context.Background(),
		revier.TargetRef{Host: "gnome", ID: "4181121382"},
		[]string{"right", "top", "75%", "100%"})
	if err != nil {
		t.Fatalf("Place: %v", err)
	}
	if want := []string{"place 4181121382 right top 75% 100%"}; !slices.Equal(w.calls, want) {
		t.Errorf("calls = %q, want %q", w.calls, want)
	}
}

// Close asks wctl to close the window by its id, which is the polite close a
// close button makes.
func TestCloseBuildsTheCommand(t *testing.T) {
	w := &wctl{}
	if err := w.host().Close(context.Background(), revier.TargetRef{Host: "gnome", ID: "4181121382"}); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if want := []string{"close 4181121382"}; !slices.Equal(w.calls, want) {
		t.Errorf("calls = %q, want %q", w.calls, want)
	}
}

// The popup's size is decided from this width before its launch, so a reply
// without one must fail rather than read as a zero-wide screen.
func TestWorkareaWidthReadsTheReply(t *testing.T) {
	cases := []struct {
		name, reply string
		want        int
		fail        bool
	}{
		{name: "wctl 0.11.0", reply: `{"monitor_index":0,"x":0,"y":32,"width":5120,"height":1408}`, want: 5120},
		{name: "no width", reply: `{"monitor_index":0}`, fail: true},
		{name: "not json", reply: `Window Control extension is not running`, fail: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := &wctl{reply: func(args string) ([]byte, error) {
				if args != "workarea --json" {
					return nil, refused(args, 9, "")
				}
				return []byte(tc.reply), nil
			}}
			got, err := w.host().WorkareaWidth(context.Background())
			if tc.fail {
				if err == nil {
					t.Fatalf("width = %d, want an error", got)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("width = %d, %v; want %d", got, err, tc.want)
			}
		})
	}
}

// Cover moves the window to its monitor and unmaximizes it before it makes it
// fullscreen: a maximized window is under a fullscreen one, and mutter may map
// the curtain's kitty maximized.
func TestCoverBuildsTheCommands(t *testing.T) {
	w := &wctl{}
	if err := w.host().Cover(t.Context(), revier.TargetRef{Host: "gnome", ID: "4181121382"}, 1); err != nil {
		t.Fatalf("Cover: %v", err)
	}
	want := []string{
		"move-to-monitor 4181121382 1",
		"unmaximize 4181121382",
		"fullscreen 4181121382",
		"above 4181121382 on",
		"sticky 4181121382 on",
	}
	if !slices.Equal(w.calls, want) {
		t.Errorf("calls = %q, want %q", w.calls, want)
	}
}

// A command that fails ends the cover: a window that is not on its monitor
// is not made fullscreen on another one.
func TestCoverStopsAtTheFirstFailure(t *testing.T) {
	w := &wctl{reply: func(args string) ([]byte, error) {
		if args == "move-to-monitor 1 3" {
			return nil, refused(args, 1, "no monitor 3")
		}
		return nil, nil
	}}
	if err := w.host().Cover(t.Context(), revier.TargetRef{Host: "gnome", ID: "1"}, 3); err == nil {
		t.Fatal("Cover on a monitor wctl refuses: want an error")
	}
	if want := []string{"move-to-monitor 1 3"}; !slices.Equal(w.calls, want) {
		t.Errorf("calls = %q, want %q", w.calls, want)
	}
}

// Above turns the state on and off by name.
func TestAboveBuildsTheCommand(t *testing.T) {
	w := &wctl{}
	h := w.host()
	ref := revier.TargetRef{Host: "gnome", ID: "7"}
	if err := h.Above(t.Context(), ref, false); err != nil {
		t.Fatalf("Above off: %v", err)
	}
	if err := h.Above(t.Context(), ref, true); err != nil {
		t.Fatalf("Above on: %v", err)
	}
	if want := []string{"above 7 off", "above 7 on"}; !slices.Equal(w.calls, want) {
		t.Errorf("calls = %q, want %q", w.calls, want)
	}
}

// Monitors asks wctl once and keeps the index and the primary flag of each.
func TestMonitorsReadsTheReply(t *testing.T) {
	w := &wctl{reply: func(args string) ([]byte, error) {
		if args != "monitors --json" {
			return nil, refused(args, 9, "")
		}
		return []byte(`[{"index":0,"is_primary":false,"width":1920},{"index":1,"is_primary":true,"width":2560}]`), nil
	}}
	got, err := w.host().Monitors(t.Context())
	if err != nil {
		t.Fatalf("Monitors: %v", err)
	}
	if want := []revier.Monitor{{Index: 0}, {Index: 1, Primary: true}}; !slices.Equal(got, want) {
		t.Errorf("monitors = %+v, want %+v", got, want)
	}
}

// A reply with no monitor, or no JSON, fails rather than read as a desktop
// with no screen to cover.
func TestMonitorsRefusesAnEmptyReply(t *testing.T) {
	for _, reply := range []string{`[]`, `Window Control extension is not running`} {
		w := &wctl{reply: func(string) ([]byte, error) { return []byte(reply), nil }}
		if got, err := w.host().Monitors(t.Context()); err == nil {
			t.Errorf("reply %q: monitors = %+v, want an error", reply, got)
		}
	}
}

// A zero ref is refused before wctl is asked.
func TestCoverAndAboveRefuseAZeroRef(t *testing.T) {
	w := &wctl{}
	h := w.host()
	if err := h.Cover(t.Context(), revier.TargetRef{}, 0); err == nil {
		t.Error("Cover: a zero ref should be refused")
	}
	if err := h.Above(t.Context(), revier.TargetRef{}, true); err == nil {
		t.Error("Above: a zero ref should be refused")
	}
	if len(w.calls) != 0 {
		t.Errorf("calls = %q, want wctl not asked", w.calls)
	}
}

// A zero ref and a short geometry are refused before wctl is asked, so a
// broken project file cannot reach the compositor.
func TestPlaceRefusesWhatItCannotSend(t *testing.T) {
	w := &wctl{}
	h := w.host()
	if err := h.Place(context.Background(), revier.TargetRef{}, []string{"a", "b", "c", "d"}); err == nil {
		t.Error("a zero ref should be refused")
	}
	if err := h.Place(context.Background(), revier.TargetRef{ID: "1"}, []string{"a"}); err == nil {
		t.Error("a geometry short of four tokens should be refused")
	}
	if len(w.calls) != 0 {
		t.Errorf("calls = %q, want wctl not asked", w.calls)
	}
}
