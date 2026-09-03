package tui

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/pkg/revier"
)

// targetKeys is the key vocabulary: every key any project binds, and the
// target name it means. A target key means the same thing in every project -
// `ctrl-shift-o` is "editor" everywhere - so it is resolved against every
// project and not only the highlighted one. That is what lets a press on a
// project with no editor say so instead of doing nothing.
func targetKeys(projects []core.Project) map[string]revier.TargetName {
	out := map[string]revier.TargetName{}
	for _, p := range projects {
		for _, t := range p.Targets {
			if t.Key == "" {
				continue
			}
			if k := keyName(t.Key); isChord(k) {
				out[k] = t.Name
			}
		}
	}
	return out
}

// isChord reports whether a key name carries a modifier. A bare letter is a
// filter character at the project level, and filtering is the primary way
// through ninety projects: a target bound to `o` must not swallow the `o` of
// `opencode`.
func isChord(k string) bool {
	return strings.Contains(k, "+") || len([]rune(k)) > 1
}

// targetKey runs the target a chord means against the highlighted project.
// The second return says whether the key was one; a key no project binds is
// not handled here.
func (m Model) targetKey(msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	name, ok := m.tkeys[msg.String()]
	if !ok {
		return m, nil, false
	}
	v, ok := m.selected()
	if !ok {
		return m, nil, true
	}
	p, ok := m.project(v.Project.Name)
	if !ok {
		return m, nil, true
	}
	for _, t := range p.Targets {
		if t.Name == name {
			return m, m.goTarget(p, name), true
		}
	}
	m.err = fmt.Errorf("%s has no %s target", p.Name, name)
	return m, nil, true
}

// targetKeysOf is the highlighted project's own chords, for the footer. They
// are the keys that will do something on this row.
func (m Model) targetKeysOf(v revier.ProjectView) []targetKeyHelp {
	var out []targetKeyHelp
	for _, t := range v.Targets {
		if t.Key == "" {
			continue
		}
		if k := keyName(t.Key); isChord(k) {
			out = append(out, targetKeyHelp{key: k, name: string(t.Name)})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out
}

type targetKeyHelp struct {
	key  string
	name string
}
