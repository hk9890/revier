package theme

import (
	"image/color"
	"reflect"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
)

// The indices are the ones termenv chose, which revier drew with before it
// rendered through lipgloss v2: in 16 colours a mocha hover row on bright
// black, where v2's own choice is black, and dim text in bright blue.
func TestA256Or16ColourTerminalGetsTheIndexRevierAlwaysDrew(t *testing.T) {
	type pair struct{ c256, c16 int }
	roles := []struct {
		name string
		of   func(Theme) color.Color
	}{
		{"Path", func(th Theme) color.Color { return th.Path.GetForeground() }},
		{"NameDim", func(th Theme) color.Color { return th.NameDim.GetForeground() }},
		{"Hover", func(th Theme) color.Color { return th.Hover.GetBackground() }},
		{"Cursor", func(th Theme) color.Color { return th.Cursor.GetBackground() }},
		{"ProjectName", func(th Theme) color.Color { return th.ProjectName.GetForeground() }},
		{"Accent", func(th Theme) color.Color { return th.Accent.GetForeground() }},
		{"Attention", func(th Theme) color.Color { return th.Attention.GetForeground() }},
		{"PathMissing", func(th Theme) color.Color { return th.PathMissing.GetForeground() }},
		{"Idle", func(th Theme) color.Color { return th.Idle.GetForeground() }},
		{"Meta", func(th Theme) color.Color { return th.Meta.GetForeground() }},
	}
	for flavour, want := range map[string][]pair{
		"catppuccin-mocha":     {{60, 12}, {103, 12}, {59, 8}, {59, 8}, {189, 13}, {183, 13}, {211, 13}, {217, 9}, {117, 14}, {111, 12}},
		"catppuccin-macchiato": {{66, 6}, {103, 12}, {59, 8}, {59, 8}, {189, 13}, {183, 13}, {210, 9}, {232, 0}, {116, 14}, {111, 12}},
		"catppuccin-frappe":    {{102, 8}, {103, 12}, {59, 8}, {59, 8}, {189, 13}, {182, 13}, {174, 9}, {232, 0}, {116, 14}, {111, 12}},
		"catppuccin-latte":     {{145, 7}, {103, 12}, {188, 7}, {146, 12}, {59, 8}, {99, 12}, {232, 0}, {167, 9}, {38, 6}, {27, 12}},
	} {
		th, err := Lookup(flavour, DefaultGlyphs)
		if err != nil {
			t.Fatal(err)
		}
		in256, in16 := th.In(colorprofile.ANSI256), th.In(colorprofile.ANSI)
		for i, role := range roles {
			if got := role.of(in256); got != ansi.IndexedColor(want[i].c256) {
				t.Errorf("%s %s in 256 colours = %v, want index %d", flavour, role.name, got, want[i].c256)
			}
			if got := role.of(in16); got != ansi.BasicColor(want[i].c16) {
				t.Errorf("%s %s in 16 colours = %v, want index %d", flavour, role.name, got, want[i].c16)
			}
		}
		if got := th.In(colorprofile.TrueColor).Hover.GetBackground(); got != th.Hover.GetBackground() {
			t.Errorf("%s hover in true colour = %v, want the hex colour %v", flavour, got, th.Hover.GetBackground())
		}
	}
}

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
