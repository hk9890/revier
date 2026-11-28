package config

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"reflect"
	"regexp"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/hk9890/revier/internal/core"
)

// projectTags is the [tags] table of a project file, and why each tag left
// out of it is. A tag is left out alone when a selector could not name it: a
// value that is not a string, or a key that is empty or holds the "=" that
// `-t key=value` splits at. The rest of the project loads either way, since
// a tag only selects.
func projectTags(data []byte) (map[string]string, []error) {
	// The key is read as written, as the line editor finds it: a struct
	// field would also take [Tags], which no write could then reach.
	var raw map[string]any
	if _, err := toml.Decode(string(data), &raw); err != nil {
		return nil, nil // the loader reports it, about the whole file
	}
	table, _ := raw["tags"].(map[string]any)
	if _, ok := raw["tags"]; ok && table == nil {
		return nil, []error{errors.New("tags: tags is a table of key = \"value\"; it is left out")}
	}
	var refused []error
	tags := make(map[string]string, len(table))
	for _, key := range slices.Sorted(maps.Keys(table)) {
		value := table[key]
		s, ok := value.(string)
		switch {
		case key == "":
			refused = append(refused, fmt.Errorf("tag %q: the key is empty, so -t cannot select it; the tag is left out", key))
		case strings.Contains(key, "="):
			refused = append(refused, fmt.Errorf("tag %q: the key holds \"=\", where -t key=value splits; the tag is left out", key))
		case !ok && nested(value):
			refused = append(refused, fmt.Errorf("tag %q: a tag's value is a string, not a table or an array; the tag is left out", key))
		case !ok:
			refused = append(refused, fmt.Errorf("tag %q: the value is %T, not a string; quote it; the tag is left out", key, value))
		default:
			tags[key] = s
		}
	}
	if len(tags) == 0 {
		tags = nil
	}
	return tags, refused
}

// nested reports whether a TOML value is a table or an array, which quoting
// does not make a string.
func nested(value any) bool {
	switch value.(type) {
	case map[string]any, []any, []map[string]any:
		return true
	}
	return false
}

// bareKey is a key the line editor finds again: a quoted one it does not
// read, so a second write of it would land twice.
var bareKey = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// ValidateTagKey refuses a key `revier tag` cannot write and find again.
func ValidateTagKey(key string) error {
	if !bareKey.MatchString(key) {
		return fmt.Errorf("tag key %q: revier tag writes letters, digits, _ and - alone; write any other key by hand", key)
	}
	return nil
}

// errTagsShape refuses a write whose result is not the file with the asked-for
// tags and nothing else changed, or that adds a second declaration of them.
var errTagsShape = errors.New("the tags are not written as plain keys under [tags]; change them by hand")

// WriteTags sets each of set in the [tags] table of a project file and
// removes each key of remove, line by line. Comments stay, and so does every
// other value: a write that would change one is refused, such as a removal of
// a dotted key, and so is one that adds a problem, and one that adds a
// [tags] table beside dotted keys. Tags that are already as asked are not
// written. The blank lines at the end of the file are not kept.
func WriteTags(file string, set []core.Tag, remove []string, inh Inherited) (core.Project, error) {
	for _, t := range set {
		if err := ValidateTagKey(t.Key); err != nil {
			return core.Project{}, err
		}
	}
	for _, key := range remove {
		if err := ValidateTagKey(key); err != nil {
			return core.Project{}, err
		}
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return core.Project{}, err
	}
	before, err := tagsDoc(string(data))
	if err != nil {
		return core.Project{}, fmt.Errorf("%s: %w", file, err)
	}
	want, tags := maps.Clone(before), map[string]any{}
	if value, ok := before["tags"]; ok {
		table, ok := value.(map[string]any)
		if !ok {
			return core.Project{}, fmt.Errorf("%s: %w", file, errTagsShape)
		}
		tags = maps.Clone(table)
	}
	literals := make([]string, len(set))
	for i, t := range set {
		tags[t.Key] = t.Value
		if literals[i], err = literalOf(t.Value); err != nil {
			return core.Project{}, fmt.Errorf("tag %q: %w", t.Key, err)
		}
	}
	for _, key := range remove {
		delete(tags, key)
	}
	want["tags"] = tags
	if len(tags) == 0 {
		delete(want, "tags")
	}
	if reflect.DeepEqual(before, want) {
		return loadProject(file, data, inh), nil
	}
	change := func(lines []string, _ projectFile) ([]string, error) {
		for i, t := range set {
			lines = putTag(lines, t.Key, literals[i])
		}
		for _, key := range remove {
			lines = dropTag(lines, key)
		}
		text := strings.Join(lines, "\n")
		if after, err := tagsDoc(text); err != nil || !reflect.DeepEqual(after, want) || addsSecondTable(string(data), text, "tags") {
			return nil, errTagsShape
		}
		return lines, nil
	}
	return editProject(file, inh, change, nil)
}

// tagsDoc is a project file's values, an empty [tags] as none.
func tagsDoc(text string) (map[string]any, error) {
	var doc map[string]any
	if _, err := toml.Decode(text, &doc); err != nil {
		return nil, err
	}
	if table, ok := doc["tags"].(map[string]any); ok && len(table) == 0 {
		delete(doc, "tags")
	}
	return doc, nil
}

// putTag puts key = literal under [tags], adding the table at the end of the
// file when it has none.
func putTag(lines []string, key, literal string) []string {
	for _, t := range tables(lines) {
		if !t.array && t.name == "tags" {
			return putKey(lines, t, key, literal)
		}
	}
	end := len(lines)
	for end > 0 && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	out := lines[:end:end]
	if end > 0 {
		out = append(out, "")
	}
	return append(out, "[tags]", key+" = "+literal, "")
}

// dropTag removes key from [tags], and the table with it when nothing but
// blank lines and comments is left under it; a table at the end of the file
// takes every blank line before it, the one putTag wrote among them. A key
// the editor does not change, a quoted or a dotted one, keeps the table:
// without its header it would land in the table above.
func dropTag(lines []string, key string) []string {
	for _, t := range tables(lines) {
		if t.array || t.name != "tags" {
			continue
		}
		lines = deleteKey(lines, t, key)
		for _, t := range tables(lines) {
			if t.array || t.name != "tags" {
				continue
			}
			for i := t.header + 1; i < t.end; i++ {
				if text := strings.TrimSpace(lines[i]); text != "" && !strings.HasPrefix(text, "#") {
					return lines
				}
			}
			last := t.end == len(lines)
			lines = dropLines(lines, t.header, t.header+1)
			if last {
				end := len(lines)
				for end > 0 && strings.TrimSpace(lines[end-1]) == "" {
					end--
				}
				lines = append(lines[:end], "")
			}
			return lines
		}
	}
	return lines
}
