package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// Every press is folded to the key a legacy terminal sends for it, which is
// the key the surface was built on. Each case is a press as bubbletea reports
// it in a terminal that speaks the kitty keyboard protocol or xterm's
// modifyOtherKeys, and the press the surface reads, or none.
func TestAPressIsFoldedToTheLegacyKey(t *testing.T) {
	const (
		ctrl  = tea.ModCtrl
		alt   = tea.ModAlt
		shift = tea.ModShift
	)
	press := func(code rune, mod tea.KeyMod) tea.KeyPressMsg { return tea.KeyPressMsg{Code: code, Mod: mod} }
	text := func(code rune, s string) tea.KeyPressMsg { return tea.KeyPressMsg{Code: code, Text: s} }
	capital := func(r rune) tea.KeyPressMsg {
		return tea.KeyPressMsg{Code: r + 'a' - 'A', ShiftedCode: r, Mod: shift, Text: string(r)}
	}
	none := tea.KeyPressMsg{}

	for _, tc := range []struct {
		name string
		in   tea.KeyPressMsg
		want tea.KeyPressMsg
	}{
		{"letter", text('a', "a"), text('a', "a")},
		{"capital", capital('A'), capital('A')},
		{"capital-from-modifyotherkeys", tea.KeyPressMsg{Code: 'A', Mod: shift, Text: "A"}, tea.KeyPressMsg{Code: 'A', Mod: shift, Text: "A"}},
		{"capital-with-capslock", tea.KeyPressMsg{Code: 'a', Mod: shift | tea.ModCapsLock}, capital('A')},
		{"letter-with-capslock", tea.KeyPressMsg{Code: 'a', Mod: tea.ModCapsLock, Text: "A"}, text('a', "A")},
		{"digit", text('1', "1"), text('1', "1")},
		{"symbol", text('?', "?"), text('?', "?")},
		{"grapheme", tea.KeyPressMsg{Code: tea.KeyExtended, Text: "é"}, tea.KeyPressMsg{Code: tea.KeyExtended, Text: "é"}},

		{"ctrl-letter", press('u', ctrl), press('u', ctrl)},
		{"ctrl-shift-letter", press('u', ctrl|shift), press('u', ctrl)},
		{"ctrl-shift-capital", press('U', ctrl|shift), press('u', ctrl)},
		{"ctrl-alt-letter", press('o', ctrl|alt), press('o', ctrl|alt)},
		{"ctrl-alt-shift-letter", press('o', ctrl|alt|shift), press('o', ctrl|alt)},
		{"ctrl-letter-with-numlock", press('u', ctrl|tea.ModNumLock), press('u', ctrl)},
		{"ctrl-i", press('i', ctrl), press(tea.KeyTab, 0)},
		{"ctrl-shift-i", press('i', ctrl|shift), press(tea.KeyTab, 0)},
		{"ctrl-alt-i", press('i', ctrl|alt), press(tea.KeyTab, alt)},
		{"ctrl-m", press('m', ctrl), press(tea.KeyEnter, 0)},
		{"ctrl-shift-m", press('m', ctrl|shift), press(tea.KeyEnter, 0)},
		{"ctrl-h", press('h', ctrl), press('h', ctrl)},
		{"ctrl-j", press('j', ctrl), press('j', ctrl)},

		{"alt-letter", press('e', alt), press('e', alt)},
		{"alt-shift-letter", press('o', alt|shift), press('o', alt|shift)},
		{"alt-shift-letter-with-shifted-code", tea.KeyPressMsg{Code: 'o', ShiftedCode: 'O', Mod: alt | shift}, press('o', alt|shift)},
		{"alt-digit", press('1', alt), press('1', alt)},
		{"alt-shift-digit", press('1', alt|shift), press('!', alt)},
		{"alt-shift-digit-with-shifted-code", tea.KeyPressMsg{Code: '1', ShiftedCode: '!', Mod: alt | shift}, press('!', alt)},
		{"alt-shift-symbol-from-modifyotherkeys", press('!', alt|shift), press('!', alt)},
		{"alt-bracket", press('[', alt), press('[', alt)},

		{"ctrl-bracket", press('[', ctrl), press(tea.KeyEscape, 0)},
		{"ctrl-alt-bracket", press('[', ctrl|alt), press(tea.KeyEscape, alt)},
		{"ctrl-at", press('@', ctrl), press('@', ctrl)},
		{"ctrl-2", press('2', ctrl), press('@', ctrl)},
		{"ctrl-shift-2", press('2', ctrl|shift), press('@', ctrl)},
		{"ctrl-3", press('3', ctrl), press(tea.KeyEscape, 0)},
		{"ctrl-backslash", press('\\', ctrl), press('\\', ctrl)},
		{"ctrl-4", press('4', ctrl), press('\\', ctrl)},
		{"ctrl-close-bracket", press(']', ctrl), press(']', ctrl)},
		{"ctrl-5", press('5', ctrl), press(']', ctrl)},
		{"ctrl-caret", press('^', ctrl), press('^', ctrl)},
		{"ctrl-6", press('6', ctrl), press('^', ctrl)},
		{"ctrl-underscore", press('_', ctrl), press('_', ctrl)},
		{"ctrl-7", press('7', ctrl), press('_', ctrl)},
		{"ctrl-slash", press('/', ctrl), press('_', ctrl)},
		{"ctrl-question", press('?', ctrl), press(tea.KeyBackspace, 0)},
		{"ctrl-8", press('8', ctrl), press(tea.KeyBackspace, 0)},
		{"ctrl-1", press('1', ctrl), none},
		{"ctrl-comma", press(',', ctrl), none},

		{"space", tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}, tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}},
		{"shift-space", press(tea.KeySpace, shift), tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}},
		{"ctrl-space", press(tea.KeySpace, ctrl), press('@', ctrl)},
		{"ctrl-shift-space", press(tea.KeySpace, ctrl|shift), press('@', ctrl)},
		{"alt-space", press(tea.KeySpace, alt), press(tea.KeySpace, alt)},
		{"alt-shift-space", press(tea.KeySpace, alt|shift), press(tea.KeySpace, alt)},
		{"ctrl-alt-space", press(tea.KeySpace, ctrl|alt), press('@', ctrl|alt)},

		{"enter", press(tea.KeyEnter, 0), press(tea.KeyEnter, 0)},
		{"shift-enter", press(tea.KeyEnter, shift), press(tea.KeyEnter, 0)},
		{"ctrl-enter", press(tea.KeyEnter, ctrl), press(tea.KeyEnter, 0)},
		{"alt-enter", press(tea.KeyEnter, alt), press(tea.KeyEnter, alt)},
		{"ctrl-alt-enter", press(tea.KeyEnter, ctrl|alt), press(tea.KeyEnter, alt)},
		{"tab", press(tea.KeyTab, 0), press(tea.KeyTab, 0)},
		{"shift-tab", press(tea.KeyTab, shift), press(tea.KeyTab, shift)},
		{"ctrl-tab", press(tea.KeyTab, ctrl), press(tea.KeyTab, 0)},
		{"ctrl-shift-tab", press(tea.KeyTab, ctrl|shift), press(tea.KeyTab, shift)},
		{"alt-tab", press(tea.KeyTab, alt), press(tea.KeyTab, alt)},
		{"alt-shift-tab", press(tea.KeyTab, alt|shift), press(tea.KeyTab, alt|shift)},
		{"esc", press(tea.KeyEscape, 0), press(tea.KeyEscape, 0)},
		{"shift-esc", press(tea.KeyEscape, shift), press(tea.KeyEscape, 0)},
		{"ctrl-esc", press(tea.KeyEscape, ctrl), press(tea.KeyEscape, 0)},
		{"alt-esc", press(tea.KeyEscape, alt), press(tea.KeyEscape, alt)},
		{"backspace", press(tea.KeyBackspace, 0), press(tea.KeyBackspace, 0)},
		{"shift-backspace", press(tea.KeyBackspace, shift), press(tea.KeyBackspace, 0)},
		{"ctrl-backspace", press(tea.KeyBackspace, ctrl), press('h', ctrl)},
		{"alt-backspace", press(tea.KeyBackspace, alt), press(tea.KeyBackspace, alt)},
		{"ctrl-alt-backspace", press(tea.KeyBackspace, ctrl|alt), press('h', ctrl|alt)},

		{"up", press(tea.KeyUp, 0), press(tea.KeyUp, 0)},
		{"shift-up", press(tea.KeyUp, shift), press(tea.KeyUp, shift)},
		{"ctrl-alt-shift-left", press(tea.KeyLeft, ctrl|alt|shift), press(tea.KeyLeft, ctrl|alt|shift)},
		{"alt-right", press(tea.KeyRight, alt), press(tea.KeyRight, alt)},
		{"ctrl-down", press(tea.KeyDown, ctrl), press(tea.KeyDown, ctrl)},
		{"home", press(tea.KeyHome, 0), press(tea.KeyHome, 0)},
		{"ctrl-shift-end", press(tea.KeyEnd, ctrl|shift), press(tea.KeyEnd, ctrl|shift)},
		{"pgup", press(tea.KeyPgUp, 0), press(tea.KeyPgUp, 0)},
		{"ctrl-pgdown", press(tea.KeyPgDown, ctrl), press(tea.KeyPgDown, ctrl)},
		{"ctrl-alt-pgup", press(tea.KeyPgUp, ctrl|alt), press(tea.KeyPgUp, ctrl|alt)},
		{"shift-pgup", press(tea.KeyPgUp, shift), none},
		{"insert", press(tea.KeyInsert, 0), press(tea.KeyInsert, 0)},
		{"alt-insert", press(tea.KeyInsert, alt), none},
		{"delete", press(tea.KeyDelete, 0), press(tea.KeyDelete, 0)},
		{"alt-delete", press(tea.KeyDelete, alt), press(tea.KeyDelete, alt)},
		{"shift-delete", press(tea.KeyDelete, shift), press(tea.KeyInsert, alt)},
		{"ctrl-delete", press(tea.KeyDelete, ctrl), none},
		{"begin", press(tea.KeyBegin, 0), none},

		{"f1", press(tea.KeyF1, 0), press(tea.KeyF1, 0)},
		{"alt-f1", press(tea.KeyF1, alt), press(tea.KeyF1, alt)},
		{"shift-f1", press(tea.KeyF1, shift), press(tea.KeyF13, 0)},
		{"shift-f4", press(tea.KeyF4, shift), press(tea.KeyF16, 0)},
		{"shift-f5", press(tea.KeyF5, shift), press(tea.KeyF17, 0)},
		{"shift-f8", press(tea.KeyF8, shift), press(tea.KeyF20, 0)},
		{"shift-f9", press(tea.KeyF9, shift), none},
		{"ctrl-f5", press(tea.KeyF5, ctrl), none},
		{"alt-f12", press(tea.KeyF12, alt), press(tea.KeyF12, alt)},
		{"f13", press(tea.KeyF13, 0), press(tea.KeyF13, 0)},
		{"alt-f16", press(tea.KeyF16, alt), press(tea.KeyF16, alt)},
		{"alt-f17", press(tea.KeyF17, alt), none},
		{"f21", press(tea.KeyF21, 0), none},

		{"keypad-enter", press(tea.KeyKpEnter, 0), press(tea.KeyEnter, 0)},
		{"keypad-digit", tea.KeyPressMsg{Code: tea.KeyKp1, Mod: tea.ModNumLock, Text: "1"}, text('1', "1")},
		{"keypad-plus", tea.KeyPressMsg{Code: tea.KeyKpPlus, Text: "+"}, text('+', "+")},
		{"keypad-up", press(tea.KeyKpUp, 0), press(tea.KeyUp, 0)},
		{"keypad-delete", press(tea.KeyKpDelete, 0), press(tea.KeyDelete, 0)},
		{"keypad-begin", press(tea.KeyKpBegin, 0), none},
		{"ctrl-keypad-digit", press(tea.KeyKp1, ctrl), none},

		{"super-letter", press('a', tea.ModSuper), press('a', tea.ModSuper)},
		{"media-key", press(tea.KeyMediaPlay, 0), none},
	} {
		got, ok := legacyKey(tc.in)
		if !ok {
			got = none
		}
		if got != tc.want || ok != (tc.want != none) {
			t.Errorf("%s: legacyKey(%q) = %q (%#v), %v; want %q (%#v)", tc.name, tc.in, got, got, ok, tc.want, tc.want)
		}
	}
}
