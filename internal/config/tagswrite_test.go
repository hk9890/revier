package config_test

import (
	"os"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
)

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

// A first tag added and then removed leaves a file that ends in one newline
// as it was: the blank line the add put before the table goes with it.
func TestWriteTagsAddThenRemoveLeavesTheFileAsItWas(t *testing.T) {
	file := tagFile(t, taggedHome)
	if _, err := config.WriteTags(file, []core.Tag{{Key: "type", Value: "work"}}, nil, config.Inherited{}); err != nil {
		t.Fatal(err)
	}
	if _, err := config.WriteTags(file, nil, []string{"type"}, config.Inherited{}); err != nil {
		t.Fatal(err)
	}
	if got := readTagFile(t, file); got != taggedHome {
		t.Errorf("file =\n%q\nwant\n%q", got, taggedHome)
	}
}

// Only a table written [tags] holds tags: the reader and the writer agree on
// the key, so a tag that loads is one revier tag can change.
func TestATableOfAnotherCaseHoldsNoTags(t *testing.T) {
	_, p := loadOne(t, "", taggedHome+"\n[Tags]\ntype = \"work\"\n")
	if len(p.Tags) != 0 {
		t.Errorf("Tags = %v, want none from [Tags]", p.Tags)
	}
}

// A table or an array under [tags] is told apart from a value quoting mends.
func TestATagThatIsATableSaysAValueIsAString(t *testing.T) {
	_, p := loadOne(t, "", taggedHome+"\n[tags]\nlist = [\"a\"]\n[tags.extra]\na = \"b\"\n")
	var all strings.Builder
	for _, err := range p.TagRefused {
		all.WriteString(err.Error() + "\n")
	}
	if strings.Contains(all.String(), "quote it") || strings.Count(all.String(), "not a table or an array") != 2 {
		t.Errorf("refusals =\n%s\nwant each to say a value is a string, without quote it", all.String())
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

// A write changes the asked-for tags and no other value: a key that cannot be
// removed is an error and not a write that did nothing, and a line inside a
// quoted key's string is not the key it reads as.
func TestWriteTagsChangesNothingElse(t *testing.T) {
	for name, c := range map[string]struct{ tags, remove string }{
		"a key with =": {"type = \"work\"\n", "type=work"},
		"a dotted key": {"lang.main = \"go\"\n", "lang"},
		"a table":      {"type = \"work\"\n[tags.sub]\na = \"b\"\n", "sub"},
	} {
		text := taggedHome + "\n[tags]\n" + c.tags
		file := tagFile(t, text)
		if _, err := config.WriteTags(file, nil, []string{c.remove}, config.Inherited{}); err == nil {
			t.Errorf("%s: want an error", name)
		}
		if got := readTagFile(t, file); got != text {
			t.Errorf("%s: a refused write changed the file:\n%s", name, got)
		}
	}

	text := taggedHome + "\n[tags]\n\"a=b\" = \"\"\"\ntype = \"x\"\n[x]\n\"\"\"\ntype = \"work\"\n"
	file := tagFile(t, text)
	if _, err := config.WriteTags(file, nil, []string{"type"}, config.Inherited{}); err != nil {
		t.Fatal(err)
	}
	if got, want := readTagFile(t, file), strings.TrimSuffix(text, "type = \"work\"\n"); got != want {
		t.Errorf("file =\n%s\nwant\n%s", got, want)
	}
}

// Tags that are already as asked are not written: the file is not replaced,
// and a table of comments keeps its header.
func TestWriteTagsThatChangesNothingDoesNotWrite(t *testing.T) {
	file := tagFile(t, taggedHome+"\n[tags]\n# type = \"work\"\n")
	before, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := config.WriteTags(file, nil, []string{"absent"}, config.Inherited{}); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) {
		t.Error("a write that changes nothing replaced the file")
	}
}
