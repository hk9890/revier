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

// The rules are usable only where the loaded extension reads every key revier
// writes: a file with a key it does not read is refused whole, the user's
// rules with it.
func TestRulesNeedAnExtensionThatReadsEveryKeyRevierWrites(t *testing.T) {
	all := `["rules","rules-overlay","rules-min-width"]`
	cases := []struct {
		name   string
		doc    string
		usable bool
	}{
		{"all three", `{"wctl":"0.23.0","extension":"23","capabilities":` + all + `}`, true},
		{"no min_width", `{"wctl":"0.23.0","extension":"21","capabilities":["rules","rules-overlay"]}`, false},
		{"no extension", `{"wctl":"0.23.0","extension":null,"capabilities":[],"message":"not running"}`, false},
		{"an old wctl", `{"wctl":"0.22.0","extension":"23","capabilities":` + all + `}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := &wctl{reply: versionReply(tc.doc, 0)}
			err := w.rules().Probe(context.Background())
			if (err == nil) != tc.usable {
				t.Errorf("Probe = %v, want usable %v", err, tc.usable)
			}
		})
	}
}

// Add puts the match first and --at last, and leaves out what the rule does
// not have; Remove names the index.
func TestTheWriterAsksWctlForTheRuleAsWritten(t *testing.T) {
	w := &wctl{}
	r := w.rules()
	ctx := context.Background()
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
// user's.
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
		{Class: "revier-popup", Place: []string{"center", "top", "1800", "100%"}, Overlay: true},
		{Class: "revier-popup", Overlay: true, Other: true},
		{Class: "code", Other: true},
		{Class: "kitty", Other: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("rules =\n%+v\nwant\n%+v", got, want)
	}
}
