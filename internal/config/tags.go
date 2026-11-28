package config

import (
	"fmt"
	"strings"

	"github.com/BurntSushi/toml"
)

// projectTags is the [tags] table of a project file, and why each tag left
// out of it is. A tag is left out alone when a selector could not name it: a
// value that is not a string, or a key that is empty or holds the "=" that
// `-t key=value` splits at. The rest of the project loads either way, since
// a tag only selects.
func projectTags(data []byte) (map[string]string, []error) {
	var file struct {
		Tags map[string]any `toml:"tags"`
	}
	if _, err := toml.Decode(string(data), &file); err != nil {
		return nil, []error{fmt.Errorf("tags: %w", err)}
	}
	var refused []error
	tags := make(map[string]string, len(file.Tags))
	for key, value := range file.Tags {
		s, ok := value.(string)
		switch {
		case key == "":
			refused = append(refused, fmt.Errorf("tag %q: the key is empty, so -t cannot select it; the tag is left out", key))
		case strings.Contains(key, "="):
			refused = append(refused, fmt.Errorf("tag %q: the key holds \"=\", where -t key=value splits; the tag is left out", key))
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
