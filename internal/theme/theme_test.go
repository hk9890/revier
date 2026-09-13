package theme

import (
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestDefaultsResolve(t *testing.T) {
	th := Default()
	if th.Name != DefaultTheme {
		t.Fatalf("default theme = %q, want %q", th.Name, DefaultTheme)
	}
	if th.Glyphs != glyphSets[DefaultGlyphs] {
		t.Fatalf("default glyphs = %+v, want %+v", th.Glyphs, glyphSets[DefaultGlyphs])
	}
}

func TestEmptyNamesMeanTheDefault(t *testing.T) {
	th, err := Lookup("", "")
	if err != nil {
		t.Fatalf("Lookup(\"\", \"\"): %v", err)
	}
	if th.Name != DefaultTheme || th.Glyphs != glyphSets[DefaultGlyphs] {
		t.Fatalf("empty names resolved to %q/%+v", th.Name, th.Glyphs)
	}
}

func TestUnknownNameListsTheValidOnes(t *testing.T) {
	for _, tc := range []struct {
		name, glyphs, wantKind, wantValid string
	}{
		{"dracula", "unicode", "theme", "catppuccin-mocha"},
		{"catppuccin-mocha", "emoji", "glyph set", "unicode"},
	} {
		_, err := Lookup(tc.name, tc.glyphs)
		if err == nil {
			t.Fatalf("Lookup(%q, %q) = no error", tc.name, tc.glyphs)
		}
		msg := err.Error()
		if !strings.Contains(msg, tc.wantKind) || !strings.Contains(msg, tc.wantValid) {
			t.Fatalf("Lookup(%q, %q) error = %q, want it to name the kind and a valid name",
				tc.name, tc.glyphs, msg)
		}
	}
}

// Every flavour must fill every role. A zero style renders unstyled text,
// which is invisible against nothing and silently different from the rest.
func TestEveryRoleIsSetInEveryFlavour(t *testing.T) {
	for _, name := range Themes() {
		th, err := Lookup(name, DefaultGlyphs)
		if err != nil {
			t.Fatalf("Lookup(%q): %v", name, err)
		}
		v := reflect.ValueOf(th)
		for i := range v.NumField() {
			f := v.Type().Field(i)
			style, ok := v.Field(i).Interface().(lipgloss.Style)
			if !ok {
				continue
			}
			if backgroundOnly[f.Name] {
				if _, unset := style.GetBackground().(lipgloss.NoColor); unset {
					t.Errorf("%s: role %s has no background", name, f.Name)
				}
				continue
			}
			if _, unset := style.GetForeground().(lipgloss.NoColor); unset {
				t.Errorf("%s: role %s has no foreground", name, f.Name)
			}
		}
	}
}

// backgroundOnly are the roles that lend a background to text styled by
// another role and never render text themselves.
var backgroundOnly = map[string]bool{"Hover": true}

// optional are the glyphs a set may leave out: the folder column, which a set
// draws for both states or not at all.
var optional = map[string]bool{"Folder": true, "NoFolder": true, "Remote": true}

// The comment on Glyphs promises one cell each. A two-cell glyph in a row
// built from padded columns moves every column after it.
func TestEveryGlyphIsOneCell(t *testing.T) {
	for setName, g := range glyphSets {
		v := reflect.ValueOf(g)
		for i := range v.NumField() {
			name, glyph := v.Type().Field(i).Name, v.Field(i).String()
			if glyph == "" && optional[name] {
				continue
			}
			if w := lipgloss.Width(glyph); w != 1 {
				t.Errorf("%s glyph %s = %q, width %d, want 1", setName, name, glyph, w)
			}
		}
	}
}

// A spinner frame stands where the working glyph does, so it is one cell too.
func TestEverySetSpinsInOneCell(t *testing.T) {
	for setName := range glyphSets {
		if len(spinners[setName]) < 2 {
			t.Errorf("%s glyph set has no spinner", setName)
			continue
		}
		// The header's still glyph is the icon the rows animate.
		if first := spinners[setName][0]; glyphSets[setName].Working != first {
			t.Errorf("%s working glyph = %q, want the spinner's first frame %q", setName, glyphSets[setName].Working, first)
		}
		for _, frame := range spinners[setName] {
			if w := lipgloss.Width(frame); w != 1 {
				t.Errorf("%s spinner frame %q, width %d, want 1", setName, frame, w)
			}
		}
	}
}

func TestNoGlyphSetIsIncomplete(t *testing.T) {
	for setName, g := range glyphSets {
		v := reflect.ValueOf(g)
		for i := range v.NumField() {
			if name := v.Type().Field(i).Name; v.Field(i).String() == "" && !optional[name] {
				t.Errorf("%s glyph set has no %s", setName, name)
			}
		}
		if (g.Folder == "") != (g.NoFolder == "") || (g.Folder == "") != (g.Remote == "") {
			t.Errorf("%s glyph set draws one folder state and not the others", setName)
		}
	}
}
