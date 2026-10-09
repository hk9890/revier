// The guard between the key reader and the write subcommands of gsettings and
// dconf. A pure function, so the default layer runs it.
package gnome_test

import (
	"testing"

	"github.com/hk9890/revier/internal/adapter/gnome"
)

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
