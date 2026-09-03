// Package theme holds every colour and glyph the TUI draws with.
//
// Styles are named by role, not by colour: a path and a key legend are both
// grey today, but they are separate roles, so one can change without the
// other. The TUI names roles and never a colour.
//
// Glyphs are a separate choice from the palette because a terminal program
// cannot ask which font is loaded. A Nerd Font glyph in a terminal without one
// draws a box and can throw the row alignment out, so the safe set is the
// default and the Nerd Font set is opt-in.
package theme

import (
	"sort"
	"strconv"
	"strings"

	catppuccin "github.com/catppuccin/go"
	"github.com/charmbracelet/lipgloss"
)

// Theme is a resolved palette and glyph set. Every field is ready to render
// with; there is no lookup at draw time.
type Theme struct {
	Name   string
	Glyphs Glyphs

	Header      lipgloss.Style // the top line
	Accent      lipgloss.Style // counts and the filter, inside the header
	Cursor      lipgloss.Style // the selected row
	ProjectName lipgloss.Style // a project or target name
	NameDim     lipgloss.Style // the same name, for something not running
	Path        lipgloss.Style // a project path
	PathMissing lipgloss.Style // a path whose directory does not exist
	Meta        lipgloss.Style // host, store, mode - the second-line columns
	Remote      lipgloss.Style // anything living on another machine
	Running     lipgloss.Style // an agent or target that is up
	Idle        lipgloss.Style // an agent at rest
	Attention   lipgloss.Style // an agent waiting for the user, and errors
	Border      lipgloss.Style // pane separators
	Help        lipgloss.Style // the key legend
}

// Glyphs is the marker set. Every glyph is one cell wide, whatever the set.
type Glyphs struct {
	Running   string // an instance is up
	Stopped   string // it is not
	Attention string // it wants the user
	Cursor    string // the selected row
	Local     string // this machine
	Remote    string // another machine
}

// The three glyph sets. Unicode is the default: every one of its characters is
// in any font a terminal ships with. Nerd is the set the shell picker uses
// (nf-fa-home, nf-fa-globe). ASCII is for a terminal whose font is not yours.
var glyphSets = map[string]Glyphs{
	"unicode": {Running: "●", Stopped: "○", Attention: "!", Cursor: "▸", Local: "⌂", Remote: "↗"},
	"nerd":    {Running: "●", Stopped: "○", Attention: "!", Cursor: "▸", Local: "", Remote: ""},
	"ascii":   {Running: "*", Stopped: "-", Attention: "!", Cursor: ">", Local: "=", Remote: "@"},
}

var flavors = map[string]catppuccin.Flavor{
	"catppuccin-mocha":     catppuccin.Mocha,
	"catppuccin-macchiato": catppuccin.Macchiato,
	"catppuccin-frappe":    catppuccin.Frappe,
	"catppuccin-latte":     catppuccin.Latte,
}

// DefaultTheme and DefaultGlyphs are what an empty configuration gets.
const (
	DefaultTheme  = "catppuccin-mocha"
	DefaultGlyphs = "unicode"
)

// Themes and GlyphSets report the valid names, sorted, for an error message.
func Themes() []string    { return sortedKeys(flavors) }
func GlyphSets() []string { return sortedKeys(glyphSets) }

// Default is the theme with no configuration.
func Default() Theme {
	t, err := Lookup(DefaultTheme, DefaultGlyphs)
	if err != nil {
		// Unreachable: both defaults are keys of the maps above, and a test
		// pins that. Returning a zero Theme would render an invisible TUI.
		panic("theme: defaults do not resolve: " + err.Error())
	}
	return t
}

// Lookup resolves a theme name and a glyph set name. An empty name means the
// default, so a configuration that sets one and not the other works.
func Lookup(name, glyphs string) (Theme, error) {
	if name == "" {
		name = DefaultTheme
	}
	if glyphs == "" {
		glyphs = DefaultGlyphs
	}
	f, ok := flavors[name]
	if !ok {
		return Theme{}, &UnknownError{Kind: "theme", Name: name, Valid: Themes()}
	}
	g, ok := glyphSets[glyphs]
	if !ok {
		return Theme{}, &UnknownError{Kind: "glyph set", Name: glyphs, Valid: GlyphSets()}
	}
	return fromFlavor(name, f, g), nil
}

// fromFlavor maps a Catppuccin flavour onto the roles. The mapping follows the
// shell picker's, so revier looks like the tool it replaces: green for
// running, red for a missing path, blue for the metadata columns, mauve for
// anything remote (os_list_json.py:54).
func fromFlavor(name string, f catppuccin.Flavor, g Glyphs) Theme {
	c := func(col catppuccin.Color) lipgloss.Color { return lipgloss.Color(col.Hex) }
	fg := func(col catppuccin.Color) lipgloss.Style {
		return lipgloss.NewStyle().Foreground(c(col))
	}
	return Theme{
		Name:        name,
		Glyphs:      g,
		Header:      fg(f.Red()).Bold(true),
		Accent:      fg(f.Mauve()),
		Cursor:      lipgloss.NewStyle().Foreground(c(f.Text())).Background(c(f.Surface0())).Bold(true),
		ProjectName: fg(f.Text()),
		NameDim:     fg(f.Overlay1()),
		Path:        fg(f.Overlay1()),
		PathMissing: fg(f.Red()),
		Meta:        fg(f.Blue()),
		Remote:      fg(f.Mauve()),
		Running:     fg(f.Green()),
		Idle:        fg(f.Sky()),
		Attention:   fg(f.Red()).Bold(true),
		Border:      fg(f.Surface1()),
		Help:        fg(f.Overlay0()),
	}
}

// UnknownError is a configuration error that names what is valid, because a
// theme name is a guess until the user sees the list.
type UnknownError struct {
	Kind  string
	Name  string
	Valid []string
}

func (e *UnknownError) Error() string {
	return "unknown " + e.Kind + " " + quote(e.Name) + "; valid: " + join(e.Valid)
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func quote(s string) string { return strconv.Quote(s) }

func join(ss []string) string { return strings.Join(ss, ", ") }
