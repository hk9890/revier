package config_test

import (
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
)

// A """ string can hold a line that reads as a header, a key or a comment.
// The line editor reads past it, so a write after it lands in the right
// table.
func TestSetReadsPastAMultiLineString(t *testing.T) {
	for _, value := range []string{
		`"""` + "\n[hosts]\nruntime = x # no comment\n" + `"""`,
		`'''` + "\n[hosts]\n" + `'''`,
		`"""` + "\n" + `say \"""` + "\n" + `"""`, // an escaped quote run is the string's own
		`"""` + "a\n" + `b"""""`,                 // the two quotes before the closing three are the string's own
	} {
		root := writeConfig(t, "[ui]\nnote = "+value+"\ntheme = \"catppuccin-mocha\"\n")
		if err := config.Set(root, "ui", "theme", "catppuccin-latte"); err != nil {
			t.Fatalf("note = %s: %v", value, err)
		}
		want := "[ui]\nnote = " + value + "\ntheme = \"catppuccin-latte\"\n"
		if got := readConfig(t, root); got != want {
			t.Errorf("config.toml =\n%s\nwant\n%s", got, want)
		}
	}
}

// The probe the review ran: a [tags] table with a """ value that holds a
// header line takes a new value for the key after it.
func TestWriteTagsReadsPastAMultiLineValue(t *testing.T) {
	text := taggedHome + "\n[tags]\nnote = \"\"\"\n[x]\n\"\"\"\ntype = \"work\"\n"
	file := tagFile(t, text)
	if _, err := config.WriteTags(file, []core.Tag{{Key: "type", Value: "private"}}, nil, config.Inherited{}); err != nil {
		t.Fatal(err)
	}
	if got := readTagFile(t, file); got != strings.Replace(text, `type = "work"`, `type = "private"`, 1) {
		t.Errorf("file =\n%s", got)
	}
}

// A comment mark inside a """ string is text: a removal keeps only the
// comments outside it.
func TestWriteTagsRemovesAMultiLineValueWithoutItsText(t *testing.T) {
	file := tagFile(t, taggedHome+"\n[tags]\ntype = \"work\"\nnote = \"\"\" # not a comment\nline\n\"\"\" # kept\n")
	if _, err := config.WriteTags(file, nil, []string{"note"}, config.Inherited{}); err != nil {
		t.Fatal(err)
	}
	got := readTagFile(t, file)
	if strings.Contains(got, "not a comment") || strings.Contains(got, "line\n") || !strings.Contains(got, "# kept") {
		t.Errorf("file =\n%s\nwant the value gone and the comment after it kept", got)
	}
}
