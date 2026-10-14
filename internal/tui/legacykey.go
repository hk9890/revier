package tui

import (
	"unicode"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"

	"github.com/hk9890/revier/internal/core"
)

// keyPress is what reaches the screens from the keyboard: a press, folded by
// legacyKey, or text pasted. A paste takes the path a press takes, so it lands
// in the field a typed character would land in, and no key matches it: its
// press is the zero one, which no binding names.
type keyPress struct {
	tea.KeyPressMsg
	pasted bool
	paste  string
	exact  core.Chord // the chord as pressed, where a target key binds it and it is not the folded one
}

// pressedKey is a press as it reaches the screens.
func pressedKey(k tea.KeyPressMsg) keyPress { return keyPress{KeyPressMsg: k} }

// pastedText is a paste as it reaches the screens.
func pastedText(text string) keyPress { return keyPress{pasted: true, paste: text} }

// input is the message a text field takes for the press.
func (k keyPress) input() tea.Msg {
	if k.pasted {
		return tea.PasteMsg{Content: k.paste}
	}
	return k.KeyPressMsg
}

// runes reports text: a paste, or a key that types a character other than a
// space, with alt held or not.
func (k keyPress) runes() bool {
	if k.pasted {
		return true
	}
	if k.Code == tea.KeySpace || k.Mod&^(tea.ModAlt|tea.ModShift) != 0 {
		return false
	}
	return k.Text != "" || k.Mod.Contains(tea.ModAlt) && (unicode.IsPrint(k.Code) || k.Code == tea.KeyExtended)
}

// space reports the space bar, with alt held or not.
func (k keyPress) space() bool {
	return !k.pasted && k.Code == tea.KeySpace && k.Mod&^tea.ModAlt == 0
}

// bare reports the key, with alt held or no modifier. A legacy terminal sends
// alt as an escape in front of the key's own sequence, so the key is read the
// same with it.
func (k keyPress) bare(code rune) bool {
	return !k.pasted && k.Code == code && k.Mod&^tea.ModAlt == 0
}

// shiftTab reports shift+tab, with alt held or not.
func (k keyPress) shiftTab() bool {
	return !k.pasted && k.Code == tea.KeyTab && k.Mod.Contains(tea.ModShift)
}

// consoleF is the start of F1 to F5 on the Linux console: ESC [ [ and then a
// letter from A to E. bubbletea does not know the sequence. It reads the start
// as an unknown sequence and the letter as a press of its own.
const consoleF = uv.UnknownCsiEvent("\x1b[[")

// consoleKey is the press of the letter after consoleF: F1 to F5 for A to E,
// which no key of the surface names, and any other press unchanged.
func consoleKey(k tea.KeyPressMsg) tea.KeyPressMsg {
	if len(k.Text) != 1 || k.Text[0] < 'A' || k.Text[0] > 'E' {
		return k
	}
	return pressOf(tea.KeyF1+rune(k.Text[0]-'A'), 0)
}

// legacyKey is a press as a terminal without the kitty keyboard protocol and
// without xterm's modifyOtherKeys sends it, which is the key the surface
// reads. bubbletea asks every terminal for both and cannot be told not to,
// and a terminal that speaks either reports a chord that a legacy terminal
// folds: ctrl+shift+u as itself and not as ctrl+u, ctrl+i as itself and not
// as tab, the keypad's enter as a key of its own. Every key of the surface,
// every target key that core.Chord.Terminal folds and every query editing key
// was chosen against the folded keys, so each press is folded back before any
// of them is matched. Only a target key that binds the press as it was made
// comes first, where the terminal reports key disambiguation (Model.unfolded).
//
// The second return is false for a press whose legacy sequence the surface
// reads as no key at all, such as ctrl+delete: that press does nothing.
func legacyKey(k tea.KeyPressMsg) (tea.KeyPressMsg, bool) {
	mod := k.Mod &^ (tea.ModCapsLock | tea.ModNumLock | tea.ModScrollLock)
	k.Mod = mod
	if mod&^(tea.ModCtrl|tea.ModAlt|tea.ModShift) != 0 {
		// Super, hyper and meta reach no binding.
		return k, true
	}
	code := k.Code
	if main, ok := keypadKeys[code]; ok {
		code = main
	}
	ctrl, shift := mod.Contains(tea.ModCtrl), mod.Contains(tea.ModShift)
	alt := mod & tea.ModAlt
	switch {
	case code == tea.KeySpace:
		switch {
		case ctrl:
			return pressOf('@', tea.ModCtrl|alt), true
		case alt != 0:
			return pressOf(tea.KeySpace, alt), true
		}
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}, true
	case code == tea.KeyEnter, code == tea.KeyEscape:
		return pressOf(code, alt), true
	case code == tea.KeyTab:
		if shift {
			return pressOf(tea.KeyTab, tea.ModShift|alt), true
		}
		return pressOf(tea.KeyTab, alt), true
	case code == tea.KeyBackspace:
		if ctrl {
			return pressOf('h', tea.ModCtrl|alt), true
		}
		return pressOf(tea.KeyBackspace, alt), true
	case isASCIILetter(code):
		return letterKey(k, unicode.ToLower(code), ctrl, shift, alt), true
	case ctrl && code < unicode.MaxASCII:
		sent, ok := ctrlSymbols[code]
		if !ok {
			return tea.KeyPressMsg{}, false
		}
		return pressOf(sent.Code, sent.Mod|alt), true
	case unicode.IsPrint(code) || code == tea.KeyExtended:
		return printableKey(k, code, shift, alt), true
	}
	return specialKey(code, mod)
}

// letterKey folds a letter. With ctrl, a legacy terminal sends a control
// byte, which has no shifted form, and the bytes of ctrl+i and ctrl+m are
// those of tab and enter.
func letterKey(k tea.KeyPressMsg, lower rune, ctrl, shift bool, alt tea.KeyMod) tea.KeyPressMsg {
	switch {
	case ctrl && lower == 'i':
		return pressOf(tea.KeyTab, alt)
	case ctrl && lower == 'm':
		return pressOf(tea.KeyEnter, alt)
	case ctrl:
		return pressOf(lower, tea.ModCtrl|alt)
	case alt != 0 && shift:
		return pressOf(lower, alt|tea.ModShift)
	case alt != 0:
		return pressOf(lower, alt)
	case k.Text != "":
		return k
	case shift:
		upper := unicode.ToUpper(lower)
		return tea.KeyPressMsg{Code: lower, ShiftedCode: upper, Mod: tea.ModShift, Text: string(upper)}
	}
	return tea.KeyPressMsg{Code: lower, Text: string(lower)}
}

// printableKey folds a key that types a character other than a letter. With
// alt, a legacy terminal sends escape and then the character the key types,
// so a shift is in the character and is no modifier. A report that does not
// say which character the shifted key types is read on a US layout.
func printableKey(k tea.KeyPressMsg, code rune, shift bool, alt tea.KeyMod) tea.KeyPressMsg {
	if alt == 0 {
		if k.Code == code && k.Text != "" {
			return k
		}
		return tea.KeyPressMsg{Code: code, Text: string(code)}
	}
	if shift {
		switch {
		case k.ShiftedCode != 0:
			code = k.ShiftedCode
		case usShifted[code] != 0:
			code = usShifted[code]
		}
	}
	return pressOf(code, alt)
}

// specialKey folds a key that a legacy terminal sends as an escape sequence,
// with each modifier held as a parameter of it. legacySequences holds every
// combination the surface reads a key from.
func specialKey(code rune, mod tea.KeyMod) (tea.KeyPressMsg, bool) {
	read, ok := legacySequences[code][mod]
	return read, ok
}

func pressOf(code rune, mod tea.KeyMod) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: code, Mod: mod}
}

func isASCIILetter(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

// keypadKeys is each keypad key as the main key a legacy terminal sends for
// it.
var keypadKeys = map[rune]rune{
	tea.KeyKpEnter: tea.KeyEnter, tea.KeyKpEqual: '=', tea.KeyKpMultiply: '*',
	tea.KeyKpPlus: '+', tea.KeyKpComma: ',', tea.KeyKpMinus: '-',
	tea.KeyKpDecimal: '.', tea.KeyKpDivide: '/', tea.KeyKpSep: ',',
	tea.KeyKp0: '0', tea.KeyKp1: '1', tea.KeyKp2: '2', tea.KeyKp3: '3', tea.KeyKp4: '4',
	tea.KeyKp5: '5', tea.KeyKp6: '6', tea.KeyKp7: '7', tea.KeyKp8: '8', tea.KeyKp9: '9',
	tea.KeyKpUp: tea.KeyUp, tea.KeyKpDown: tea.KeyDown, tea.KeyKpLeft: tea.KeyLeft,
	tea.KeyKpRight: tea.KeyRight, tea.KeyKpPgUp: tea.KeyPgUp, tea.KeyKpPgDown: tea.KeyPgDown,
	tea.KeyKpHome: tea.KeyHome, tea.KeyKpEnd: tea.KeyEnd, tea.KeyKpInsert: tea.KeyInsert,
	tea.KeyKpDelete: tea.KeyDelete, tea.KeyKpBegin: tea.KeyBegin,
}

// ctrlSymbols is what a legacy terminal sends for ctrl and a key that is no
// letter: one of the five control bytes past ctrl+z, or the byte of escape or
// of backspace. Every other one sends nothing the surface reads.
var ctrlSymbols = map[rune]tea.KeyPressMsg{
	'@': pressOf('@', tea.ModCtrl), '2': pressOf('@', tea.ModCtrl),
	'[': pressOf(tea.KeyEscape, 0), '3': pressOf(tea.KeyEscape, 0),
	'\\': pressOf('\\', tea.ModCtrl), '4': pressOf('\\', tea.ModCtrl),
	']': pressOf(']', tea.ModCtrl), '5': pressOf(']', tea.ModCtrl),
	'^': pressOf('^', tea.ModCtrl), '6': pressOf('^', tea.ModCtrl),
	'_': pressOf('_', tea.ModCtrl), '7': pressOf('_', tea.ModCtrl), '/': pressOf('_', tea.ModCtrl),
	'?': pressOf(tea.KeyBackspace, 0), '8': pressOf(tea.KeyBackspace, 0),
}

// usShifted is the character each key types with shift on a US layout.
var usShifted = map[rune]rune{
	'`': '~', '1': '!', '2': '@', '3': '#', '4': '$', '5': '%', '6': '^', '7': '&',
	'8': '*', '9': '(', '0': ')', '-': '_', '=': '+', '[': '{', ']': '}', '\\': '|',
	';': ':', '\'': '"', ',': '<', '.': '>', '/': '?',
}

// legacySequences is, for each key a legacy terminal sends as an escape
// sequence, the key the surface reads from that sequence with each set of
// modifiers it reads one from. Two are another key's: a legacy terminal's
// shift+f1 is the sequence of f13, and its shift+delete is read as
// alt+insert.
var legacySequences = func() map[rune]map[tea.KeyMod]tea.KeyPressMsg {
	const (
		s = tea.ModShift
		a = tea.ModAlt
		c = tea.ModCtrl
	)
	same := func(code rune, mods ...tea.KeyMod) map[tea.KeyMod]tea.KeyPressMsg {
		out := map[tea.KeyMod]tea.KeyPressMsg{0: pressOf(code, 0)}
		for _, mod := range mods {
			out[mod] = pressOf(code, mod)
		}
		return out
	}
	every := []tea.KeyMod{s, a, s | a, c, c | s, c | a, c | a | s}
	out := map[rune]map[tea.KeyMod]tea.KeyPressMsg{
		tea.KeyUp: same(tea.KeyUp, every...), tea.KeyDown: same(tea.KeyDown, every...),
		tea.KeyLeft: same(tea.KeyLeft, every...), tea.KeyRight: same(tea.KeyRight, every...),
		tea.KeyHome: same(tea.KeyHome, every...), tea.KeyEnd: same(tea.KeyEnd, every...),
		tea.KeyPgUp: same(tea.KeyPgUp, a, c, c|a), tea.KeyPgDown: same(tea.KeyPgDown, a, c, c|a),
		tea.KeyInsert: same(tea.KeyInsert),
		tea.KeyDelete: same(tea.KeyDelete, a),
	}
	out[tea.KeyDelete][s] = pressOf(tea.KeyInsert, a)
	for i, f := range []rune{tea.KeyF1, tea.KeyF2, tea.KeyF3, tea.KeyF4, tea.KeyF5, tea.KeyF6,
		tea.KeyF7, tea.KeyF8, tea.KeyF9, tea.KeyF10, tea.KeyF11, tea.KeyF12} {
		out[f] = same(f, a)
		if i < 8 {
			out[f][s] = pressOf(tea.KeyF13+rune(i), 0)
		}
	}
	for i, f := range []rune{tea.KeyF13, tea.KeyF14, tea.KeyF15, tea.KeyF16, tea.KeyF17,
		tea.KeyF18, tea.KeyF19, tea.KeyF20} {
		if i < 4 {
			out[f] = same(f, a)
		} else {
			out[f] = same(f)
		}
	}
	return out
}()
