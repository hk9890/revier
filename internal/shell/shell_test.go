package shell_test

import (
	"testing"

	"github.com/hk9890/revier/internal/shell"
)

// A shell reads a word with a space, a quote or a shell character back as
// one word only once it is quoted, and a plain word needs no quotes.
func TestQuoteKeepsAWordWhole(t *testing.T) {
	cases := map[string]string{
		"demo":           "demo",
		"--json":         "--json",
		"run the tests":  "'run the tests'",
		"it's":           `'it'\''s'`,
		"a;rm -rf /":     "'a;rm -rf /'",
		"$HOME":          "'$HOME'",
		"":               "''",
		"demo:home":      "demo:home",
		"C#":             "'C#'",
		"x=y":            "x=y",
		"=echo":          "'=echo'",
		"~/dev":          "'~/dev'",
		"say \"hi\" now": `'say "hi" now'`,
		"back\\slash":    `'back'\\'slash'`,
		"trail\\":        `'trail'\\''`,
		"caret^":         "'caret^'",
		"user@host":      "user@host",
		"a\nb":           "'a\nb'",
		"glob*":          "'glob*'",
		"bang!":          "'bang!'",
		"back`tick":      "'back`tick'",
	}
	for in, want := range cases {
		if got := shell.Quote(in); got != want {
			t.Errorf("quote(%q) = %s, want %s", in, got, want)
		}
	}
}
