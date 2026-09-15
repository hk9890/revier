package tui

import (
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/hk9890/revier/internal/config"
)

// The help screen: every key the surface answers to, in one place. The footer
// names only the keys of the focus and the row under the cursor, and cuts the
// rest on a narrow terminal, so a key used once a week is on no screen when it
// is wanted. The screen lists them all, grouped by what they act on, and reads
// each from where it is declared - the key map, the bar, the query's editing
// keys, the target vocabulary - so the list cannot drift from what a press
// does.

// helpEntry is one line of the screen: the key as the footer spells it, and
// what it does.
type helpEntry struct {
	key  string
	desc string
}

type helpSection struct {
	title   string
	entries []helpEntry
}

// openHelp is the "help" button and alt+h. The cursor comes back to the list
// first, so the surface the screen stands over is the one it is left on.
func (m Model) openHelp() (tea.Model, tea.Cmd) {
	m.err = nil
	m.toList()
	m.dialog = dialogHelp
	m.body.SetYOffset(0)
	return m, nil
}

// helpScreenKey is every press on the help screen. The screen only reads, so
// a press scrolls it or leaves it. The key that opened it closes it too.
func (m Model) helpScreenKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, m.keys.Back), msg.String() == helpBarKey:
		m.dialog = dialogNone
		m.layout()
	case key.Matches(msg, m.keys.Up):
		m.body.ScrollUp(1)
	case key.Matches(msg, m.keys.Down):
		m.body.ScrollDown(1)
	}
	return m, nil
}

// helpBarKey is the help button's key, named so the screen can close on it.
const helpBarKey = "alt+h"

// helpSections is the screen's content, in the order a new user needs it:
// moving through the list, then what the row and the bar reach, then the keys
// the configuration adds.
func (m Model) helpSections() []helpSection {
	k := m.keys
	sections := []helpSection{
		{title: "Projects", entries: []helpEntry{
			{key: "↑ / ctrl+p", desc: "up"},
			{key: "↓ / ctrl+n", desc: "down"},
			{key: "enter", desc: "open the project"},
			{key: "type", desc: "filter the projects"},
			{key: "esc", desc: "clear the filter, or quit"},
			{key: k.Quit.Help().Key, desc: "quit"},
		}},
		{title: "Sections", entries: []helpEntry{
			{key: k.Next.Help().Key, desc: "next: projects, targets, agents"},
			{key: k.Prev.Help().Key, desc: "previous"},
		}},
		{title: "Targets", entries: []helpEntry{
			{key: "enter", desc: "go to the target"},
			{key: "type", desc: "filter the targets"},
			{key: "esc", desc: "clear the filter, or back to the projects"},
		}},
		{title: "Agents", entries: []helpEntry{
			{key: "enter", desc: "go to the agent's tab"},
			{key: "type", desc: "filter the agents"},
			{key: "esc", desc: "clear the filter, or back to the projects"},
		}},
		{title: "Selected project", entries: []helpEntry{
			{key: k.Edit.Help().Key, desc: "edit its project file"},
			{key: k.Delete.Help().Key, desc: "delete its project file"},
		}},
		{title: "Top bar", entries: barHelpEntries()},
		{title: "Sessions", entries: []helpEntry{
			{key: "enter", desc: "restore the session: open what it recorded and is not running"},
			{key: sessionsBarKey, desc: "save the projects open now, under an optional name"},
			{key: "esc", desc: "back to the projects"},
		}},
		{title: "Query editing", entries: []helpEntry{
			{key: "ctrl+a", desc: "start of the line"},
			{key: "ctrl+e", desc: "end of the line"},
			{key: "ctrl+w / alt+backspace", desc: "delete a word"},
			{key: "ctrl+u", desc: "delete the line"},
		}},
	}
	if targets := m.targetHelpEntries(); len(targets) > 0 {
		sections = append(sections, helpSection{title: "Target keys", entries: targets})
	}
	if len(k.actions) > 0 {
		actions := helpSection{title: "Actions"}
		for _, b := range k.actions {
			actions.entries = append(actions.entries, helpEntry{key: b.Help().Key, desc: b.Help().Desc})
		}
		sections = append(sections, actions)
	}
	return append(sections, helpSection{title: "Desktop", entries: m.desktopHelpEntries()})
}

func barHelpEntries() []helpEntry {
	descs := map[string]string{
		"new":      "add a project on this machine",
		"remote":   "link a project on another machine",
		"sessions": "save and restore the set of open projects",
		"config":   "change the configuration",
		"help":     "show this screen",
	}
	out := make([]helpEntry, 0, len(barActions))
	for _, a := range barActions {
		out = append(out, helpEntry{key: a.key, desc: descs[a.label]})
	}
	return out
}

// targetHelpEntries is the target keys the surface binds, over every project.
// A desktop key the terminal delivers as another chord is shown as the chord
// that reaches the surface, because that is the key that does it here.
func (m Model) targetHelpEntries() []helpEntry {
	out := make([]helpEntry, 0, len(m.tkeys))
	for c, name := range m.tkeys {
		out = append(out, helpEntry{key: string(c), desc: "go to " + string(name) + " of the selected project"})
	}
	sortEntries(out)
	return out
}

// desktopHelpEntries is the keys the desktop carries rather than the surface:
// the one that opens revier, and every target's. `revier keys install` puts
// them on the desktop, so on a desktop where it has not run they do nothing.
func (m Model) desktopHelpEntries() []helpEntry {
	seen := map[helpEntry]bool{}
	var targets []helpEntry
	for _, p := range m.projects {
		for _, t := range p.Targets {
			c, ok := chordName(t.Key)
			if !ok {
				continue
			}
			e := helpEntry{key: string(c), desc: "go to " + string(t.Name)}
			if !seen[e] {
				seen[e] = true
				targets = append(targets, e)
			}
		}
	}
	sortEntries(targets)
	// Read from [ui] as it is now: the config screen changes the trigger key
	// while the surface runs. config.Load and the screen both refuse a key
	// that does not parse.
	trigger, _ := (&config.Config{UI: m.ui}).TriggerKey()
	return append([]helpEntry{{key: string(trigger), desc: "open revier"}}, targets...)
}

// sortEntries orders by what a key does, so the target named first reads
// first, and by key where two keys do the same thing.
func sortEntries(es []helpEntry) {
	sort.Slice(es, func(i, j int) bool {
		if es[i].desc != es[j].desc {
			return es[i].desc < es[j].desc
		}
		return es[i].key < es[j].key
	})
}

// helpScreen is what stands in the list's place while the screen is up. One
// key column is shared by every section, so the descriptions line up down the
// whole screen and it reads as one table.
func (m Model) helpScreen() string {
	th := m.theme
	w := m.listWidth()
	sections := m.helpSections()
	keyWidth := 0
	for _, s := range sections {
		for _, e := range s.entries {
			keyWidth = max(keyWidth, lipgloss.Width(e.key))
		}
	}
	var lines []string
	for i, s := range sections {
		if i > 0 {
			lines = append(lines, "")
		}
		rule := max(w-lipgloss.Width(s.title)-3, 0)
		lines = append(lines, clipTo(" "+th.Heading.Render(s.title)+" "+th.Border.Render(strings.Repeat("─", rule)), w))
		for _, e := range s.entries {
			lines = append(lines, clipTo("   "+th.Accent.Render(pad(e.key, keyWidth))+"  "+th.Help.Render(e.desc), w))
		}
	}
	return strings.Join(lines, "\n")
}
