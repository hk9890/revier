package core

import (
	"cmp"
	"errors"
	"slices"
	"strings"

	"github.com/hk9890/revier/pkg/revier"
)

// Tag is one key=value of a project file's [tags].
type Tag struct{ Key, Value string }

func (t Tag) String() string { return t.Key + "=" + t.Value }

// ParseTag reads a tag as a selector writes it: key=value, split at the first
// "=", so a value can hold one. The value can be empty; the key cannot.
func ParseTag(s string) (Tag, error) {
	key, value, ok := strings.Cut(s, "=")
	if !ok || key == "" {
		return Tag{}, errors.New("a tag is key=value: " + s)
	}
	return Tag{Key: key, Value: value}, nil
}

// HasTags reports whether the project carries every one of tags.
func HasTags(p revier.Project, tags []Tag) bool {
	for _, t := range tags {
		if v, ok := p.Tags[t.Key]; !ok || v != t.Value {
			return false
		}
	}
	return true
}

// WithTags is the projects that carry every one of tags, in the order given.
func WithTags(projects []Project, tags []Tag) []Project {
	return slices.DeleteFunc(slices.Clone(projects), func(p Project) bool { return !HasTags(p.Project, tags) })
}

// TagUse is one tag and the projects that carry it.
type TagUse struct {
	Tag
	Projects []revier.ProjectName
}

// TagsInUse is every tag the projects carry, by key and then by value, each
// with its projects in the order given. It is how a user or an agent learns
// which keys and values exist before selecting by one.
func TagsInUse(projects []Project) []TagUse {
	by := map[Tag][]revier.ProjectName{}
	for _, p := range projects {
		for k, v := range p.Tags {
			t := Tag{Key: k, Value: v}
			by[t] = append(by[t], p.Name)
		}
	}
	out := make([]TagUse, 0, len(by))
	for t, names := range by {
		out = append(out, TagUse{Tag: t, Projects: names})
	}
	slices.SortFunc(out, func(a, b TagUse) int {
		return cmp.Or(cmp.Compare(a.Key, b.Key), cmp.Compare(a.Value, b.Value))
	})
	return out
}

// SortedTags is a project's tags by key, the order every surface shows them in.
func SortedTags(tags map[string]string) []Tag {
	out := make([]Tag, 0, len(tags))
	for k, v := range tags {
		out = append(out, Tag{Key: k, Value: v})
	}
	slices.SortFunc(out, func(a, b Tag) int { return cmp.Compare(a.Key, b.Key) })
	return out
}
