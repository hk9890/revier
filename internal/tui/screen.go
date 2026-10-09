package tui

import (
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/theme"
	"github.com/hk9890/revier/pkg/revier"
)

// A screen standing over the surface is a struct of its own: its fields, its
// keys and what it draws. It reads the surface through a surface value and
// writes nothing of it: what a press came to goes back as the screen's result,
// and the root model applies it. A screen is therefore built and driven with
// no root model, which is how its tests run it.

// surface is what a screen reads of the surface it stands over.
type surface struct {
	theme    theme.Theme
	spun     theme.Theme // theme with the working glyph at the spinner's frame
	keys     keyMap
	core     *core.Core
	projects []core.Project
	targets  []revier.Target  // the shared targets, as config.toml holds them
	usable   []map[string]any // the shared targets a project file gets
	err      error            // what the footer shows now, for a press that leaves it
	list     int              // the columns the list has
	pane     int              // the columns the pane's text has
	page     int              // the list rows on the screen at once
}

func (m Model) surface() surface {
	return surface{
		theme: m.theme, spun: m.spun(), keys: m.keys, core: m.core, projects: m.projects, targets: m.targets, usable: m.usable, err: m.err,
		list: m.listWidth(), pane: m.paneCols() - paneChrome, page: m.listPage(),
	}
}

// projectNamed is the project of that name among projects.
func projectNamed(projects []core.Project, name revier.ProjectName) (core.Project, bool) {
	for _, p := range projects {
		if p.Name == name {
			return p, true
		}
	}
	return core.Project{}, false
}
