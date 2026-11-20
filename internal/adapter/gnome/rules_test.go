// The rules reader and writer against a recorder in place of wctl: what each
// asks wctl, and what it reads of the answer, with revier's own file in a
// scratch directory. No GNOME session and no wctl process, so the default
// layer runs it.
package gnome_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/adapter/gnome"
	"github.com/hk9890/revier/pkg/revier"
)

const allRuleCaps = `["rules","rules-overlay","rules-min-width","rules-dropin"]`

// Listing the rules and removing an older revier's need any wctl the host
// drives. Writing needs a wctl that knows rules.d, and a loaded extension
// that reads it and every key revier writes: a file it does not read would
// be reported installed and never applied.
func TestRulesWriteOnlyWhereTheExtensionReadsRevierFile(t *testing.T) {
	cases := []struct {
		name   string
		doc    string
		usable bool
		adds   bool
	}{
		{"all four", `{"wctl":"0.24.0","extension":"24","capabilities":` + allRuleCaps + `}`, true, true},
		{"no rules.d", `{"wctl":"0.24.0","extension":"23","capabilities":["rules","rules-overlay","rules-min-width"]}`, true, false},
		{"no min_width", `{"wctl":"0.24.0","extension":"21","capabilities":["rules","rules-overlay","rules-dropin"]}`, true, false},
		{"no extension", `{"wctl":"0.24.0","extension":null,"capabilities":[],"message":"not running"}`, true, false},
		{"a wctl with no rules.d", `{"wctl":"0.23.0","extension":"24","capabilities":` + allRuleCaps + `}`, true, false},
		{"a wctl the host does not drive", `{"wctl":"0.22.0","extension":"24","capabilities":` + allRuleCaps + `}`, false, false},
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
// the popup's launch asks wctl for no second version. The host drives a
// wctl older than the one the rules need, and its Rules then write none.
func TestTheHostsRulesKnowWhatItsProbeRead(t *testing.T) {
	for _, tc := range []struct {
		wctl, caps string
		adds       bool
	}{
		{"0.24.0", allRuleCaps, true},
		{"0.24.0", `["rules","rules-overlay","rules-min-width"]`, false},
		{"0.23.0", allRuleCaps, false},
	} {
		onGNOME(t)
		w := &wctl{reply: versionReply(`{"wctl":"`+tc.wctl+`","extension":"24","capabilities":`+tc.caps+`}`, 0)}
		h := w.host()
		if err := h.Probe(t.Context()); err != nil {
			t.Fatal(err)
		}
		calls := len(w.calls)
		if err := h.Rules().CanAdd(); (err == nil) != tc.adds {
			t.Errorf("%s %s: CanAdd = %v, want adds %v", tc.wctl, tc.caps, err, tc.adds)
		}
		if len(w.calls) != calls {
			t.Errorf("%s: CanAdd ran wctl: %q", tc.caps, w.calls[calls:])
		}
	}
}

// files is a recorder for the rules commands over a scratch config
// directory: path --dir names its rules.d, check --json answers check, and
// list answers from lists, by the file it names, "" for the user's.
func files(t *testing.T, check string, lists map[string]string) (*wctl, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "rules.d")
	return &wctl{reply: func(args string) ([]byte, error) {
		switch {
		case args == "rules path --dir":
			return []byte(dir + "\n"), nil
		case args == "rules check --json":
			return []byte(strings.ReplaceAll(check, "DIR", dir)), nil
		case strings.HasPrefix(args, "rules list --json"):
			file := strings.TrimPrefix(strings.TrimPrefix(args, "rules list --json"), " --file ")
			return []byte(lists[filepath.Base(file)]), nil
		}
		return nil, nil
	}}, dir
}

// A file of revier's that the extension would not load is there, and
// broken; with none, the set names where it would be.
func TestReadTellsABrokenFileOfReviersFromNone(t *testing.T) {
	lists := map[string]string{".": `[]`}
	w, _ := files(t, `{"path":"/c/rules.json","valid":true,"dropins":[{"path":"DIR/revier.json","valid":false,"error":"rules[0].bogus: unknown key"}]}`, lists)
	set, err := w.rules().Read(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !set.OwnExists || set.OwnBroken != "rules[0].bogus: unknown key" || len(set.Own.Rules) != 0 {
		t.Errorf("broken: %+v", set)
	}
	if slices.ContainsFunc(w.calls, func(c string) bool { return strings.Contains(c, "--file") }) {
		t.Errorf("listed a broken file: %q", w.calls)
	}

	w, dir := files(t, `{"path":"/c/rules.json","valid":true}`, lists)
	set, err = w.rules().Read(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if set.OwnExists || set.Own.Path != filepath.Join(dir, "revier.json") || set.User.Path != "/c/rules.json" {
		t.Errorf("none: %+v", set)
	}
}

// A user's file the extension would not load is reported broken, and not
// listed. The extension loads the drop-ins without it, so revier's file is
// still read.
func TestReadReportsABrokenUsersFileAndReadsReviers(t *testing.T) {
	w, _ := files(t, `{"path":"/c/rules.json","valid":false,"error":"/c/rules.json: not valid JSON","dropins":[{"path":"DIR/revier.json","valid":true}]}`,
		map[string]string{"revier.json": `[{"match":{"class":"revier-popup"},"overlay":true}]`})
	set, err := w.rules().Read(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if set.UserBroken != "/c/rules.json: not valid JSON" || set.User.Rules != nil || !set.OwnExists || len(set.Own.Rules) != 1 {
		t.Errorf("set = %+v", set)
	}
	if slices.Contains(w.calls, "rules list --json") {
		t.Errorf("listed the broken file: %q", w.calls)
	}
}

// On a wctl that knows no rules.d, the read lists the user's file alone,
// so that an uninstall still finds an older revier's rules in it.
func TestReadOnAWctlWithNoRulesDListsTheUsersFileAlone(t *testing.T) {
	w := &wctl{reply: func(args string) ([]byte, error) {
		switch args {
		case "version --json":
			return []byte(`{"wctl":"0.23.0","extension":"23","capabilities":["rules","rules-overlay","rules-min-width"]}`), nil
		case "rules path":
			return []byte("/c/rules.json\n"), nil
		case "rules list --json":
			return []byte(`[{"match":{"class":"revier-popup"},"overlay":true}]`), nil
		}
		return nil, refused(args, 2, "unknown option")
	}}
	r := w.rules()
	if err := r.Probe(t.Context()); err != nil {
		t.Fatal(err)
	}
	w.calls = nil
	set, err := r.Read(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if set.User.Path != "/c/rules.json" || len(set.User.Rules) != 1 || set.OwnExists || set.Own.Path != "/c/rules.d/revier.json" {
		t.Errorf("set = %+v", set)
	}
	if want := []string{"rules path", "rules list --json"}; !slices.Equal(w.calls, want) {
		t.Errorf("calls = %q, want %q", w.calls, want)
	}
}

// WriteOwn makes rules.d, writes the file under a name the extension skips,
// has wctl check that file, and renames it into place: nothing else is left
// in rules.d.
func TestWriteOwnChecksTheFileBeforeItIsInPlace(t *testing.T) {
	w, dir := files(t, "", nil)
	rules := []revier.WindowRule{{Class: "revier-popup", MinWidth: 3400, Place: []string{"center", "top", "1800", "100%"}, Overlay: true}}
	if err := w.rules().WriteOwn(t.Context(), rules); err != nil {
		t.Fatal(err)
	}
	tmp := "rules check --file " + filepath.Join(dir, ".revier.json.")
	if len(w.calls) != 2 || w.calls[0] != "rules path --dir" || !strings.HasPrefix(w.calls[1], tmp) {
		t.Errorf("calls = %q\nwant path --dir, then a check of %s*", w.calls, tmp)
	}
	got, err := os.ReadFile(filepath.Join(dir, "revier.json"))
	if err != nil {
		t.Fatal(err)
	}
	if want, _ := gnome.EncodeRules(rules); string(got) != string(want) {
		t.Errorf("file =\n%s\nwant\n%s", got, want)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Errorf("rules.d holds %d entries, want revier.json alone", len(entries))
	}
}

// A file wctl refuses is never put in place, and leaves nothing behind; a
// file of revier's already there stays as it was.
func TestWriteOwnPutsNoRefusedFileInPlace(t *testing.T) {
	w, dir := files(t, "", nil)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	own := filepath.Join(dir, "revier.json")
	if err := os.WriteFile(own, []byte("[]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	inner := w.reply
	w.reply = func(args string) ([]byte, error) {
		if strings.HasPrefix(args, "rules check --file") {
			return nil, refused(args, 1, "Error: rules[0].place: bad")
		}
		return inner(args)
	}
	if err := w.rules().WriteOwn(t.Context(), []revier.WindowRule{{Class: "revier-popup"}}); err == nil {
		t.Fatal("WriteOwn = nil, want the refusal")
	}
	if got, _ := os.ReadFile(own); string(got) != "[]\n" {
		t.Errorf("revier.json = %q, want it as it was", got)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Errorf("rules.d holds %d entries, want revier.json alone", len(entries))
	}
}

// RemoveOwn deletes revier's file, and a file already gone is no error.
// RemoveUser names the index of the user's file.
func TestTheWritersRemoves(t *testing.T) {
	w, dir := files(t, "", nil)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	own := filepath.Join(dir, "revier.json")
	if err := os.WriteFile(own, []byte("[]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := w.rules()
	for range 2 {
		if err := r.RemoveOwn(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(own); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("revier.json still there: %v", err)
	}
	w.calls = nil
	if err := r.RemoveUser(t.Context(), 3); err != nil {
		t.Fatal(err)
	}
	if want := []string{"rules remove 3"}; !slices.Equal(w.calls, want) {
		t.Errorf("calls = %q, want %q", w.calls, want)
	}
}

// The guard itself: the reader runs list, check and path, and nothing that
// writes.
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
	for _, args := range [][]string{
		{"rules", "list", "--json"},
		{"rules", "check", "--json"},
		{"rules", "path", "--dir"},
	} {
		if err := gnome.RulesReadOnly(args); err != nil {
			t.Errorf("RulesReadOnly(%q) = %v", args, err)
		}
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
