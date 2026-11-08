package plain_test

import (
	"testing"

	"github.com/hk9890/revier/internal/plain"
)

// Every escape sequence comes off with its payload, whole or cut short, and
// so does every control character but the newline and the tab. A bare escape
// starts a sequence, so it takes the character after it too.
func TestTextTakesOffEveryEscapeAndControl(t *testing.T) {
	for _, tc := range []struct {
		name, text, want string
	}{
		{"a CSI colour", "a\x1b[31mb\x1b[0mc", "abc"},
		{"a CSI screen clear and cursor home", "a\x1b[2J\x1b[Hb", "ab"},
		{"a CSI private mode", "a\x1b[?25lb", "ab"},
		{"a CSI cut short", "a\x1b[31", "a"},
		{"an OSC ended by a bell", "a\x1b]0;title\x07b", "ab"},
		{"an OSC 8 link ended by ST", "a\x1b]8;;u\x1b\\link\x1b]8;;\x1b\\b", "alinkb"},
		{"an OSC cut short", "a\x1b]0;title", "a"},
		{"a DCS with its payload", "a\x1bPq#0;2;0;0;0\x1b\\b", "ab"},
		{"a DCS cut short", "a\x1bPq#0", "a"},
		{"an SOS", "a\x1bXsos\x1b\\b", "ab"},
		{"a PM", "a\x1b^pm\x1b\\b", "ab"},
		{"an APC graphics payload", "a\x1b_Gf=24;AAAA\x1b\\b", "ab"},
		{"an ESC charset", "a\x1b(Bb", "ab"},
		{"an ESC Fp cursor save and restore", "a\x1b7b\x1b8c", "abc"},
		{"an ESC Fs reset", "a\x1bcb", "ab"},
		{"a bare escape", "before \x1b after", "before fter"},
		{"a bare escape at the end", "before \x1b", "before "},
		{"a bell and a backspace", "a\ab\bc", "abc"},
		{"a carriage return", "a\rb", "ab"},
		{"a CRLF", "a\r\nb", "a\nb"},
		{"a DEL", "a\x7fb", "ab"},
		{"a C1 CSI, whose parameters stay as text", "a\u009b31mb", "a31mb"},
		{"a C1 OSC and ST", "a\u009d0;t\u009cb", "a0;tb"},
		{"a C1 next line", "a\u0085b", "ab"},
		{"an invalid byte", "a\x9bb", "a"},
		{"a tab", "a\tb", "a\tb"},
		{"a newline", "a\nb", "a\nb"},
		{"UTF-8 text", "héllo 世界 🙂", "héllo 世界 🙂"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := plain.Text(tc.text); got != tc.want {
				t.Errorf("Text(%q) = %q, want %q", tc.text, got, tc.want)
			}
		})
	}
}
