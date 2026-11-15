// The rules reader and writer against a recorder in place of wctl: what each
// asks wctl, and what it reads of the answer. No GNOME session and no wctl
// process, so the default layer runs it.
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

// rules is a rules writer that runs w in place of wctl.
func (w *wctl) rules() *gnome.RuleWriter {
	r := &gnome.RuleWriter{}
	r.SetRunner(func(_ context.Context, args ...string) ([]byte, error) {
		joined := strings.Join(args, " ")
		w.calls = append(w.calls, joined)
		if w.reply == nil {
			return nil, nil
		}
		return w.reply(joined)
	})
	return r
}

// Listing and removing need only a wctl that has the rules. Adding needs a
// loaded extension that reads every key revier writes: a file with a key it
// does not read is refused whole, the user's rules with it.
func TestRulesAddOnlyWhereTheExtensionReadsEveryKeyRevierWrites(t *testing.T) {
	all := `["rules","rules-overlay","rules-min-width"]`
	cases := []struct {
		name   string
		doc    string
		usable bool
		adds   bool
	}{
		{"all three", `{"wctl":"0.23.0","extension":"23","capabilities":` + all + `}`, true, true},
		{"no min_width", `{"wctl":"0.23.0","extension":"21","capabilities":["rules","rules-overlay"]}`, true, false},
		{"no extension", `{"wctl":"0.23.0","extension":null,"capabilities":[],"message":"not running"}`, true, false},
		{"an old wctl", `{"wctl":"0.22.0","extension":"23","capabilities":` + all + `}`, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := (&wctl{reply: versionReply(tc.doc, 0)}).rules()
			if err := r.Probe(t.Context()); (err == nil) != tc.usable {
				t.Fatalf("Probe = %v, want usable %v", err, tc.usable)
			}
			if !tc.usable {
				return
			}
			if err := r.CanAdd(); (err == nil) != tc.adds {
				t.Errorf("CanAdd = %v, want adds %v", err, tc.adds)
			}
		})
	}
}

// The host's Rules read what the host's own probe read of the extension, so
// the popup's launch asks wctl for no second version.
func TestTheHostsRulesKnowWhatItsProbeRead(t *testing.T) {
	for _, tc := range []struct {
		caps string
		adds bool
	}{
		{`["rules","rules-overlay","rules-min-width"]`, true},
		{`["rules","rules-overlay"]`, false},
	} {
		onGNOME(t)
		w := &wctl{reply: versionReply(`{"wctl":"0.23.0","extension":"23","capabilities":`+tc.caps+`}`, 0)}
		h := w.host()
		if err := h.Probe(t.Context()); err != nil {
			t.Fatal(err)
		}
		calls := len(w.calls)
		if err := h.Rules().CanAdd(); (err == nil) != tc.adds {
			t.Errorf("%s: CanAdd = %v, want adds %v", tc.caps, err, tc.adds)
		}
		if len(w.calls) != calls {
			t.Errorf("%s: CanAdd ran wctl: %q", tc.caps, w.calls[calls:])
		}
	}
}

// Add puts the match first and --at last, and leaves out what the rule does
// not have; Remove names the index.
func TestTheWriterAsksWctlForTheRuleAsWritten(t *testing.T) {
	w := &wctl{}
	r := w.rules()
	ctx := t.Context()
	if err := r.Add(ctx, 0, revier.WindowRule{Class: "revier-popup", MinWidth: 3400, Place: []string{"center", "top", "1800", "100%"}, Overlay: true}); err != nil {
		t.Fatal(err)
	}
	if err := r.Add(ctx, 1, revier.WindowRule{Class: "revier-popup", Place: []string{"left", "top", "100%", "100%"}}); err != nil {
		t.Fatal(err)
	}
	if err := r.Remove(ctx, 3); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"rules add -c revier-popup --min-width 3400 --overlay place center top 1800 100% --at 0",
		"rules add -c revier-popup place left top 100% 100% --at 1",
		"rules remove 3",
	}
	if !slices.Equal(w.calls, want) {
		t.Errorf("calls = %q\nwant    %q", w.calls, want)
	}
}

// The guard itself: the reader runs list and nothing that writes.
func TestTheRulesReaderCannotWrite(t *testing.T) {
	for _, args := range [][]string{
		{"rules", "add", "-c", "x", "--overlay"},
		{"rules", "remove", "0"},
		{"rules", "apply"},
		{"place", "1", "0", "0", "10", "10"},
		{"rules"},
	} {
		if err := gnome.RulesReadOnly(args); err == nil {
			t.Errorf("RulesReadOnly(%q) = nil, want a refusal", args)
		}
	}
	if err := gnome.RulesReadOnly([]string{"rules", "list", "--json"}); err != nil {
		t.Errorf("RulesReadOnly(list) = %v", err)
	}
}

// A place value may be a number in a file the user wrote, and reads as the
// string revier writes. A key revier does not write marks the rule as the
// user's. Each rule keeps all of what the file held, for the user to put
// back.
func TestDecodeRulesReadsNumbersAndMarksTheRest(t *testing.T) {
	got, err := gnome.DecodeRules([]byte(`[
		{"match":{"class":"revier-popup"},"place":["center","top",1800,"100%"],"overlay":true},
		{"match":{"class":"revier-popup","title":"x"},"overlay":true},
		{"match":{"class":"code"},"tile":"left"},
		{"match":{"class":"kitty"},"place":["center","top","50%"]}
	]`))
	if err != nil {
		t.Fatal(err)
	}
	want := []revier.WindowRule{
		{Class: "revier-popup", Place: []string{"center", "top", "1800", "100%"}, Overlay: true,
			Raw: `{"match":{"class":"revier-popup"},"place":["center","top",1800,"100%"],"overlay":true}`},
		{Class: "revier-popup", Overlay: true, Other: true, Raw: `{"match":{"class":"revier-popup","title":"x"},"overlay":true}`},
		{Class: "code", Other: true, Raw: `{"match":{"class":"code"},"tile":"left"}`},
		{Class: "kitty", Other: true, Raw: `{"match":{"class":"kitty"},"place":["center","top","50%"]}`},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("rules =\n%+v\nwant\n%+v", got, want)
	}
}
