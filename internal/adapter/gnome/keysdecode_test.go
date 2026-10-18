// The GNOME keybinding parser, against output written inline. No GNOME
// session, no gsettings binary and no recorded output, so the default layer
// runs it.
package gnome_test

import (
	"testing"

	"github.com/hk9890/revier/internal/adapter/gnome"
)

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
