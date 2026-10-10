package tui

import (
	"strings"
	"testing"
)

// An agent quotes what a tool printed, and a tool prints escapes: a cleared
// screen, a moved cursor, a colour, a word to the terminal emulator. plainText
// takes every one of them off, because the row styles the message itself and
// a sequence that passes through repaints the surface around it.
func TestPlainTextTakesOffWhatAMessageMustNotCarry(t *testing.T) {
	for _, tc := range []struct {
		name, text, want string
	}{
		{"a screen clear and a cursor home", "before \x1b[2J\x1b[H after", "before  after"},
		{"a colour a diff was captured with", "\x1b[31m-gone\x1b[0m", "-gone"},
		{"a word to the emulator", "title \x1b]0;owned\x07 here", "title  here"},
		{"a bare escape", "before \x1b after", "before  after"},
		{"a carriage return", "first\rsecond", "firstsecond"},
		{"a bell and a backspace", "alarm\ahere\bthere", "alarmherethere"},
		{"a C1 introducer", "before \u009b2J after", "before 2J after"},
		{"the newlines the lines are split on", "one\ntwo", "one\ntwo"},
		{"a tab, as wide as it is drawn", "a\tb", "a" + strings.Repeat(" ", tabWidth) + "b"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := plainText(tc.text); got != tc.want {
				t.Errorf("plainText = %q, want %q", got, tc.want)
			}
		})
	}
}
