package testenv_test

import (
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/testenv"
)

// A name fits a socket path: letters and digits alone, at most 40 of them.
func TestAShortNameHoldsLettersAndDigitsAlone(t *testing.T) {
	t.Run("a sub/test, with 0.1", func(t *testing.T) {
		if got, want := testenv.ShortName(t), "AShortNameHoldsLettersAndDigitsA"; !strings.HasPrefix(got, want) || len(got) != 40 {
			t.Errorf("ShortName = %q, want %q and a hash, 40 in all", got, want)
		}
	})
	t.Run("x", func(t *testing.T) {
		if got, want := testenv.ShortName(t), "AShortNameHoldsLettersAndDigitsAlonex"; got != want {
			t.Errorf("ShortName = %q, want %q", got, want)
		}
	})
}

// Two subtests whose names share their first 32 letters and digits get two
// names: they would otherwise share a socket.
func TestTwoLongNamesThatShareTheirStartDiffer(t *testing.T) {
	var names []string
	for _, sub := range []string{"one", "two"} {
		t.Run(sub, func(t *testing.T) { names = append(names, testenv.ShortName(t)) })
	}
	if names[0] == names[1] {
		t.Errorf("both subtests are named %q", names[0])
	}
}
