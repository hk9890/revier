package config

import (
	"fmt"
	"log/slog"
	"os"

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
