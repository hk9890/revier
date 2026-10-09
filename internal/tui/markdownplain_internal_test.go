package tui

import (
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/theme"
)

// An agent quotes what a tool printed, and a tool prints escapes: a cleared
// screen, a moved cursor, a colour, a word to the terminal emulator. plainText
// takes every one of them off, because the pane styles the message itself and
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

// sgrOnly is a colour or a weight, which is all lipgloss writes and so all
// the pane's own lines may carry.
var sgrOnly = regexp.MustCompile("^\x1b\\[[0-9;]*m")

// Whatever the message held, every escape left in the pane's lines is one the
// pane wrote.
func TestMarkdownLetsNoEscapeThroughFromTheMessage(t *testing.T) {
	text := "Fixed it \x1b[2J\x1b[H\n\n```\n\x1b[31m-gone\x1b]0;owned\x07\n```\n\n- \ba note\r"
	for _, line := range markdown(text, 40, theme.Default()) {
		for at := strings.IndexByte(line, 0x1b); at >= 0; at = strings.IndexByte(line, 0x1b) {
			if !sgrOnly.MatchString(line[at:]) {
				t.Errorf("line carries %q from the message, not a colour", line[at:])
			}
			line = line[at+1:]
		}
		if strings.ContainsAny(line, "\a\b\r\u009b") {
			t.Errorf("line %q carries a control character from the message", line)
		}
	}
}

// A code block and a table are blocks, and a block stands off the paragraph
// before it: an agent writes "Fixed it:" and then the diff, and the two read
// as one paragraph without the line between.
func TestMarkdownSetsACodeBlockAndATableOffFromTheTextAboveIt(t *testing.T) {
	for _, tc := range []struct {
		name, text string
	}{
		{"a code block", "Fixed it:\n```go\nx := 1\n```"},
		{"a table", "The layers:\n| layer | tag |\n|---|---|\n| L1 | - |"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := plain(tc.text, 40)
			if len(got) < 3 || got[1] != "" {
				t.Errorf("markdown = %q, want a blank line under the paragraph", got)
			}
		})
	}
}

// A list item whose marker and indent fill the column has no room to wrap in.
// It is cut on the right, as a code block is: a line that vanishes reads as a
// message that never held it.
func TestMarkdownKeepsAListItemTooDeeplyIndentedToWrap(t *testing.T) {
	got := plain(strings.Repeat(" ", 12)+"- the deepest point", 14)
	if len(got) != 1 || !strings.Contains(got[0], "•") {
		t.Errorf("markdown = %q, want the item kept and cut", got)
	}
	if slices.Contains(got, "") {
		t.Errorf("markdown = %q, want no empty line in place of the item", got)
	}
}
