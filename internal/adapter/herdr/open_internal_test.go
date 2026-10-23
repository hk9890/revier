// The parts of Open that need no server: the layout it hands herdr, which
// values herdr keeps as written, and the version check. No herdr and no
// process, so the default layer runs it.
package herdr

import (
	"encoding/json"
	"testing"

	"github.com/hk9890/revier/pkg/revier"
)

// Three panels are side by side in equal widths: the first split gives its
// first pane a third, the second split half of what is left.
func TestLayoutPutsThePanelsSideBySideInEqualWidths(t *testing.T) {
	got, err := json.Marshal(layout([]revier.PanelSpec{
		{Title: "agent", Command: []string{"claude"}, Dir: "/p"},
		{Dir: "/p"},
		{Command: []string{"lazygit"}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"type":"split","direction":"right","ratio":0.3333333333333333,` +
		`"first":{"type":"pane","label":"agent","cwd":"/p","command":["claude"]},` +
		`"second":{"type":"split","direction":"right","ratio":0.5,` +
		`"first":{"type":"pane","cwd":"/p"},"second":{"type":"pane","command":["lazygit"]}}}`
	if string(got) != want {
		t.Errorf("layout =\n%s\nwant\n%s", got, want)
	}
}

// The pane ids herdr answers a layout with come back left to right, the
// order of the panels.
func TestPanesOfALayoutAreLeftToRight(t *testing.T) {
	var n layoutNode
	answer := `{"type":"split","first":{"type":"pane","pane_id":"w1:p2"},` +
		`"second":{"type":"split","first":{"type":"pane","pane_id":"w1:p3"},"second":{"type":"pane","pane_id":"w1:p4"}}}`
	if err := json.Unmarshal([]byte(answer), &n); err != nil {
		t.Fatal(err)
	}
	if got := n.panes(); len(got) != 3 || got[0] != "w1:p2" || got[1] != "w1:p3" || got[2] != "w1:p4" {
		t.Errorf("panes = %q, want w1:p2 w1:p3 w1:p4", got)
	}
}

// herdr trims a token, takes out control characters and cuts it at 80
// characters, so only a value it keeps as written may be one.
func TestOnlyAValueHerdrKeepsAsWrittenIsAToken(t *testing.T) {
	long := ""
	for range 81 {
		long += "ü"
	}
	for v, want := range map[string]bool{
		"home":            true,
		"demo|pipe":       true,
		long[:2*80]:       true,
		long:              false,
		"":                false,
		" home":           false,
		"demo|pipe\nline": false,
		"tab\there":       false,
	} {
		if got := survives(v); got != want {
			t.Errorf("survives(%q) = %v, want %v", v, got, want)
		}
	}
	for k, want := range map[string]bool{"revier_target": true, "a-b": true, "": false, "a.b": false, "ü": false} {
		if got := validKey(k); got != want {
			t.Errorf("validKey(%q) = %v, want %v", k, got, want)
		}
	}
}

// Each part counts by the digits it starts with, so a pre-release of a newer
// herdr is not taken for an older one.
func TestOlderComparesEachPartAsANumber(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"0.9.4", "0.9.5", true},
		{"0.9.5", "0.9.5", false},
		{"0.10.0", "0.9.5", false},
		{"1.0", "0.9.5", false},
		{"0.9", "0.9.5", true},
		{"0.9.6-dev", "0.9.5", false},
		{"0.9.4-rc1", "0.9.5", true},
	} {
		if got := older(c.a, c.b); got != c.want {
			t.Errorf("older(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}
