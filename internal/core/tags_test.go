package core_test

import (
	"slices"
	"testing"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/pkg/revier"
)

// A tag splits at its first "=", so a value can hold one, and a key cannot
// be empty: "=x" names no key a project could carry.
func TestParseTagSplitsAtTheFirstEquals(t *testing.T) {
	for in, want := range map[string]core.Tag{
		"type=work":   {Key: "type", Value: "work"},
		"url=a=b":     {Key: "url", Value: "a=b"},
		"note=":       {Key: "note", Value: ""},
		"language=go": {Key: "language", Value: "go"},
	} {
		got, err := core.ParseTag(in)
		if err != nil || got != want {
			t.Errorf("ParseTag(%q) = %+v, %v; want %+v", in, got, err, want)
		}
	}
	for _, in := range []string{"", "type", "=work"} {
		if _, err := core.ParseTag(in); err == nil {
			t.Errorf("ParseTag(%q): want an error", in)
		}
	}
}

func tagged(name string, tags map[string]string) core.Project {
	return core.Project{Project: revier.Project{Name: revier.ProjectName(name), Tags: tags}}
}

// More than one tag selects the projects that carry every one; a value is
// matched whole and with its case.
func TestWithTagsKeepsTheProjectsThatCarryEveryTag(t *testing.T) {
	projects := []core.Project{
		tagged("shop", map[string]string{"type": "work", "language": "go"}),
		tagged("blog", map[string]string{"type": "private", "language": "go"}),
		tagged("api", map[string]string{"type": "work", "language": "ts"}),
		tagged("bare", nil),
	}
	names := func(ps []core.Project) []revier.ProjectName {
		var out []revier.ProjectName
		for _, p := range ps {
			out = append(out, p.Name)
		}
		return out
	}
	for _, c := range []struct {
		tags []core.Tag
		want []revier.ProjectName
	}{
		{[]core.Tag{{Key: "type", Value: "work"}}, []revier.ProjectName{"shop", "api"}},
		{[]core.Tag{{Key: "type", Value: "work"}, {Key: "language", Value: "go"}}, []revier.ProjectName{"shop"}},
		{[]core.Tag{{Key: "type", Value: "Work"}}, nil},
		{[]core.Tag{{Key: "type", Value: "wor"}}, nil},
		{nil, []revier.ProjectName{"shop", "blog", "api", "bare"}},
	} {
		if got := names(core.WithTags(projects, c.tags)); !slices.Equal(got, c.want) {
			t.Errorf("WithTags(%v) = %v, want %v", c.tags, got, c.want)
		}
	}
}

func TestTagsInUseIsEachTagByKeyThenValueWithItsProjects(t *testing.T) {
	got := core.TagsInUse([]core.Project{
		tagged("shop", map[string]string{"type": "work", "language": "go"}),
		tagged("blog", map[string]string{"type": "private", "language": "go"}),
		tagged("bare", nil),
	})
	want := []core.TagUse{
		{Tag: core.Tag{Key: "language", Value: "go"}, Projects: []revier.ProjectName{"shop", "blog"}},
		{Tag: core.Tag{Key: "type", Value: "private"}, Projects: []revier.ProjectName{"blog"}},
		{Tag: core.Tag{Key: "type", Value: "work"}, Projects: []revier.ProjectName{"shop"}},
	}
	if !slices.EqualFunc(got, want, func(a, b core.TagUse) bool { return a.Tag == b.Tag && slices.Equal(a.Projects, b.Projects) }) {
		t.Errorf("TagsInUse = %+v, want %+v", got, want)
	}
}
