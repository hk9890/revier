package config_test

import (
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
)

func crlf(s string) string { return strings.ReplaceAll(s, "\n", "\r\n") }

// A project file whose lines end in CRLF keeps them through every write: a
// value replaced in place, a key added to a table, and a table added at the
// end.
func TestAWriteKeepsCRLFLineEnds(t *testing.T) {
	file := tagFile(t, crlf(taggedHome+"\n[tags]\ntype = \"work\" # who pays\n"))
	if _, err := config.WriteTags(file, []core.Tag{{Key: "type", Value: "private"}, {Key: "lang", Value: "go"}}, nil, config.Inherited{}); err != nil {
		t.Fatal(err)
	}
	if _, err := config.SetProjectValue(file, "git_url", "git@github.com:o/demo.git", config.Inherited{}); err != nil {
		t.Fatal(err)
	}
	got := readTagFile(t, file)
	if strings.Count(got, "\n") != strings.Count(got, "\r\n") {
		t.Errorf("file has a line end that is not CRLF:\n%q", got)
	}
	for _, want := range []string{"type = \"private\" # who pays\r\n", "lang = \"go\"\r\n", "git_url = \"git@github.com:o/demo.git\"\r\n"} {
		if !strings.Contains(got, want) {
			t.Errorf("file does not hold %q:\n%q", want, got)
		}
	}
}

func TestSetKeepsCRLFLineEnds(t *testing.T) {
	root := writeConfig(t, crlf("[ui]\ntheme = \"catppuccin-mocha\"\n"))
	if err := config.Set(root, "ui", "glyphs", "ascii"); err != nil {
		t.Fatal(err)
	}
	if got, want := readConfig(t, root), crlf("[ui]\ntheme = \"catppuccin-mocha\"\nglyphs = \"ascii\"\n"); got != want {
		t.Errorf("config.toml = %q, want %q", got, want)
	}
}

// A file that is mixed already is not made one or the other: the lines the
// write does not touch keep the ends they had, and so does the one it
// replaces a value in.
func TestAWriteLeavesAMixedFileMixed(t *testing.T) {
	file := tagFile(t, taggedHome+"\n[tags]\r\ntype = \"work\"\r\n")
	if _, err := config.WriteTags(file, []core.Tag{{Key: "type", Value: "private"}}, nil, config.Inherited{}); err != nil {
		t.Fatal(err)
	}
	if got, want := readTagFile(t, file), taggedHome+"\n[tags]\r\ntype = \"private\"\r\n"; got != want {
		t.Errorf("file = %q, want %q", got, want)
	}
}
