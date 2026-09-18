// The commands the GNOME writer builds, against a recorder. No GNOME session,
// no dconf and no recorded output, so the default layer runs it: what is
// asserted is the argv, because that is the whole of what this type
// contributes.
package gnome_test

import (
	"context"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/adapter/gnome"
	"github.com/hk9890/revier/pkg/revier"
)

// writerRecorder answers a `gsettings get` of the enabled list from the
// fixture and records every command.
type writerRecorder struct {
	calls []string
	list  string
}

func (r *writerRecorder) run(_ context.Context, bin string, args ...string) ([]byte, error) {
	r.calls = append(r.calls, bin+" "+strings.Join(args, " "))
	if args[0] == "get" {
		return []byte(r.list), nil
	}
	return nil, nil
}

func newWriter(list string) (*gnome.Writer, *writerRecorder) {
	rec := &writerRecorder{list: list}
	w := &gnome.Writer{}
	w.SetRunner(rec.run)
	return w, rec
}

func ran(t *testing.T, rec *writerRecorder, want string) {
	t.Helper()
	for _, c := range rec.calls {
		if c == want {
			return
		}
	}
	t.Errorf("no call %q in:\n  %s", want, strings.Join(rec.calls, "\n  "))
}

func neverRan(t *testing.T, rec *writerRecorder, prefix string) {
	t.Helper()
	for _, c := range rec.calls {
		if strings.HasPrefix(c, prefix) {
			t.Errorf("ran %q, which starts with %q", c, prefix)
		}
	}
}

const base = "/org/gnome/settings-daemon/plugins/media-keys/custom-keybindings/"

func TestBindWritesTheEntryThenListsIt(t *testing.T) {
	w, rec := newWriter("['" + base + "custom0/']")
	err := w.Bind(context.Background(), revier.Binding{
		ID:      "revier-home",
		Chord:   "<Shift><Control>u",
		Command: `sh -lc "revier go home --picker"`,
		Label:   "revier: home",
	})
	if err != nil {
		t.Fatalf("Bind: %v", err)
	}

	ran(t, rec, `dconf write `+base+`revier-home/name 'revier: home'`)
	ran(t, rec, `dconf write `+base+`revier-home/binding '<Shift><Control>u'`)
	ran(t, rec, `dconf write `+base+`revier-home/command 'sh -lc "revier go home --picker"'`)
	// The entry is written before its path joins the list. A path in the list
	// whose entry is not there yet is a shortcut with no command.
	ran(t, rec, `gsettings set org.gnome.settings-daemon.plugins.media-keys custom-keybindings `+
		`['`+base+`custom0/', '`+base+`revier-home/']`)

	last := rec.calls[len(rec.calls)-1]
	if !strings.HasPrefix(last, "gsettings set") {
		t.Errorf("the last call is %q, want the list written last", last)
	}
}

// A second Bind of the same shortcut writes the entry again and leaves the
// list alone, because the path is already in it.
func TestBindDoesNotAddAPathTwice(t *testing.T) {
	w, rec := newWriter("['" + base + "revier-home/']")
	if err := w.Bind(context.Background(), revier.Binding{
		ID: "revier-home", Chord: "<Shift><Control>u", Command: "x", Label: "revier: home",
	}); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	neverRan(t, rec, "gsettings set")
}

// Switching off a custom shortcut takes its path out of the list and touches
// nothing else. Every word of the entry stays, so whatever wrote it puts it
// back.
func TestDisableTakesThePathOutAndKeepsTheEntry(t *testing.T) {
	w, rec := newWriter("['" + base + "custom0/', '" + base + "custom1/']")
	if err := w.Disable(context.Background(), revier.Binding{
		ID: "custom0", Where: base + "custom0/", Source: revier.BindingCustom,
	}); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	ran(t, rec, `gsettings set org.gnome.settings-daemon.plugins.media-keys custom-keybindings `+
		`['`+base+`custom1/']`)
	neverRan(t, rec, "dconf reset")
	neverRan(t, rec, "dconf write")
}

// A desktop default is a setting, not an entry, so the key comes out of the
// setting rather than an entry out of the list. It holding that key alone,
// the setting is left empty.
func TestDisableEmptiesADesktopDefault(t *testing.T) {
	w, rec := newWriter("['<Alt>space']")
	if err := w.Disable(context.Background(), revier.Binding{
		Chord: "<Alt>space", Where: "org.gnome.desktop.wm.keybindings", Label: "activate-window-menu",
		Source: revier.BindingBuiltin,
	}); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	ran(t, rec, "gsettings set org.gnome.desktop.wm.keybindings activate-window-menu @as []")
}

// A setting can hold several keys, and only the one revier needs comes out:
// emptied whole, `close` would lose Alt+F4 to a target that asked for Super+Q.
func TestDisableKeepsTheOtherKeysOfADesktopDefault(t *testing.T) {
	w, rec := newWriter("['<Alt>F4', '<Super>q']")
	if err := w.Disable(context.Background(), revier.Binding{
		Chord: "<Super>q", Where: "org.gnome.desktop.wm.keybindings", Label: "close",
		Source: revier.BindingBuiltin,
	}); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	ran(t, rec, "gsettings set org.gnome.desktop.wm.keybindings close ['<Alt>F4']")
	neverRan(t, rec, "gsettings set org.gnome.desktop.wm.keybindings close @as []")
}

func TestRemoveUnlistsThenDeletes(t *testing.T) {
	w, rec := newWriter("['" + base + "revier-home/']")
	if err := w.Remove(context.Background(), revier.Binding{
		ID: "revier-home", Where: base + "revier-home/", Source: revier.BindingCustom,
	}); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	ran(t, rec, "gsettings set org.gnome.settings-daemon.plugins.media-keys custom-keybindings @as []")
	ran(t, rec, "dconf reset -f "+base+"revier-home/")
}

// A desktop default has no entry to delete. Removing one would mean resetting
// a GNOME schema, which is not what any caller asked for.
func TestRemoveRefusesADesktopDefault(t *testing.T) {
	w, rec := newWriter("@as []")
	err := w.Remove(context.Background(), revier.Binding{
		Where: "org.gnome.desktop.wm.keybindings", Label: "close", Source: revier.BindingBuiltin,
	})
	if err == nil {
		t.Fatal("Remove accepted a desktop setting")
	}
	if len(rec.calls) != 0 {
		t.Errorf("it ran %v", rec.calls)
	}
}

// A command carrying a quote must reach dconf as text and not as syntax.
func TestAQuoteInACommandIsEscaped(t *testing.T) {
	w, rec := newWriter("@as []")
	if err := w.Bind(context.Background(), revier.Binding{
		ID: "revier-home", Chord: "<Alt>space", Label: "revier: home",
		Command: `sh -lc "echo it's here"`,
	}); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	ran(t, rec, `dconf write `+base+`revier-home/command 'sh -lc "echo it\'s here"'`)
}

// The writer runs writes; it must still not run the ones nobody asked for.
// `dconf load` rewrites a whole tree from a file, which no operation here
// needs and which would replace configuration this type does not own.
func TestTheWriterStillRefusesWhatItDoesNotNeed(t *testing.T) {
	for _, tc := range []struct {
		bin  string
		args []string
	}{
		{"dconf", []string{"load", "/org/gnome/"}},
		{"gsettings", []string{"reset-recursively", "org.gnome.desktop.wm.keybindings"}},
		{"/usr/bin/rm", []string{"-rf", "/"}},
	} {
		if err := gnome.WriterAllows(tc.bin, tc.args); err == nil {
			t.Errorf("the writer allows %q %v", tc.bin, tc.args)
		}
	}
	for _, tc := range []struct {
		bin  string
		args []string
	}{
		{"gsettings", []string{"set", "a", "b", "c"}},
		{"dconf", []string{"write", "/a", "'b'"}},
		{"dconf", []string{"reset", "-f", "/a/"}},
		{"gsettings", []string{"get", "a", "b"}},
	} {
		if err := gnome.WriterAllows(tc.bin, tc.args); err != nil {
			t.Errorf("the writer refuses %q %v: %v", tc.bin, tc.args, err)
		}
	}
}

// The reader is what `revier keys status` is given, and it must stay unable to
// write whatever the writer beside it can do.
func TestTheReaderStillCannotWrite(t *testing.T) {
	for _, tc := range []struct {
		bin  string
		args []string
	}{
		{"gsettings", []string{"set", "a", "b", "c"}},
		{"dconf", []string{"write", "/a", "'b'"}},
		{"dconf", []string{"reset", "-f", "/a/"}},
	} {
		if err := gnome.ReadOnly(tc.bin, tc.args); err == nil {
			t.Errorf("the reader allows %q %v", tc.bin, tc.args)
		}
	}
}

// GNOME writes a custom-keybindings path with a trailing slash and accepts it
// without one, and the reader compares it away. A shortcut listed without one
// would otherwise read as switched on and be impossible to switch off, so
// force would report a key taken that still runs somebody else's command.
func TestAPathListedWithoutItsTrailingSlashIsStillUnlisted(t *testing.T) {
	w, rec := newWriter("['" + base + "custom0', '" + base + "custom1/']")
	if err := w.Disable(context.Background(), revier.Binding{
		ID: "custom0", Where: base + "custom0/", Source: revier.BindingCustom,
	}); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	ran(t, rec, `gsettings set org.gnome.settings-daemon.plugins.media-keys custom-keybindings `+
		`['`+base+`custom1/']`)
}
