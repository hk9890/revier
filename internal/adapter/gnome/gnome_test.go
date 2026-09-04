//go:build integration

// Layer L3: the wctl parsers, against recorded output. No GNOME session, no
// wctl binary, no display. The fixtures are hand-written to the shape wctl
// 0.7.0 emits; real captured output carries the user's window titles and does
// not belong in a repository.
package gnome_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/hk9890/revier/internal/adapter/gnome"
	"github.com/hk9890/revier/pkg/revier"
)

func read(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return b
}

func TestDecodeList(t *testing.T) {
	got, err := (&gnome.Host{}).Decode(read(t, "list.json"))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	// The hidden window is dropped: it cannot be activated, so matching it
	// would produce a keypress that appears to do nothing.
	if len(got) != 3 {
		t.Fatalf("got %d instances, want 3 (the hidden one dropped)", len(got))
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
	instances, err := (&gnome.Host{}).Decode(read(t, "list.json"))
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
	dir := t.TempDir()
	log := filepath.Join(dir, "argv")
	stub := filepath.Join(dir, "wctl")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" > " + log + "\n"
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	h := &gnome.Host{Bin: stub}
	err := h.Place(context.Background(),
		revier.TargetRef{Host: "gnome", ID: "4181121382"},
		[]string{"right", "top", "75%", "100%"})
	if err != nil {
		t.Fatalf("Place: %v", err)
	}

	got, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	want := "place 4181121382 right top 75% 100% --settled\n"
	if string(got) != want {
		t.Errorf("argv = %q, want %q", got, want)
	}
}

// A zero ref and a short geometry are refused before the process starts, so a
// broken project file cannot reach the compositor.
func TestPlaceRefusesWhatItCannotSend(t *testing.T) {
	h := &gnome.Host{Bin: "/nonexistent"}
	if err := h.Place(context.Background(), revier.TargetRef{}, []string{"a", "b", "c", "d"}); err == nil {
		t.Error("a zero ref should be refused")
	}
	if err := h.Place(context.Background(), revier.TargetRef{ID: "1"}, []string{"a"}); err == nil {
		t.Error("a geometry short of four tokens should be refused")
	}
}
