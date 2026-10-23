// The commands the GNOME host sends, against a recorder in place of wctl. No
// GNOME session, no wctl binary and no recorded output, so the default layer
// runs it.
package gnome_test

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/adapter/gnome"
	"github.com/hk9890/revier/pkg/revier"
)

// refused is what the host gets from a wctl that exits non-zero: the command,
// the exit status, and what wctl wrote to its standard error.
func refused(args string, code int, stderr string) error {
	return fmt.Errorf("wctl %s: exit status %d: %s", args, code, stderr)
}

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

// Hide minimizes with --no-animation, which is what keeps the popup in place
// on Esc, and Focus activates with it, which brings the popup back in place.
// A wctl without the flag and an extension without the method each refuse it
// in their own words; the call is then made with the animation and the flag
// is not asked for again, so one process pays for the refusal once. A wctl
// without the flag answers minimize with its usage, and activate, which has
// its own parser, with the option it does not know. An extension that is not
// running exits as the old one does and is no refusal: it reaches the caller,
// and the next call asks again. So does a refusal that the call with the
// animation fails after, which is how wctl words an extension that is not
// running where the shell's locale is not English.
func TestHideAndFocusSkipTheAnimationWhereWctlCan(t *testing.T) {
	ref := revier.TargetRef{Host: "gnome", ID: "4181121382"}
	t.Run("hide", func(t *testing.T) {
		testSkipsTheAnimation(t, "minimize", "Usage: wctl minimize <WINDOW>", "MinimizeNoAnimation",
			func(h *gnome.Host) error { return h.Hide(context.Background(), ref) })
	})
	t.Run("focus", func(t *testing.T) {
		testSkipsTheAnimation(t, "activate", "Unknown option: --no-animation", "ActivateNoAnimation",
			func(h *gnome.Host) error { return h.Focus(context.Background(), ref) })
	})
}

// A wctl and an extension can each have one flag and not the other, so a
// refusal of one is not remembered for the other.
func TestARefusedHideLeavesFocusWithoutTheAnimation(t *testing.T) {
	w := &wctl{reply: func(args string) ([]byte, error) {
		if strings.HasPrefix(args, "minimize") && strings.HasSuffix(args, "--no-animation") {
			return nil, refused(args, 1, "Error: Usage: wctl minimize <WINDOW>")
		}
		return nil, nil
	}}
	h := w.host()
	ref := revier.TargetRef{Host: "gnome", ID: "4181121382"}
	if err := h.Hide(context.Background(), ref); err != nil {
		t.Fatalf("Hide: %v", err)
	}
	if err := h.Focus(context.Background(), ref); err != nil {
		t.Fatalf("Focus: %v", err)
	}
	want := []string{"minimize 4181121382 --no-animation", "minimize 4181121382", "activate 4181121382 --no-animation"}
	if !slices.Equal(w.calls, want) {
		t.Errorf("calls = %q, want %q", w.calls, want)
	}
}

func testSkipsTheAnimation(t *testing.T, command, flagUnknown, method string, call func(*gnome.Host) error) {
	flagged, plain := command+" 4181121382 --no-animation\n", command+" 4181121382\n"
	cases := []struct {
		name          string
		refusal       string // wctl's stderr for --no-animation; empty when it accepts
		always        bool   // the call with the animation gets the same answer
		code          int
		first, second string // the argv of each of two calls
		fail          bool
	}{
		{name: "supported", first: flagged, second: flagged},
		{
			name:    "wctl without the flag",
			refusal: "Error: " + flagUnknown,
			code:    1,
			first:   flagged + plain, second: plain,
		},
		{
			name: "extension without the method",
			refusal: "Error: No such method “" + method + "”. The extension GNOME Shell" +
				" has loaded is older than this wctl.",
			code:  5,
			first: flagged + plain, second: plain,
		},
		{
			name:    "extension not running",
			refusal: "Error: Window Control extension is not running. Enable it in GNOME Extensions.",
			code:    5,
			first:   flagged, second: flagged,
			fail: true,
		},
		{
			name: "extension not running, worded as one without the method",
			refusal: "Error: Objekt existiert nicht am Pfad. The extension GNOME Shell" +
				" has loaded is older than this wctl.",
			always: true,
			code:   5,
			first:  flagged + plain, second: flagged + plain,
			fail: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := &wctl{reply: func(args string) ([]byte, error) {
				if tc.refusal != "" && (tc.always || strings.HasSuffix(args, "--no-animation")) {
					return nil, refused(args, tc.code, tc.refusal)
				}
				return nil, nil
			}}
			h := w.host()
			for i, want := range []string{tc.first, tc.second} {
				if err := call(h); (err != nil) != tc.fail {
					t.Fatalf("call %d: err = %v, want failure %v", i+1, err, tc.fail)
				}
				if got := strings.Join(w.calls, "\n") + "\n"; got != want {
					t.Errorf("call %d: argv = %q, want %q", i+1, got, want)
				}
				w.calls = nil
			}
		})
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
