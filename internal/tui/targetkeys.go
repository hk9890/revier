package tui

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/pkg/revier"
)

// pressed is a keypress as a canonical chord, the form every configured key
// is compared in. bubbletea names a press its own way - alt first, as
// `alt+ctrl+o`, and alt+shift+o as `alt+O` - so its name is parsed rather
// than compared as text. A paste, or several runes at once, is typed text and
// no chord.
func pressed(msg tea.KeyMsg) (core.Chord, bool) {
	name := msg.String()
	switch msg.Type {
	case tea.KeyRunes:
		if msg.Paste || len(msg.Runes) != 1 {
			return "", false
		}
		if r := msg.Runes[0]; msg.Alt && unicode.IsUpper(r) {
			name = "alt+shift+" + string(unicode.ToLower(r))
		}
	case tea.KeySpace:
		name = strings.TrimSuffix(name, " ") + "space"
	}
	c, err := core.ParseChord(name)
	return c, err == nil
}

// targetKeys is the key vocabulary: what each press the terminal can deliver
// means, over every project. A target key means the same thing in every
// project - `ctrl-shift-o` is "editor" everywhere - so it is resolved against
// every project and not only the highlighted one. That is what lets a press on
// a project with no editor say so instead of doing nothing.
//
// A target key is a desktop key first, and the terminal does not deliver
// every chord the desktop does (core.Chord.Terminal). ctrl+shift+o reaches the
// surface as ctrl+o, so it is bound under ctrl+o - pressing the desktop key
// inside the surface then does what it does outside - unless ctrl+o already
// means something here: one of the surface's own keys, an action, a query
// editing key, or a target that declares ctrl+o itself. Then it binds nothing
// here and stays a desktop key only.
func targetKeys(projects []core.Project, keys keyMap) map[core.Chord]revier.TargetName {
	type folded struct {
		sent core.Chord
		name revier.TargetName
	}
	out := map[core.Chord]revier.TargetName{}
	var late []folded
	for _, p := range projects {
		for _, t := range p.Targets {
			c, ok := chordName(t.Key)
			if !ok {
				continue
			}
			sent, ok := c.Terminal()
			switch {
			case !ok || keys.claims(sent):
			case sent == c:
				out[c] = t.Name
			default:
				late = append(late, folded{sent, t.Name})
			}
		}
	}
	for _, f := range late {
		if _, taken := out[f.sent]; !taken && !slices.Contains(queryKeys, string(f.sent)) {
			out[f.sent] = f.name
		}
	}
	return out
}

// keyLabel is a target's key as the footer spells it, so the row, the pane
// and the legend agree. A key that does not parse is shown as written.
func keyLabel(key string) string {
	if c, err := core.ParseChord(key); err == nil {
		return string(c)
	}
	return key
}

// chordName is the key a target declares, in canonical form. A key that does
// not parse binds nothing here; config.Validate has already refused it at
// load, so this is the hand-built case only. Neither does a key typed as
// text: filtering is the primary way through ninety projects, and a target
// bound to `o` must not swallow the `o` of `opencode`.
func chordName(key string) (core.Chord, bool) {
	if key == "" {
		return "", false
	}
	c, err := core.ParseChord(key)
	if err != nil {
		return "", false
	}
	return c, !c.Typed()
}

// targetKey runs the target a chord means against the highlighted project.
// The second return says whether the key was one; a key no project binds is
// not handled here.
func (m Model) targetKey(msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	c, ok := pressed(msg)
	if !ok {
		return m, nil, false
	}
	name, ok := m.tkeys[c]
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
			next, cmd := opened(m, m.goTarget(p, name))
			return next, cmd, true
		}
	}
	m.err = fmt.Errorf("%s has no %s target", p.Name, name)
	return m, nil, true
}

// targetKeysOf is the highlighted project's own chords, for the footer. They
// are the keys that will do something on this row, so a desktop-only key is
// left out: the footer would name a key that does nothing here.
func (m Model) targetKeysOf(v revier.ProjectView) []targetKeyHelp {
	var out []targetKeyHelp
	for _, t := range v.Targets {
		c, ok := chordName(t.Key)
		if !ok {
			continue
		}
		if sent, ok := c.Terminal(); ok && m.tkeys[sent] == t.Name {
			out = append(out, targetKeyHelp{key: string(c), name: string(t.Name)})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out
}

type targetKeyHelp struct {
	key  string
	name string
}
