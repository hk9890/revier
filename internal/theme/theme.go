// Package theme holds every colour and glyph the TUI draws with.
//
// Styles are named by role, not by colour: a path and a key legend are both
// grey today, but they are separate roles, so one can change without the
// other. The TUI names roles and never a colour.
//
// Glyphs are a separate choice from the palette because a terminal program
// cannot ask which font is loaded. The Nerd Font set is the default, because
// the popup this replaces drew its icons from one and the surface is meant to
// look like it; a terminal without the font draws boxes, and picks the unicode
// set in [ui].
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

	Badge       lipgloss.Style // the product name, as a tag at the start of the header
	Header      lipgloss.Style // the top line
	Heading     lipgloss.Style // a section title in the detail pane
	Count       lipgloss.Style // a count that is zero, and so says nothing is wrong
	Match       lipgloss.Style // the letters of a name the filter matched
	Accent      lipgloss.Style // counts and the filter, inside the header
	Cursor      lipgloss.Style // the selected row
	Hover       lipgloss.Style // the row or button under the pointer
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
	Frame       lipgloss.Style // the border around the whole surface
}

// Glyphs is the marker set. Every glyph is one cell wide, whatever the set.
//
// Each column of a row means one thing. The mark says whether the project is
// open; the agent glyph, beside the agent's state in words, says what its
// agent is doing. A "!" in the mark column for an agent that wanted the user
// read as punctuation, and said again what the words on the right said.
//
// There is no glyph for where a project lives. Every project is on this
// machine until sessions over a network exist, and a mark that is the same on
// every row says nothing.
type Glyphs struct {
	Running string // the project is open
	Stopped string // it is not
	Cursor  string // the bar down the left of the selected row

	// What an agent is doing. Each stands beside the state in words, so a
	// glyph only has to be told apart from the others, not read on its own.
	NeedsYou string // it asked for the user and waits
	Working  string // it is in a turn
	Idle     string // it is at rest, waiting for the next prompt
	Unknown  string // its probe could not tell

	// Folder and NoFolder are an icon column in front of the name: whether
	// the project's directory is on this machine. A set draws both or
	// neither. A set without them says it only in words, on the path line,
	// and gives the name the room.
	Folder   string
	NoFolder string
	// Remote takes the folder column for a project on another machine. A
	// set that draws the folder draws this too.
	Remote string
}

// The three glyph sets. Nerd is the default: it says things with icons - a
// bell for an agent that wants you, which reads as a call rather than as
// punctuation - and needs a patched font. Unicode says the same with the
// characters any font a terminal ships with has. ASCII is for a terminal whose
// font is not yours.
//
// The cursor is a half block in every set but ASCII: a bar the height of the
// row reads as "this one" from across the screen, where a chevron in front of
// the first line reads as punctuation (fzf draws its selection the same way).
//
// The unicode agent glyphs are one family: a diamond for an agent waiting on
// the user, solid when it is blocked on an answer and hollow when it has only
// finished its turn, and a play mark for one that is working.
var glyphSets = map[string]Glyphs{
	"unicode": {
		Running: "\u25cf", Stopped: "\u25cb", Cursor: "\u258c",
		NeedsYou: "\u25c6", Working: "\u25b6", Idle: "\u25c7", Unknown: "?",
	},
	"nerd": {
		Running:  "\uf111", // nf-fa-circle
		Stopped:  "\uf10c", // nf-fa-circle_o
		Cursor:   "\u258c", // a half block, as in the unicode set
		NeedsYou: "\uf0f3", // nf-fa-bell
		Working:  "\uf110", // nf-fa-spinner
		Idle:     "\uf04c", // nf-fa-pause
		Unknown:  "\uf128", // nf-fa-question
		Folder:   "\uf07b", // nf-fa-folder
		NoFolder: "\uf114", // nf-fa-folder_o
		Remote:   "\uf233", // nf-fa-server
	},
	"ascii": {
		Running: "*", Stopped: "-", Cursor: ">",
		NeedsYou: "!", Working: "~", Idle: ".", Unknown: "?",
	},
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
	DefaultGlyphs = "nerd"
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
// running, blue for the metadata columns, mauve for anything remote
// (os_list_json.py:54).
//
// Three departures from it. A path is a step dimmer than a stopped name, so the
// names are what the eye lands on. A missing path is maroon, not red: on a
// machine that has a third of the checkouts, red on every third row drowned
// out the red that means an agent wants you. And the header is quiet text
// behind a badge, where it was red throughout, so the one red count in it is
// the one that needs reading.
func fromFlavor(name string, f catppuccin.Flavor, g Glyphs) Theme {
	c := func(col catppuccin.Color) lipgloss.Color { return lipgloss.Color(col.Hex) }
	fg := func(col catppuccin.Color) lipgloss.Style {
		return lipgloss.NewStyle().Foreground(c(col))
	}
	return Theme{
		Name:        name,
		Glyphs:      g,
		Badge:       fg(f.Base()).Background(c(f.Mauve())).Bold(true).Padding(0, 1),
		Header:      fg(f.Text()).Bold(true),
		Heading:     fg(f.Blue()).Bold(true),
		Count:       fg(f.Overlay0()),
		Match:       fg(f.Peach()).Bold(true),
		Accent:      fg(f.Mauve()),
		Cursor:      lipgloss.NewStyle().Foreground(c(f.Mauve())).Background(c(f.Surface1())).Bold(true),
		Hover:       lipgloss.NewStyle().Background(c(f.Surface0())),
		ProjectName: fg(f.Text()),
		NameDim:     fg(f.Overlay1()),
		Path:        fg(f.Overlay0()),
		PathMissing: fg(f.Maroon()),
		Meta:        fg(f.Blue()),
		Remote:      fg(f.Mauve()),
		Running:     fg(f.Green()),
		Idle:        fg(f.Sky()),
		Attention:   fg(f.Red()).Bold(true),
		Border:      fg(f.Surface1()),
		Help:        fg(f.Overlay0()),
		Frame: fg(f.Text()).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(c(f.Surface1())).
			Padding(0, 1),
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

// OnSelection is a style as it renders inside the selected row: the row's
// background, applied to every segment, so the highlight is continuous rather
// than one band per styled run.
func (t Theme) OnSelection(s lipgloss.Style) lipgloss.Style {
	return s.Background(t.Cursor.GetBackground())
}

// OnHover is a style as it renders inside the row or button the pointer is
// on. It is a step below the selection: two rows can be lit at once - the
// one the keys act on and the one the pointer is over - and which of them
// Enter means has to be readable at a glance.
func (t Theme) OnHover(s lipgloss.Style) lipgloss.Style {
	return s.Background(t.Hover.GetBackground())
}
