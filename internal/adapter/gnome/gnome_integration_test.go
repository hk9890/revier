//go:build integration

// Layer L3: the wctl parsers, against recorded output. No GNOME session, no
// wctl binary, no display. The fixtures are hand-written to the shape wctl
// 0.7.0 emits; real captured output carries the user's window titles and does
// not belong in a repository. version.json is captured: it names no window.
package gnome_test

import (
	"context"
	"slices"
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

func TestDecodeFocused(t *testing.T) {
	ref, err := (&gnome.Host{}).DecodeFocused(read(t, "focused.json"))
	if err != nil {
		t.Fatalf("DecodeFocused: %v", err)
	}
	if ref.ID != "3811157401" || ref.Host != "gnome" {
		t.Errorf("ref = %+v", ref)
	}
}

// The version document of wctl 0.16.0, recorded: the host takes it as usable,
// and reads that the extension minimizes and activates without the animation.
func TestProbeReadsTheRecordedVersion(t *testing.T) {
	onGNOME(t)
	w := &wctl{reply: versionReply(string(read(t, "version.json")), 0)}
	h := w.host()
	if err := h.Probe(context.Background()); err != nil {
		t.Fatalf("Probe: %v", err)
	}
	w.calls = nil
	ref := revier.TargetRef{Host: "gnome", ID: "1"}
	if err := h.Hide(context.Background(), ref); err != nil {
		t.Fatalf("Hide: %v", err)
	}
	if err := h.Focus(context.Background(), ref); err != nil {
		t.Fatalf("Focus: %v", err)
	}
	if want := []string{"minimize 1 --no-animation", "activate 1 --no-animation"}; !slices.Equal(w.calls, want) {
		t.Errorf("calls = %q, want %q", w.calls, want)
	}
}
