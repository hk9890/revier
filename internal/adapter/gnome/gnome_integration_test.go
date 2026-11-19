//go:build integration

// Layer L3: the wctl parsers, against recorded output. No GNOME session, no
// wctl binary, no display. The fixtures are hand-written to the shape wctl
// 0.7.0 emits; real captured output carries the user's window titles and does
// not belong in a repository. version.json is captured: it names no window.
package gnome_test

import (
	"context"
	"reflect"
	"slices"
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

func TestDecodeFocused(t *testing.T) {
	ref, err := (&gnome.Host{}).DecodeFocused(read(t, "focused.json"))
	if err != nil {
		t.Fatalf("DecodeFocused: %v", err)
	}
	if ref.ID != "3811157401" || ref.Host != "gnome" {
		t.Errorf("ref = %+v", ref)
	}
}

// The listing of wctl 0.23.0, recorded from a scratch file: revier's two
// rules, a rule that tiles, and the overlay rule the guide had users write by
// hand. Each keeps all of what wctl printed of it.
func TestDecodeRulesReadsTheRecordedListing(t *testing.T) {
	got, err := gnome.DecodeRules(read(t, "rules.json"))
	if err != nil {
		t.Fatal(err)
	}
	want := []revier.WindowRule{
		{Class: "revier-popup", MinWidth: 3400, Place: []string{"center", "top", "1800", "100%"}, Overlay: true,
			Raw: `{"match":{"class":"revier-popup","min_width":3400},"place":["center","top","1800","100%"],"overlay":true}`},
		{Class: "revier-popup", Place: []string{"left", "top", "100%", "100%"}, Overlay: true,
			Raw: `{"match":{"class":"revier-popup"},"place":["left","top","100%","100%"],"overlay":true}`},
		{Class: "code", Other: true, Raw: `{"match":{"class":"code"},"tile":"left"}`},
		{Class: "revier-popup", Overlay: true, Raw: `{"match":{"class":"revier-popup"},"overlay":true}`},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("rules =\n%+v\nwant\n%+v", got, want)
	}
}

// popupTiers are the three rules revier writes, as the core builds them.
var popupTiers = []revier.WindowRule{
	{Class: "revier-popup", MinWidth: 3400, Place: []string{"center", "top", "1800", "100%"}, Overlay: true},
	{Class: "revier-popup", MinWidth: 2200, Place: []string{"center", "top", "75%", "100%"}, Overlay: true},
	{Class: "revier-popup", Place: []string{"left", "top", "100%", "100%"}, Overlay: true},
}

// revier writes its file byte for byte as wctl 0.24.0 writes the same three
// rules with `wctl rules add --file`, recorded in revier.json: a repeat
// install that did write would change no byte.
func TestRevierFileIsWhatWctlWrites(t *testing.T) {
	got, err := gnome.EncodeRules(popupTiers)
	if err != nil {
		t.Fatal(err)
	}
	if want := read(t, "revier.json"); string(got) != string(want) {
		t.Errorf("file =\n%s\nwant\n%s", got, want)
	}
}

// The check of wctl 0.24.0, recorded over a rules.json, a drop-in of another
// program's before revier's, a broken one, revier's, and one after it. check
// exits 1 for the broken file. The read takes the user's file, the earlier
// file that loads, and revier's, and lists no other.
func TestReadTakesTheRecordedCheck(t *testing.T) {
	const dir = "/home/you/.config/gnome-window-control/rules.d"
	lists := map[string]string{
		"":                        `[{"match":{"class":"code"},"tile":"left"}]`,
		dir + "/0-other.json":     `[{"match":{"class":"revier-popup"},"tile":"left"}]`,
		dir + "/" + "revier.json": string(read(t, "revier.json")),
	}
	w := &wctl{reply: func(args string) ([]byte, error) {
		switch {
		case args == "rules path --dir":
			return []byte(dir + "\n"), nil
		case args == "rules check --json":
			return read(t, "rules-check.json"), refused(args, 1, "")
		case strings.HasPrefix(args, "rules list --json"):
			list, ok := lists[strings.TrimPrefix(strings.TrimPrefix(args, "rules list --json"), " --file ")]
			if !ok {
				t.Errorf("listed %q", args)
			}
			return []byte(list), nil
		}
		return nil, nil
	}}
	set, err := w.rules().Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := revier.WindowRuleSet{
		User: revier.WindowRuleFile{Path: "/home/you/.config/gnome-window-control/rules.json", Rules: []revier.WindowRule{
			{Class: "code", Other: true, Raw: `{"match":{"class":"code"},"tile":"left"}`}}},
		Earlier: []revier.WindowRuleFile{{Path: dir + "/0-other.json", Rules: []revier.WindowRule{
			{Class: "revier-popup", Other: true, Raw: `{"match":{"class":"revier-popup"},"tile":"left"}`}}}},
		Own:       revier.WindowRuleFile{Path: dir + "/revier.json"},
		OwnExists: true,
	}
	for i := range set.Own.Rules {
		set.Own.Rules[i].Raw = ""
	}
	if !reflect.DeepEqual(set.Own.Rules, popupTiers) {
		t.Errorf("own = %+v, want the three tiers", set.Own.Rules)
	}
	set.Own.Rules = nil
	if !reflect.DeepEqual(set, want) {
		t.Errorf("set =\n%+v\nwant\n%+v", set, want)
	}
}

// The version document of wctl 0.24.0, recorded: the extension reads
// rules.d and every key revier writes, so revier may write its file.
func TestRulesCanWriteOnTheRecordedVersion(t *testing.T) {
	r := (&wctl{reply: versionReply(string(read(t, "version-0.24.json")), 0)}).rules()
	if err := r.Probe(context.Background()); err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if err := r.CanAdd(); err != nil {
		t.Errorf("CanAdd: %v", err)
	}
}

// The version document of wctl 0.23.0, recorded: the host takes it as usable,
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
