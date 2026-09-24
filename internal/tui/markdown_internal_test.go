package tui

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/hk9890/revier/internal/theme"
)

// plain is markdown's lines with their styling taken off, as a reader sees
// them.
func plain(text string, w int) []string {
	lines := markdown(text, w, theme.Default())
	for i := range lines {
		lines[i] = ansi.Strip(lines[i])
	}
	return lines
}

func TestMarkdownSetsEachPartAsItReads(t *testing.T) {
	for _, tc := range []struct {
		name string
		text string
		w    int
		want []string
	}{
		{"a heading loses its marks", "## What I did", 40,
			[]string{"What I did"}},
		{"bold, code and a link read as their text", "**Tests:** `go test` passed, see [the PR](https://x/1)", 60,
			[]string{"Tests: go test passed, see the PR"}},
		{"a paragraph wraps at the width", "one two three four five six", 13,
			[]string{"one two three", "four five six"}},
		{"a list item hangs under its text", "- first second third fourth", 14,
			[]string{"• first second", "  third fourth"}},
		{"a numbered item keeps its number", "2. the second step", 40,
			[]string{"2. the second step"}},
		{"a nested item keeps its indent", "- outer\n  - inner", 40,
			[]string{"• outer", "  • inner"}},
		{"a code block is indented and cut, not wrapped", "```bash\ngit log --oneline -1\n```", 12,
			[]string{"  git log --"}},
		{"a table is set in columns without its separator", "| Step | Result |\n|:---|---:|\n| Merge PR #28 | done |", 40,
			[]string{"Step          Result", "Merge PR #28  done"}},
		{"blank lines collapse and none trails", "first\n\n\n\nsecond\n\n", 40,
			[]string{"first", "", "second"}},
		{"a heading is set apart from the line before it", "done.\n## Next", 40,
			[]string{"done.", "", "Next"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := plain(tc.text, tc.w); !slices.Equal(got, tc.want) {
				t.Errorf("markdown(%q, %d) =\n%q\nwant\n%q", tc.text, tc.w, got, tc.want)
			}
		})
	}
}

// A backtick left open is text: it is shown, and it does not start code that
// swallows the rest of the line.
func TestMarkdownLeavesAnUnclosedBacktickAsText(t *testing.T) {
	if got := plain("run `make` then `test", 40); !slices.Equal(got, []string{"run make then `test"}) {
		t.Errorf("markdown = %q, want the open backtick and the text after it kept", got)
	}
}

// Markup around and inside code reads as text: a heading's code, bold that
// holds code, and code that holds what looks like bold.
func TestMarkdownSetsCodeInsideOtherMarkup(t *testing.T) {
	for _, tc := range []struct{ text, want string }{
		{"### `internal/tui/markdown.go`", "internal/tui/markdown.go"},
		{"**`go test`** passed", "go test passed"},
		{"see `**not bold**` here", "see **not bold** here"},
	} {
		if got := plain(tc.text, 60); !slices.Equal(got, []string{tc.want}) {
			t.Errorf("markdown(%q) = %q, want %q", tc.text, got, []string{tc.want})
		}
	}
}

// A tab in a table cell - the header's included, which is drawn bold - is set
// as the spaces it is drawn as, so the columns line up and the padding that
// does it is never negative.
func TestMarkdownSetsATableWithATabInItsHeader(t *testing.T) {
	got := plain("| a\tb | c |\n|---|---|\n| x | y |", 40)
	if len(got) != 2 {
		t.Fatalf("lines = %q, want a header and a row", got)
	}
	if c, y := strings.Index(got[0], "c"), strings.Index(got[1], "y"); c != y {
		t.Errorf("columns do not line up: c at %d, y at %d in %q", c, y, got)
	}
}

// A cell's markup is measured as the text it reads as, so a column with code
// in it lines up with one without.
func TestMarkdownMeasuresATablesCellsAsTheyRead(t *testing.T) {
	got := plain("| A | B |\n|---|---|\n| `long cell` | y |\n| s | z |", 40)
	if len(got) != 3 {
		t.Fatalf("lines = %q, want a header and two rows", got)
	}
	b, y, z := strings.Index(got[0], "B"), strings.Index(got[1], "y"), strings.Index(got[2], "z")
	if b != y || y != z {
		t.Errorf("columns do not line up: B at %d, y at %d, z at %d in %q", b, y, z, got)
	}
}

func TestAgoSaysTheLargestWholeUnit(t *testing.T) {
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		before time.Duration
		want   string
	}{
		{20 * time.Second, "just now"},
		{time.Minute, "1 minute ago"},
		{14 * time.Minute, "14 minutes ago"},
		{90 * time.Minute, "1 hour ago"},
		{5 * time.Hour, "5 hours ago"},
		{50 * time.Hour, "2 days ago"},
	} {
		if got := ago(now, now.Add(-tc.before)); got != tc.want {
			t.Errorf("ago(%v before) = %q, want %q", tc.before, got, tc.want)
		}
	}
}
