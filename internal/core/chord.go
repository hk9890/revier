package core

import (
	"fmt"
	"regexp"
	"strings"
)

// One chord already has three spellings in this repository:
//
//	ctrl-shift-u        a target's key in a project file
//	ctrl+shift+u        what bubbletea reports, internal/tui/tui.go keyName
//	<Shift><Control>u   what GNOME stores
//
// Comparing them is the whole of "does revier hold this key", so there is one
// canonical form and a parser that accepts every spelling. Canonical is the
// bubbletea one, because the TUI already prints it and a user reading
// `revier keys status` should see the same text the footer shows.
type Chord string

// modifiers, in the order a canonical chord lists them.
var modOrder = []string{"ctrl", "alt", "shift", "super"}

// modAlias maps every spelling of a modifier revier accepts to the canonical
// one. GNOME writes <Primary> for control in newer settings and <Control> in
// older ones, and both appear in the same dconf tree.
var modAlias = map[string]string{
	"ctrl": "ctrl", "control": "ctrl", "primary": "ctrl",
	"alt": "alt", "mod1": "alt",
	"shift": "shift",
	"super": "super", "mod4": "super", "meta": "super", "hyper": "super",
}

// gnomeMod is how GNOME spells each canonical modifier.
var gnomeMod = map[string]string{
	"ctrl": "<Control>", "alt": "<Alt>", "shift": "<Shift>", "super": "<Super>",
}

// gnomeKey is how GNOME spells the keys whose name is not simply the
// canonical one. GNOME key names come from gdk_keyval_from_name, which is case
// sensitive, so "return" is not a key and "Return" is.
var gnomeKey = map[string]string{
	"return": "Return", "escape": "Escape", "tab": "Tab",
	"backspace": "BackSpace", "delete": "Delete", "insert": "Insert",
	"up": "Up", "down": "Down", "left": "Left", "right": "Right",
	"home": "Home", "end": "End", "pageup": "Page_Up", "pagedown": "Page_Down",
	"page_up": "Page_Up", "page_down": "Page_Down", "menu": "Menu",
	"print": "Print",
}

var fnKey = regexp.MustCompile(`^f([1-9]|1[0-9]|2[0-4])$`)

// ParseChord accepts any of the three spellings and returns the canonical one.
//
// It is deliberately strict about modifiers: an unknown one is an error naming
// it, because silently dropping a modifier would make two different chords
// compare equal and report a key as held when it is not.
func ParseChord(s string) (Chord, error) {
	raw := strings.TrimSpace(s)
	if raw == "" {
		return "", fmt.Errorf("empty key")
	}

	var mods []string
	var key string
	if strings.HasPrefix(raw, "<") {
		var err error
		if mods, key, err = splitAngle(raw); err != nil {
			return "", err
		}
	} else {
		if strings.ContainsAny(raw, "<>") {
			return "", fmt.Errorf("key %q: a modifier must come before the key", s)
		}
		// Split rather than FieldsFunc: FieldsFunc drops empty fields, so
		// "ctrl-" would read as the key "ctrl" instead of a chord with a
		// missing key.
		parts := strings.Split(strings.ReplaceAll(raw, "-", "+"), "+")
		for _, p := range parts {
			if p == "" {
				return "", fmt.Errorf("key %q has an empty part", s)
			}
		}
		mods, key = parts[:len(parts)-1], parts[len(parts)-1]
	}

	if key == "" {
		return "", fmt.Errorf("key %q has modifiers but no key", s)
	}
	seen := map[string]bool{}
	for _, m := range mods {
		canon, ok := modAlias[strings.ToLower(m)]
		if !ok {
			return "", fmt.Errorf("key %q: unknown modifier %q", s, m)
		}
		seen[canon] = true
	}

	var out []string
	for _, m := range modOrder {
		if seen[m] {
			out = append(out, m)
		}
	}
	out = append(out, strings.ToLower(key))
	return Chord(strings.Join(out, "+")), nil
}

// splitAngle reads the <Modifier> prefixes off a GNOME accelerator.
func splitAngle(raw string) (mods []string, key string, err error) {
	rest := raw
	for strings.HasPrefix(rest, "<") {
		end := strings.Index(rest, ">")
		if end < 0 {
			return nil, "", fmt.Errorf("key %q: unclosed modifier", raw)
		}
		mods = append(mods, rest[1:end])
		rest = rest[end+1:]
	}
	if strings.ContainsAny(rest, "<>") {
		return nil, "", fmt.Errorf("key %q: modifier after the key", raw)
	}
	return mods, rest, nil
}

// GNOME renders the chord the way GNOME stores it. The modifier order is
// GTK's own - shift, control, alt, super - so a chord revier writes reads back
// identical to one the settings UI wrote.
//
// Nothing calls this yet: reading goes the other way, through ParseChord. It
// is the half `keys install` needs, and it is here now because a conversion
// proved in one direction only is a conversion nobody has checked.
func (c Chord) GNOME() string {
	// Split never returns nothing, so the last field is always the key.
	parts := strings.Split(string(c), "+")
	key, mods := parts[len(parts)-1], parts[:len(parts)-1]

	held := map[string]bool{}
	for _, m := range mods {
		held[m] = true
	}
	var out strings.Builder
	for _, m := range []string{"shift", "ctrl", "alt", "super"} {
		if held[m] {
			out.WriteString(gnomeMod[m])
		}
	}
	out.WriteString(gnomeKeyName(key))
	return out.String()
}

func gnomeKeyName(key string) string {
	if g, ok := gnomeKey[key]; ok {
		return g
	}
	if fnKey.MatchString(key) {
		return strings.ToUpper(key)
	}
	return key
}
