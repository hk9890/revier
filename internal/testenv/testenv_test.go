package testenv_test

import (
	"testing"

	"github.com/hk9890/revier/internal/testenv"
)

// A name fits a socket path: the test name's letters and digits, then an
// 8-digit hash.
func TestShortName(t *testing.T) {
	t.Run("a b/c,d.1", func(t *testing.T) {
		got := testenv.ShortName(t)
		if want := "ShortNameabcd1"; len(got) != len(want)+8 || got[:len(want)] != want {
			t.Errorf("ShortName = %q, want %q and a hash", got, want)
		}
	})
	t.Run("a subtest whose name runs on past the first thirty-two letters", func(t *testing.T) {
		if got := testenv.ShortName(t); len(got) != 40 || got[:32] != "ShortNameasubtestwhosenamerunson" {
			t.Errorf("ShortName = %q, want its first 32 letters and a hash, 40 in all", got)
		}
	})
}

// Two tests whose names differ only in what ShortName cuts or drops get two
// names: they would otherwise share a socket.
func TestShortNamesDifferWhereTheirTestNamesDo(t *testing.T) {
	for _, pair := range [][2]string{
		{"x y", "xy"},
		{"a name that shares its first thirty-two letters, one", "a name that shares its first thirty-two letters, two"},
	} {
		var names []string
		for _, sub := range pair {
			t.Run(sub, func(t *testing.T) { names = append(names, testenv.ShortName(t)) })
		}
		if names[0] == names[1] {
			t.Errorf("%q and %q are both named %q", pair[0], pair[1], names[0])
		}
	}
}
