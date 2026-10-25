package config_test

import (
	"reflect"
	"testing"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
)

func termConfig(t *testing.T, text string) *config.Config {
	t.Helper()
	cfg, err := config.Read(writeConfig(t, text))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	return cfg
}

func problemTexts(cfg *config.Config) []string {
	var out []string
	for _, err := range cfg.Problems {
		out = append(out, err.Error())
	}
	return out
}

// With no [terminal] table the shell and the default agent have their keys,
// and a key outside a workspace opens a tab.
func TestTheTerminalKeysHaveDefaults(t *testing.T) {
	cfg := termConfig(t, "")
	want := []core.TermKey{{Chord: "ctrl+shift+t", Shell: true}, {Chord: "ctrl+shift+z"}}
	if got := cfg.TermKeys(); !reflect.DeepEqual(got, want) {
		t.Errorf("TermKeys = %+v, want %+v", got, want)
	}
	if got := cfg.Outside(); got != core.OutsideTab {
		t.Errorf("Outside = %q, want %q", got, core.OutsideTab)
	}
}

// A key written empty is no key, any spelling of a chord is read as the
// canonical one, and a named agent's key comes after the two.
func TestTheTerminalKeysAreRead(t *testing.T) {
	cfg := termConfig(t, `
[terminal]
shell_key = ""
agent_key = "Ctrl-Alt-A"
outside = "none"

[[agent]]
name = "dtop"
command = ["dtop"]
key = "ctrl-shift-d"

[[agent]]
name = "plain"
command = ["plain"]
`)
	if len(cfg.Problems) > 0 {
		t.Fatalf("Problems = %v", cfg.Problems)
	}
	want := []core.TermKey{{Chord: "ctrl+alt+a"}, {Chord: "ctrl+shift+d", Agent: "dtop"}}
	if got := cfg.TermKeys(); !reflect.DeepEqual(got, want) {
		t.Errorf("TermKeys = %+v, want %+v", got, want)
	}
	if got := cfg.Outside(); got != core.OutsideNone {
		t.Errorf("Outside = %q, want %q", got, core.OutsideNone)
	}
}

// A key that does not parse, or that an earlier key holds, is refused alone;
// a refused agent has no key; an unknown outside is the default.
func TestABadTerminalKeyIsRefusedAlone(t *testing.T) {
	cfg := termConfig(t, `
[terminal]
agent_key = "ctrl+shift+t"
outside = "window"

[[agent]]
name = "dtop"
command = ["dtop"]
key = "hyperish+d"

[[agent]]
name = "idle"
key = "ctrl+shift+i"

[[agent]]
name = "ok"
command = ["ok"]
key = "ctrl+shift+o"
`)
	want := []string{
		`agent "idle": has no command`,
		`terminal.outside: "window" is neither "tab" nor "none"`,
		`terminal.agent_key: key "ctrl+shift+t" is held by terminal.shell_key already`,
		`agent "dtop" key: key "hyperish+d": unknown modifier "hyperish"`,
	}
	if got := problemTexts(cfg); !reflect.DeepEqual(got, want) {
		t.Errorf("Problems =\n%q\nwant\n%q", got, want)
	}
	wantKeys := []core.TermKey{{Chord: "ctrl+shift+t", Shell: true}, {Chord: "ctrl+shift+o", Agent: "ok"}}
	if got := cfg.TermKeys(); !reflect.DeepEqual(got, wantKeys) {
		t.Errorf("TermKeys = %+v, want %+v", got, wantKeys)
	}
	if got := cfg.Outside(); got != core.OutsideTab {
		t.Errorf("Outside = %q, want %q", got, core.OutsideTab)
	}
}
