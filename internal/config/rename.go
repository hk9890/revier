package config

import (
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/pkg/revier"
)

// Rename gives a project a new name by moving its file, since the file name is
// the name (decisions.md D80), and loads it back under the new one. A link
// that leaves its name on the host to be derived from its own gets that name
// written first, so the link still reaches the project it reached. An
// existing file is never overwritten, and the old file is removed only once
// the new one loads.
func Rename(root string, from, to revier.ProjectName, shared []map[string]any) (core.Project, error) {
	old := ProjectFile(root, from)
	data, err := os.ReadFile(old)
	if err != nil {
		return core.Project{}, err
	}
	declared, err := decodeProject(data, nil)
	if err != nil {
		return core.Project{}, fmt.Errorf("%s: %w", old, err)
	}
	if target, value, found := writesName(declared.Targets, from); found {
		return core.Project{}, fmt.Errorf("target %q writes the name out as %q; make it {{.Name}} there first, or the target would look for %q after the rename", target, value, from)
	}
	text, err := keepRemoteProject(string(data), from)
	if err != nil {
		return core.Project{}, fmt.Errorf("%s: %w", old, err)
	}
	p, err := write(root, to, text, shared)
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

// writesName finds a target value that carries the project's name as text
// rather than as {{.Name}}: `revier new` writes one into a match pattern for
// a name a regexp would read, such as the dot in "example.com", and a file
// written by hand may hold others. A rename leaves such a value looking for
// the old name, so Open would stop producing what Match finds
// (docs/CODING.md), and the rename is refused instead.
func writesName(targets []revier.Target, name revier.ProjectName) (revier.TargetName, string, bool) {
	forms := []string{string(name), regexp.QuoteMeta(string(name))}
	for _, t := range targets {
		for _, r := range []*revier.Realization{t.Runtime, t.Window} {
			if r == nil {
				continue
			}
			values := []string{r.Name, r.Dir, r.Place, r.Match.Title, r.Match.Class}
			values = append(values, r.Launch...)
			for _, p := range r.Panels {
				values = append(values, p.Title)
				values = append(values, p.Command...)
			}
			for _, v := range values {
				for _, form := range forms {
					if strings.Contains(v, form) {
						return t.Name, v, true
					}
				}
			}
		}
	}
	return "", "", false
}

// keepRemoteProject writes a link's name on the host where the file leaves it
// to default to the link's name, which is about to change.
func keepRemoteProject(text string, name revier.ProjectName) (string, error) {
	var doc struct {
		Remote *revier.Link `toml:"remote"`
	}
	if _, err := toml.Decode(text, &doc); err != nil {
		return "", err
	}
	if doc.Remote == nil || doc.Remote.Project != "" {
		return text, nil
	}
	return setKey(text, "remote", "project", quote(string(name))), nil
}
