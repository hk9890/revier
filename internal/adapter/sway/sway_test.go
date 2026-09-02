//go:build integration

// Layer L3: the swaymsg parsers against recorded output. The tree fixture is
// the shape sway 1.9 emits, reduced to the fields the host reads.
package sway_test

import (
	"os"
	"testing"

	"github.com/hk9890/revier/internal/adapter/sway"
	"github.com/hk9890/revier/pkg/revier"
)

func read(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestDecodeTreeListsWindowsOnly(t *testing.T) {
	got, err := sway.Decode(read(t, "get_tree.json"))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	// Four windows: the kitty, the XWayland chromium, the code window, and
	// the floating popup. Root, output, workspace, and the split are not.
	if len(got) != 4 {
		t.Fatalf("got %d instances, want 4:\n%+v", len(got), got)
	}
	if got[0].Ref.ID != "4" || got[0].Title != "session:revier" || got[0].Class != "kitty" || got[0].PID != 4100 {
		t.Errorf("first = %+v", got[0])
	}
	if got[1].Class != "revier-revier-pulls" {
		t.Errorf("XWayland class = %q, want window_properties.class", got[1].Class)
	}
	if got[3].Class != "revier-popup" {
		t.Errorf("floating window = %+v", got[3])
	}
}

// The same match rules serve Wayland and XWayland windows.
func TestDecodedInstancesMatchRealizations(t *testing.T) {
	instances, _ := sway.Decode(read(t, "get_tree.json"))
	cases := []struct {
		match revier.Match
		want  string
	}{
		{revier.Match{Class: "^kitty$", Title: "^session:revier$"}, "4"},
		{revier.Match{Class: "^revier-revier-pulls$"}, "6"},
		{revier.Match{Class: "^code$", Title: "revier"}, "7"},
	}
	for _, tc := range cases {
		m, err := tc.match.Compile()
		if err != nil {
			t.Fatal(err)
		}
		found := ""
		for _, inst := range instances {
			if m.Matches(inst) {
				found = inst.Ref.ID
				break
			}
		}
		if found != tc.want {
			t.Errorf("%+v matched %q, want %q", tc.match, found, tc.want)
		}
	}
}

func TestDecodeEvents(t *testing.T) {
	got, err := sway.DecodeEvents(read(t, "events.jsonl"))
	if err != nil {
		t.Fatalf("DecodeEvents: %v", err)
	}
	// title changes are not window events revier reports.
	if len(got) != 3 {
		t.Fatalf("got %d events, want new, focus, close", len(got))
	}
	if got[0].Kind != revier.WindowOpened || got[0].Instance.Class != "meld" || got[0].Instance.Ref.ID != "9" {
		t.Errorf("first = %+v", got[0])
	}
	if got[1].Kind != revier.WindowFocused || got[2].Kind != revier.WindowClosed {
		t.Errorf("kinds = %v %v", got[1].Kind, got[2].Kind)
	}
}
