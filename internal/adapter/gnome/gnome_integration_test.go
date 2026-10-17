//go:build integration

// Layer L3: the wctl parsers, against recorded output. No GNOME session, no
// wctl binary, no display. The fixtures are hand-written to the shape wctl
// 0.7.0 emits; real captured output carries the user's window titles and does
// not belong in a repository.
package gnome_test

import (
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/adapter/gnome"
	"github.com/hk9890/revier/pkg/revier"
)

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
	for _, raw := range []string{"", "null", "[]", "  \n", "No window focused\n"} {
		ref, err := (&gnome.Host{}).DecodeFocused([]byte(raw))
		if err != nil {
			t.Errorf("DecodeFocused(%q) errored: %v", raw, err)
		}
		if !ref.IsZero() {
			t.Errorf("DecodeFocused(%q) = %+v, want zero", raw, ref)
		}
	}
}

// Only the text wctl prints for no focus is a normal state: any other output
// that is not JSON is a wctl that misbehaved.
func TestDecodeFocusedRejectsOtherText(t *testing.T) {
	if _, err := (&gnome.Host{}).DecodeFocused([]byte("Error: extension not enabled\n")); err == nil {
		t.Error("DecodeFocused took text that is not JSON for a window")
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
