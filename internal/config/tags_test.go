package config_test

import (
	"maps"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/config"
)

const taggedHome = `path = "/tmp/demo"
[[target]]
name = "home"
home = true
  [target.runtime]
  name = "session:{{.Name}}"
  match = { title = "^session:{{.Name}}$" }
    [[target.runtime.panels]]
    kind = "shell"
`

// A tag a selector cannot name is left out alone, with its reason among the
// project's problems: the project and its other tags load.
func TestATagThatCannotBeSelectedIsLeftOutAlone(t *testing.T) {
	_, p := loadOne(t, "", taggedHome+`
[tags]
type = "work"
year = 2024
active = true
"a=b" = "c"
"" = "x"
`)
	if p.Invalid != nil {
		t.Fatalf("a bad tag made the project invalid: %v", p.Invalid)
	}
	if want := map[string]string{"type": "work"}; !maps.Equal(p.Tags, want) {
		t.Errorf("Tags = %v, want %v", p.Tags, want)
	}
	if len(p.TagRefused) != 4 {
		t.Fatalf("TagRefused = %v, want one per refused tag", p.TagRefused)
	}
	problems := config.Problems(p)
	var all strings.Builder
	for _, err := range problems {
		all.WriteString(err.Error() + "\n")
	}
	for _, want := range []string{`tag "year": the value is int64, not a string`, `tag "active"`, `tag "a=b": the key holds "="`, `tag "": the key is empty`} {
		if !strings.Contains(all.String(), want) {
			t.Errorf("problems do not say %q:\n%s", want, all.String())
		}
	}
}

// A link's tags load from its own file, with the shared targets merged.
func TestALinkKeepsItsTags(t *testing.T) {
	_, p := loadOne(t, "", "[remote]\nhost = \"far\"\n[tags]\ntype = \"work\"\n")
	if p.Tags["type"] != "work" || len(config.Problems(p)) != 0 {
		t.Errorf("Tags = %v, problems %v; want type=work and none", p.Tags, config.Problems(p))
	}
}
