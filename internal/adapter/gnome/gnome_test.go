//go:build integration

// Layer L3: the wctl parsers, against recorded output. No GNOME session, no
// wctl binary, no display. The fixtures are hand-written to the shape wctl
// 0.7.0 emits; real captured output carries the user's window titles and does
// not belong in a repository.
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

func TestDecodeList(t *testing.T) {
	got, err := (&gnome.Host{}).Decode(read(t, "list.json"), onWorkspace(0))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	// The hidden helper is on no workspace: mutter has not shown it yet.
	if len(got) != 3 {
		t.Fatalf("got %d instances, want 3 (the unshown one dropped)", len(got))
	}

	first := got[0]
	if first.Ref.Host != "gnome" || first.Ref.ID != "3811157392" {
		t.Errorf("ref = %+v, want gnome/3811157392", first.Ref)
	}
	if first.Title != "session:revier" || first.Class != "kitty" || first.PID != 162022 {
		t.Errorf("instance = %+v", first)
	}
}

// The fields a realization matches on must survive the decode, or every
// declared target silently stops resolving.
func TestDecodedInstancesMatchRealizations(t *testing.T) {
	instances, err := (&gnome.Host{}).Decode(read(t, "list.json"), onWorkspace(0))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	cases := []struct {
		name  string
		match revier.Match
		want  string
	}{
		{"workspace by title", revier.Match{Class: "^kitty$", Title: "^session:revier$"}, "3811157392"},
		{"editor by class", revier.Match{Class: "^code$"}, "3811157401"},
		{"page by marker class", revier.Match{Class: "^revier-revier-pulls$"}, "3811157410"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, err := tc.match.Compile()
			if err != nil {
				t.Fatalf("Compile: %v", err)
			}
			for _, inst := range instances {
				if m.Matches(inst) {
					if inst.Ref.ID != tc.want {
						t.Errorf("matched %s, want %s", inst.Ref.ID, tc.want)
					}
					return
				}
			}
			t.Errorf("no instance matched %+v", tc.match)
		})
	}
}

// onWorkspace is an active-workspace answer for Decode.
func onWorkspace(n int) func() (int, bool) { return func() (int, bool) { return n, true } }

// A hidden window is a window revier can raise unless mutter has not shown it
// yet: activate restores a minimized window and switches to one on another
// workspace. The active workspace is asked only when the answer depends on it.
func TestDecodeKeepsEveryHiddenWindowActivateCanRaise(t *testing.T) {
	cases := []struct {
		name   string
		window string
		keep   bool
		asks   bool
	}{
		{"shown", `"is_hidden": false, "workspace_index": 0`, true, false},
		{"minimized", `"is_hidden": true, "is_minimized": true, "workspace_index": 0`, true, false},
		{"on another workspace", `"is_hidden": true, "workspace_index": 1`, true, true},
		{"unshown on the active workspace", `"is_hidden": true, "workspace_index": 0`, false, true},
		{"unshown on no workspace", `"is_hidden": true, "workspace_index": -1`, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			asked := false
			active := func() (int, bool) { asked = true; return 0, true }
			// Alone in the listing, so no shown window tells the workspace.
			raw := `[{"id": 1, "title": "session:revier", "wm_class": "kitty", ` + tc.window + `}]`
			got, err := (&gnome.Host{}).Decode([]byte(raw), active)
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if kept := len(got) == 1; kept != tc.keep {
				t.Errorf("kept = %v, want %v", kept, tc.keep)
			}
			if asked != tc.asks {
				t.Errorf("asked for the active workspace = %v, want %v", asked, tc.asks)
			}
		})
	}
}

// A shown window tells the active workspace, so a desktop in use costs no
// second wctl call however many of its windows are on other workspaces.
func TestDecodeReadsTheActiveWorkspaceFromAShownWindow(t *testing.T) {
	raw := `[
		{"id": 1, "title": "editor", "is_hidden": false, "workspace_index": -1},
		{"id": 2, "title": "session:revier", "is_hidden": false, "workspace_index": 1},
		{"id": 3, "title": "session:setup", "is_hidden": true, "workspace_index": 0},
		{"id": 4, "title": "unshown", "is_hidden": true, "workspace_index": 1}
	]`
	asked := false
	got, err := (&gnome.Host{}).Decode([]byte(raw), func() (int, bool) { asked = true; return 0, true })
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if asked {
		t.Error("asked wctl for the active workspace; the shown window on workspace 1 says it")
	}
	var titles []string
	for _, inst := range got {
		titles = append(titles, inst.Title)
	}
	if want := "editor session:revier session:setup"; strings.Join(titles, " ") != want {
		t.Errorf("kept %v, want %s: the window on workspace 0 is on another workspace, the one on 1 is unshown", titles, want)
	}
}

// When the active workspace cannot be learned, a hidden window with a
// workspace is kept: dropped, a window on another workspace is not found.
func TestDecodeKeepsAHiddenWindowWhenTheWorkspaceIsUnknown(t *testing.T) {
	raw := `[{"id": 1, "title": "session:revier", "is_hidden": true, "workspace_index": 2}]`
	got, err := (&gnome.Host{}).Decode([]byte(raw), func() (int, bool) { return 0, false })
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("got %d instances, want the window kept", len(got))
	}
}

func TestDecodeFocused(t *testing.T) {
	ref, err := (&gnome.Host{}).DecodeFocused(read(t, "focused.json"))
	if err != nil {
		t.Fatalf("DecodeFocused: %v", err)
	}
	if ref.ID != "3811157401" || ref.Host != "gnome" {
		t.Errorf("ref = %+v", ref)
	}
}

// Nothing focused is a normal desktop state, so it must not error: the survey
// still has to render.
func TestDecodeFocusedEmpty(t *testing.T) {
	for _, raw := range []string{"", "null", "[]", "  \n"} {
		ref, err := (&gnome.Host{}).DecodeFocused([]byte(raw))
		if err != nil {
			t.Errorf("DecodeFocused(%q) errored: %v", raw, err)
		}
		if !ref.IsZero() {
			t.Errorf("DecodeFocused(%q) = %+v, want zero", raw, ref)
		}
	}
}

func TestDecodeFocusedArrayForm(t *testing.T) {
	ref, err := (&gnome.Host{}).DecodeFocused([]byte(`[{"id":7,"title":"x"}]`))
	if err != nil {
		t.Fatalf("DecodeFocused: %v", err)
	}
	if ref.ID != "7" {
		t.Errorf("ref = %+v, want id 7", ref)
	}
}

// Place builds the command the extension expects. --settled is the part worth
// pinning: without it the request is made before the compositor has placed the
// window, and the compositor's own placement overwrites it.
func TestPlaceBuildsTheCommand(t *testing.T) {
	w := &wctl{}
	err := w.host().Place(context.Background(),
		revier.TargetRef{Host: "gnome", ID: "4181121382"},
		[]string{"right", "top", "75%", "100%"})
	if err != nil {
		t.Fatalf("Place: %v", err)
	}
	if want := []string{"place 4181121382 right top 75% 100% --settled"}; !slices.Equal(w.calls, want) {
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
