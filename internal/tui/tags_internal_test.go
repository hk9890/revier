package tui

import (
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/plain"
	"github.com/hk9890/revier/internal/theme"
)

func tagList(pairs ...string) []core.Tag {
	var out []core.Tag
	for i := 0; i < len(pairs); i += 2 {
		out = append(out, core.Tag{Key: pairs[i], Value: pairs[i+1]})
	}
	return out
}

// Tags that fit are all shown, even where the room a "+N more" would need
// after the first is not there; a first tag with no room left for any of it
// is not counted as shown.
func TestClosedTagsShowWhatFitsAndCountTheRest(t *testing.T) {
	for _, c := range []struct {
		name  string
		tags  []core.Tag
		room  int
		shown string
		n     int
		cut   bool
	}{
		{"all fit, with no room for a more", tagList("k1", "v1", "k2", "v2", "k3", "v3"), 20, "k1=v1  k2=v2  k3=v3", 3, false},
		{"two that fit exactly", tagList("aaaa", "1", "bb", "2"), 12, "aaaa=1  bb=2", 2, false},
		{"some fit", tagList("k1", "v1", "k2", "v2", "k3", "v3333"), 21, "k1=v1  k2=v2", 2, false},
		{"one too long", tagList("repo", "https://github.com/org/a-long-name"), 20, "repo=https://github…", 1, true},
		{"no room for any", tagList("a", "1", "b", "2", "c", "3", "d", "4", "e", "5", "f", "6"), 5, "", 0, false},
		{"a control character is no text", tagList("note", "a\tb\nc"), 20, "note=a bc", 1, false},
	} {
		shown, n, cut := closedTags(c.tags, c.room)
		if shown != c.shown || n != c.n || cut != c.cut {
			t.Errorf("%s: closedTags = %q, %d, %v; want %q, %d, %v", c.name, shown, n, cut, c.shown, c.n, c.cut)
		}
	}
}

// A tag cut in the closed row opens, and the open row wraps it, so a click
// shows every tag whole.
func TestACutTagOpensAndWrapsWhole(t *testing.T) {
	th := theme.Default()
	tags := tagList("repo", "https://github.com/org/a-long-repository-name")
	_, toggles := tagsRow(th, tags, 30, false, false)
	if !toggles {
		t.Error("a row that cut its one tag does not open")
	}
	open, _ := tagsRow(th, tags, 30, true, false)
	if got := strings.Join(strings.Fields(plain.Text(open)), ""); !strings.Contains(got, "repo=https://github.com/org/a-long-repository-name") {
		t.Errorf("the open row does not hold the whole tag:\n%s", open)
	}
}
