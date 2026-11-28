package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
)

func tagFile(t *testing.T, text string) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), "demo.toml")
	if err := os.WriteFile(file, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return file
}

func readTagFile(t *testing.T, file string) string {
	t.Helper()
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// A first tag adds the table at the end; every line before it stays, and a
// value is written as TOML reads it back.
func TestWriteTagsAddsTheTableAndKeepsTheFile(t *testing.T) {
	file := tagFile(t, "# my project\n"+taggedHome)
	p, err := config.WriteTags(file, []core.Tag{{Key: "type", Value: "work"}, {Key: "note", Value: "a \"b\"\nc"}}, nil, config.Inherited{})
	if err != nil {
		t.Fatal(err)
	}
	want := "# my project\n" + taggedHome + "\n[tags]\ntype = \"work\"\nnote = \"a \\\"b\\\"\\nc\"\n"
	if got := readTagFile(t, file); got != want {
		t.Errorf("file =\n%s\nwant\n%s", got, want)
	}
	if p.Tags["note"] != "a \"b\"\nc" {
		t.Errorf("Tags = %q", p.Tags)
	}
}

// A tag set again is replaced where it stands, with its comment; a value that
// was refused is mended by the write.
func TestWriteTagsReplacesInPlace(t *testing.T) {
	file := tagFile(t, taggedHome+"\n[tags]\ntype = \"work\" # who pays\nyear = 2024\n")
	if _, err := config.WriteTags(file, []core.Tag{{Key: "type", Value: "private"}, {Key: "year", Value: "2024"}}, nil, config.Inherited{}); err != nil {
		t.Fatal(err)
	}
	want := taggedHome + "\n[tags]\ntype = \"private\" # who pays\nyear = \"2024\"\n"
	if got := readTagFile(t, file); got != want {
		t.Errorf("file =\n%s\nwant\n%s", got, want)
	}
}

// The last tag removed takes its table with it; a comment in it stays, and a
// key the table does not have is no error.
func TestWriteTagsRemovesTheTableWithTheLastTag(t *testing.T) {
	file := tagFile(t, taggedHome+"\n[tags] # mine\ntype = \"work\"\nlanguage = \"go\"\n")
	p, err := config.WriteTags(file, nil, []string{"type", "language", "absent"}, config.Inherited{})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Tags) != 0 {
		t.Errorf("Tags = %v, want none", p.Tags)
	}
	if got := readTagFile(t, file); strings.Contains(got, "[tags]") || !strings.Contains(got, "# mine") {
		t.Errorf("file =\n%s\nwant no [tags] table and its comment kept", got)
	}
}

// A key the editor does not read keeps the table when the last key it reads
// goes: without its header the key would land in the table above, and the
// refusal doctor gives for it would go with it.
func TestWriteTagsKeepsATableAKeyItCannotReadStillHolds(t *testing.T) {
	file := tagFile(t, taggedHome+"\n[tags]\ntype = \"work\"\n\"a=b\" = \"c\"\n")
	p, err := config.WriteTags(file, nil, []string{"type"}, config.Inherited{})
	if err != nil {
		t.Fatal(err)
	}
	if got := readTagFile(t, file); !strings.Contains(got, "[tags]\n\"a=b\" = \"c\"\n") {
		t.Errorf("file =\n%s\nwant [tags] kept over the quoted key", got)
	}
	if len(p.TagRefused) != 1 {
		t.Errorf("TagRefused = %v, want the quoted key still refused", p.TagRefused)
	}
}

// Refusals come in key order, so two runs of doctor print the same lines.
func TestRefusedTagsAreInKeyOrder(t *testing.T) {
	_, p := loadOne(t, "", taggedHome+"\n[tags]\nzeta = 1\nalpha = true\nmid = 2.5\n")
	var keys []string
	for _, err := range p.TagRefused {
		keys = append(keys, strings.SplitN(err.Error(), ":", 2)[0])
	}
	if got := strings.Join(keys, ","); got != `tag "alpha",tag "mid",tag "zeta"` {
		t.Errorf("refusals = %s, want them by key", got)
	}
}

// Tags the line editor cannot find again are refused, and the file stays as
// it was.
func TestWriteTagsRefusesWhatItCannotWriteAgain(t *testing.T) {
	inline := "tags = { type = \"work\" }\n" + taggedHome
	file := tagFile(t, inline)
	if _, err := config.WriteTags(file, []core.Tag{{Key: "type", Value: "private"}}, nil, config.Inherited{}); err == nil {
		t.Error("a write over an inline [tags]: want an error")
	}
	if got := readTagFile(t, file); got != inline {
		t.Errorf("a refused write changed the file:\n%s", got)
	}
	if _, err := config.WriteTags(file, []core.Tag{{Key: "a b", Value: "c"}}, nil, config.Inherited{}); err == nil {
		t.Error("a key that is not bare: want an error")
	}
}
