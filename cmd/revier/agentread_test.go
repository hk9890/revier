package main

import (
	"slices"
	"testing"
)

// A panel's text is printed as text: the colours, a hyperlink and a control
// character a tool printed are gone, with the padding of each row and the
// empty rows under the last line.
func TestScreenLinesAreThePanelsTextAndNothingElse(t *testing.T) {
	screen := "\x1b[1;32m● done\x1b[0m   \n" +
		"\x1b]8;;https://example.com\x1b\\a link\x1b]8;;\x1b\\\n" +
		"\n" +
		"a\ttab and a bell\a\n" +
		"   \n\n"
	want := []string{"● done", "a link", "", "a\ttab and a bell"}
	if got := screenLines(screen, 0); !slices.Equal(got, want) {
		t.Errorf("lines = %q, want %q", got, want)
	}
	if got := screenLines(screen, 2); !slices.Equal(got, want[2:]) {
		t.Errorf("the last 2 lines = %q, want %q", got, want[2:])
	}
	if got := screenLines(screen, 9); !slices.Equal(got, want) {
		t.Errorf("the last 9 lines = %q, want all %d", got, len(want))
	}
}
