// The wctl parsers, against output written inline. No GNOME session, no wctl
// binary and no recorded output, so the default layer runs it.
package gnome_test

import (
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/adapter/gnome"
)

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
