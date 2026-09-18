package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/pkg/revier"
)

// NameFor is the project name a directory gets when none is given: its base
// name, with the spaces and colons the shell tool also replaces
// (session_get_name) turned into underscores.
func NameFor(dir string) revier.ProjectName {
	return revier.ProjectName(strings.Map(func(r rune) rune {
		if r == ':' || unicode.IsSpace(r) {
			return '_'
		}
		return r
	}, filepath.Base(dir)))
}

// ValidateName refuses a name that cannot be a project file's stem.
func ValidateName(name revier.ProjectName) error {
	n := string(name)
	switch {
	case n == "", n == ".", n == "..":
		return fmt.Errorf("project name %q is not a file name", n)
	case strings.ContainsRune(n, filepath.Separator):
		return fmt.Errorf("project name %q contains %q", n, filepath.Separator)
	case strings.IndexFunc(n, func(r rune) bool { return unicode.IsSpace(r) || !unicode.IsPrint(r) }) >= 0:
		return fmt.Errorf("project name %q contains whitespace or a control character", n)
	}
	return nil
}

// ProjectFile is where the project called name lives under a config root.
func ProjectFile(root string, name revier.ProjectName) string {
	return filepath.Join(root, "projects", string(name)+".toml")
}

// Create writes a project file for dir from the built-in template, and loads
// it back. An existing file is never overwritten, and a file that does not
// load is removed again, so Create leaves behind a project revier accepts or
// nothing at all. gitURL may be empty; a non-empty one must pass
// ValidateGitURL. When config.toml has shared targets, the file has no
// targets of its own: the project has the shared ones.
func Create(root string, name revier.ProjectName, dir, gitURL string) (core.Project, error) {
	shared, err := sharedTargets(root)
	if err != nil {
		return core.Project{}, err
	}
	return write(root, name, projectTOML(name, dir, gitURL, len(shared) == 0), shared)
}

// CreateLink writes a link file for a project on another machine
// (decisions.md D41), and loads it back as Create does. on is the project as
// the host reports it: its name there, empty for the link's own name, and
// the path and repository a target here renders (decisions.md D83).
func CreateLink(root string, name revier.ProjectName, host string, on revier.Project) (core.Project, error) {
	if err := validateHost(host); err != nil {
		return core.Project{}, fmt.Errorf("host: %w", err)
	}
	// A link gets the shared targets through their remote part (decisions.md
	// D82), so it is loaded back with them: the project handed to the caller
	// is the one the next start reads, and a shared target that would refuse
	// the link is caught here rather than after the file is written.
	shared, err := sharedTargets(root)
	if err != nil {
		return core.Project{}, err
	}
	return write(root, name, linkTOML(name, host, on), shared)
}

// write puts body under the project's file name and loads it back. An
// existing file is never overwritten, and a file that does not load is
// removed again, so the directory holds a project revier accepts or nothing.
func write(root string, name revier.ProjectName, body string, shared []map[string]any) (core.Project, error) {
	if err := ValidateName(name); err != nil {
		return core.Project{}, err
	}
	path := ProjectFile(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return core.Project{}, err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if errors.Is(err, os.ErrExist) {
		return core.Project{}, fmt.Errorf("project %q already exists: %s", name, path)
	}
	if err != nil {
		return core.Project{}, err
	}
	_, werr := f.WriteString(body)
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		_ = os.Remove(path)
		return core.Project{}, werr
	}
	p, err := LoadProject(path, shared)
	if err != nil {
		_ = os.Remove(path)
		return core.Project{}, err
	}
	slog.Info("project file written", "project", name, "path", path)
	return p, nil
}

// projectTOML is a new project: a workspace with an agent beside a shell, and
// an editor: the shape of the ninety projects converted from the shell
// .session files, so a new project looks like the ones before it.
//
// Neither target carries a key. A key means the same target in every project,
// and one written here would either repeat the user's own choice or conflict
// with it; without one, the desktop chords the other projects declare still
// reach these targets by name.
//
// With targets false the file is the project alone, for a configuration
// whose shared targets give it its targets.
func projectTOML(name revier.ProjectName, dir, gitURL string, targets bool) string {
	var b strings.Builder
	p := func(format string, args ...any) { fmt.Fprintf(&b, format, args...) }

	p("# %s: written by `revier new`.\n", name)
	p("path = %s\n", quote(contractHome(dir)))
	if gitURL != "" {
		p("git_url = %s\n", quote(gitURL))
	}
	if !targets {
		return b.String()
	}
	p("\n")

	p("[[target]]\nname = \"home\"\nhome = true\n")
	p("  [target.runtime]\n")
	p("  name = \"session:{{.Name}}\"\n")
	p("  match = { title = %s }\n", quote("^session:"+namePattern(name)+"$"))
	p("    [[target.runtime.panels]]\n")
	p("    kind = \"agent\"\n    title = \"claude\"\n    command = [\"claude\"]\n")
	p("    [[target.runtime.panels]]\n")
	p("    kind = \"shell\"\n    title = \"shell\"\n\n")

	// IntelliJ shows the project name first in its title, then " - <file>" or
	// " [<path>] - <file>"; project names carry no spaces, so a space or the
	// end of the title ends the name.
	p("[[target]]\nname = \"editor\"\n")
	p("  [target.window]\n")
	p("  launch = [\"snap\", \"run\", \"intellij-idea\", \"{{.Path}}\"]\n")
	p("  match = { class = \"^jetbrains-idea\", title = %s }\n", quote("^"+namePattern(name)+"( |$)"))
	return b.String()
}

// namePattern is the name inside a match regexp: the {{.Name}} template, or
// the name quoted literally when it holds a character a regexp would read,
// such as the dot in "example.com".
func namePattern(name revier.ProjectName) string {
	if quoted := regexp.QuoteMeta(string(name)); quoted != string(name) {
		return quoted
	}
	return "{{.Name}}"
}

// quote renders a string as a TOML basic string.
func quote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// contractHome writes a path under the home directory as ~/..., because the
// project file is shared between machines and the home directory is not.
func contractHome(p string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return p
	}
	if rest, ok := strings.CutPrefix(p, home+string(filepath.Separator)); ok {
		return "~/" + rest
	}
	return p
}
