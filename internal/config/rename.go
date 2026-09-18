package config

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/pkg/revier"
)

// Rename gives a project a new name by moving its file, since the file name is
// the name (decisions.md D80), and loads it back under the new one. A link
// that leaves its name on the host to be derived from its own gets that name
// written first, so the link still reaches the project it reached. An
// existing file is never overwritten, and the old file is removed only once
// the new one loads no worse than it did: what was wrong with the file before
// is not the rename's to refuse.
func Rename(root string, from, to revier.ProjectName, shared []map[string]any) (core.Project, error) {
	old := ProjectFile(root, from)
	if info, err := os.Lstat(old); err == nil && info.Mode()&os.ModeSymlink != 0 {
		// Moved here, the file would leave the repository it is linked in
		// from; the write paths keep such a link, and a rename cannot.
		target, _ := filepath.EvalSymlinks(old)
		return core.Project{}, fmt.Errorf("%s is a link to %s; rename that file, and the link, by hand", old, target)
	}
	data, err := os.ReadFile(old)
	if err != nil {
		return core.Project{}, err
	}
	declared, _, err := decodeProject(data, nil)
	if err != nil {
		return core.Project{}, fmt.Errorf("%s: %w", old, err)
	}
	if target, value, found := writesName(declared.Targets, from); found {
		return core.Project{}, fmt.Errorf("target %q writes the name out as %q beside {{.Name}}; make it {{.Name}} there first, or the target would look for %q after the rename", target, value, from)
	}
	text := string(data)
	if declared.Remote != nil && declared.Remote.Project == "" {
		text = setKey(text, "remote", "project", quote(string(from)))
	}
	before := loadProject(old, data, shared)
	p, err := writeUnless(root, to, text, shared, func(p core.Project) []error { return newProblems(before, p) })
	if err != nil {
		return core.Project{}, err
	}
	if err := os.Remove(old); err != nil {
		_ = os.Remove(p.File)
		return core.Project{}, err
	}
	slog.Info("project renamed", "from", from, "to", to)
	return p, nil
}

// nameTemplate is the template that renders the project's name.
var nameTemplate = regexp.MustCompile(`\{\{\s*\.Name\b`)

// writesName finds a realization that writes the project's name out beside
// {{.Name}}: `revier new` writes the name quoted into a match pattern where
// a regexp would read it, such as the dot in "example.com", under a name
// that renders the template. A rename leaves such a pattern looking for the
// old name while the name follows, so Open would stop producing what Match
// finds (docs/CODING.md), and the rename is refused instead. A realization
// that writes the name out everywhere stays consistent with itself, and a
// launch or a panel command is nothing Match reads.
func writesName(targets []revier.Target, name revier.ProjectName) (revier.TargetName, string, bool) {
	forms := []string{string(name), regexp.QuoteMeta(string(name))}
	for _, t := range targets {
		for _, r := range []*revier.Realization{t.Runtime, t.Window} {
			if r == nil {
				continue
			}
			literal, templated := "", false
			for _, v := range []string{r.Name, r.Match.Title, r.Match.Class} {
				if nameTemplate.MatchString(v) {
					templated = true
					continue
				}
				for _, form := range forms {
					if literal == "" && strings.Contains(v, form) {
						literal = v
					}
				}
			}
			if templated && literal != "" {
				return t.Name, literal, true
			}
		}
	}
	return "", "", false
}
