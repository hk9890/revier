//go:build integration

// Layer L3: the GNOME keybinding parsers, against recorded gsettings and dconf
// output. No GNOME session, no display. The fixtures carry the four shortcuts
// this machine really has, written in the shape the two tools emit.
package gnome_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/adapter/gnome"
	"github.com/hk9890/revier/pkg/revier"
)

// recorder answers from the fixtures and remembers every command it was asked
// to run.
type recorder struct{ calls []string }

func (r *recorder) run(t *testing.T) func(context.Context, string, ...string) ([]byte, error) {
	return func(_ context.Context, bin string, args ...string) ([]byte, error) {
		r.calls = append(r.calls, bin+" "+strings.Join(args, " "))
		switch {
		case bin == "dconf" && args[0] == "dump":
			return read(t, "custom-keybindings.dconf"), nil
		case args[0] == "get":
			return read(t, "custom-keybindings.list"), nil
		case args[0] == "list-recursively" && args[1] == "org.gnome.desktop.wm.keybindings":
			return read(t, "wm-keybindings.txt"), nil
		case args[0] == "list-recursively" && args[1] == "org.gnome.settings-daemon.plugins.media-keys":
			return read(t, "media-keys.txt"), nil
		}
		// Every other schema is one this GNOME does not ship.
		return nil, errors.New("no such schema")
	}
}

func binding(t *testing.T, in []revier.Binding, chord string) revier.Binding {
	t.Helper()
	for _, b := range in {
		if b.Chord == chord {
			return b
		}
	}
	t.Fatalf("no binding for %q in %+v", chord, in)
	return revier.Binding{}
}

func TestDecodeCustomReadsTheTreeAndTheList(t *testing.T) {
	got := gnome.DecodeCustom(read(t, "custom-keybindings.dconf"), read(t, "custom-keybindings.list"))
	if len(got) != 5 {
		t.Fatalf("got %d bindings, want 5: %+v", len(got), got)
	}

	one := binding(t, got, "<Shift><Control>u")
	if one.Label != "to-session-terminal" {
		t.Errorf("label = %q, want to-session-terminal", one.Label)
	}
	if one.Command != `sh -lc "$HOME/setup/scripts/sessions/os-to-session.sh"` {
		t.Errorf("command = %q, want the quoting stripped and nothing else", one.Command)
	}
	if !one.Enabled || one.Source != revier.BindingCustom {
		t.Errorf("binding = %+v, want an enabled custom entry", one)
	}
	if !strings.HasSuffix(one.Where, "/custom1/") {
		t.Errorf("where = %q, want the dconf path", one.Where)
	}

	// The entry in the tree that the list does not name. It is stored, shown
	// nowhere, and fires on no press.
	orphan := binding(t, got, "<Shift><Control>y")
	if orphan.Enabled {
		t.Errorf("%+v: an entry the list does not name is not enabled", orphan)
	}
}

// A command holding quotes of its own must survive the unquoting, or it never
// compares equal to the command revier would install.
func TestACommandWithNestedQuotesSurvives(t *testing.T) {
	got := gnome.DecodeCustom(read(t, "custom-keybindings.dconf"), read(t, "custom-keybindings.list"))
	popup := binding(t, got, "<Alt>space")
	want := `sh -lc "$HOME/setup/scripts/sessions/gnome-run.sh "$HOME/setup/scripts/sessions/os-fzf-popup.sh""`
	if popup.Command != want {
		t.Errorf("command =\n  %q\nwant\n  %q", popup.Command, want)
	}
}

// A schema holds shortcuts and ordinary settings side by side. Only the values
// shaped like a list of accelerators are shortcuts, which is what keeps a list
// of key names out of this adapter.
func TestDecodeBuiltinTakesOnlyTheAccelerators(t *testing.T) {
	got := gnome.DecodeBuiltin(read(t, "wm-keybindings.txt"))
	if len(got) != 4 {
		t.Fatalf("got %d bindings, want 4: %+v", len(got), got)
	}
	// close has two accelerators, and both hold the chord.
	for _, chord := range []string{"<Alt>F4", "<Super>q"} {
		b := binding(t, got, chord)
		if b.Label != "close" || b.Where != "org.gnome.desktop.wm.keybindings" {
			t.Errorf("%s = %+v, want the schema and the key", chord, b)
		}
		if b.Source != revier.BindingBuiltin {
			t.Errorf("%s source = %q, want builtin", chord, b.Source)
		}
	}
	// An empty setting holds nothing, and an empty list is written @as [].
	for _, b := range got {
		if b.Label == "activate-window-menu" || b.Label == "workspace-names" {
			t.Errorf("%+v: an empty setting is not a shortcut", b)
		}
	}
}

// The shape check is on the start of the value. A string setting that happens
// to hold brackets is not an array, and reading one as a list of accelerators
// would report chords nobody bound.
func TestASettingThatMerelyContainsBracketsIsNotAShortcut(t *testing.T) {
	raw := []byte(`org.gnome.shell.keybindings some-label 'a[b]c'
org.gnome.shell.keybindings some-count uint32 4
org.gnome.shell.keybindings toggle-overview ['<Super>s']
`)
	got := gnome.DecodeBuiltin(raw)
	if len(got) != 1 {
		t.Fatalf("got %d bindings, want only the real shortcut: %+v", len(got), got)
	}
	if got[0].Chord != "<Super>s" {
		t.Errorf("binding = %+v, want the toggle-overview accelerator", got[0])
	}
}

func TestListReadsBothToolsAndCombinesThem(t *testing.T) {
	rec := &recorder{}
	k := &gnome.Keys{}
	k.SetRunner(rec.run(t))

	got, err := k.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if b := binding(t, got, "<Shift><Control>o"); b.Source != revier.BindingCustom {
		t.Errorf("custom shortcut = %+v", b)
	}
	if b := binding(t, got, "<Super>Up"); b.Source != revier.BindingBuiltin {
		t.Errorf("desktop shortcut = %+v", b)
	}
}

// The guard itself, not only what List happens to ask for today. Both tools
// write, and the write subcommand is one character away from the read.
func TestTheGuardRefusesEveryWrite(t *testing.T) {
	for _, tc := range []struct {
		bin  string
		args []string
	}{
		{"gsettings", []string{"set", "org.gnome.settings-daemon.plugins.media-keys", "custom-keybindings", "[]"}},
		{"gsettings", []string{"reset-recursively", "org.gnome.desktop.wm.keybindings"}},
		{"dconf", []string{"write", "/org/gnome/x", "'y'"}},
		{"dconf", []string{"load", "/org/gnome/x/"}},
		{"dconf", []string{"reset", "-f", "/org/gnome/x/"}},
		{"/usr/bin/rm", []string{"-rf", "/"}},
		{"gsettings", nil},
	} {
		if err := gnome.ReadOnly(tc.bin, tc.args); err == nil {
			t.Errorf("ReadOnly(%q, %v) = nil, want a refusal", tc.bin, tc.args)
		}
	}
	for _, tc := range []struct {
		bin  string
		args []string
	}{
		{"gsettings", []string{"get", "a", "b"}},
		{"/usr/bin/gsettings", []string{"list-recursively", "a"}},
		{"dconf", []string{"dump", "/org/gnome/x/"}},
	} {
		if err := gnome.ReadOnly(tc.bin, tc.args); err != nil {
			t.Errorf("ReadOnly(%q, %v) = %v, want a read to pass", tc.bin, tc.args, err)
		}
	}
}

// The one thing this command may never do. gsettings and dconf both write, and
// a status command that could change a binding is one nobody may run against a
// live desktop.
func TestListRunsNothingThatWrites(t *testing.T) {
	rec := &recorder{}
	k := &gnome.Keys{}
	k.SetRunner(rec.run(t))
	if _, err := k.List(context.Background()); err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rec.calls) == 0 {
		t.Fatal("List ran nothing at all")
	}
	for _, call := range rec.calls {
		fields := strings.Fields(call)
		verb := fields[1]
		switch verb {
		case "get", "list-recursively", "dump", "read", "list":
		default:
			t.Errorf("List ran %q; %q is not a read", call, verb)
		}
	}
}
