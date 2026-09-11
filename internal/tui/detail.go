package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"

	"github.com/hk9890/revier/internal/theme"
	"github.com/hk9890/revier/pkg/revier"
)

// The pane's width bounds, and the least the list keeps beside it. The list
// is what the surface is for, so the pane takes what is left over and not the
// other way round: at ninety columns a half-and-half split cut every path in
// the list to make room for a pane that wrapped every line of its own.
const (
	minPaneWidth = 44
	maxPaneWidth = 90
	minListWidth = 56
)

// paneWidth is what the detail pane gets, or zero when the terminal is too
// narrow to give both the list and the pane their least.
func (m Model) paneWidth() int {
	// Half, as the picker gives its preview 55% (os-fzf.sh:782). A fixed cap
	// left the pane at 28% of a 200-column terminal, which is where the paths
	// and the tree it holds are longest.
	w := m.width / 2
	if w > maxPaneWidth {
		w = maxPaneWidth
	}
	inner, _ := m.inner()
	if spare := inner - minListWidth; w > spare {
		w = spare
	}
	if w < minPaneWidth {
		return 0
	}
	return w
}

// paneChrome is the border column and the padding column the pane's frame
// takes from what it holds.
const paneChrome = 2

func newDetail(th theme.Theme) viewport.Model {
	v := viewport.New(0, 0)
	v.Style = th.Border.
		Border(lipgloss.NormalBorder(), false, false, false, true).
		PaddingLeft(1)
	return v
}

// syncDetail rebuilds the pane for whatever the cursor is on. It runs after
// every message, because the cursor moves on a keypress and the content
// changes on a survey. The wheel scrolls the pane; a survey keeps that
// scroll, and a different project starts at its top.
func (m *Model) syncDetail() {
	if m.paneWidth() == 0 {
		return
	}
	v, ok := m.selected()
	if !ok {
		m.detail.SetContent("")
		return
	}
	m.detail.SetContent(m.detailContent(v))
	if v.Project.Name != m.shown {
		m.shown = v.Project.Name
		m.detail.GotoTop()
	}
}

// detailContent is what the shell picker's preview shows, in its order
// (os-fzf.sh:290): what this project is, then what is up, then what the
// agents are doing.
//
// A path and an activity line wrap, as the preview does (os-fzf.sh:788,
// --preview-window=...,wrap): they are the fields worth reading whole, and a
// cut takes exactly the end that says which checkout or which step. Tree rows
// are cut instead, because a wrapped tree row loses its indentation.
func (m *Model) detailContent(v revier.ProjectView) string {
	th := m.theme
	w := m.paneWidth() - paneChrome
	var b strings.Builder

	line := func(label, value string, style lipgloss.Style) {
		b.WriteString(hang(th.Meta.Render(pad(label, detailLabelWidth)), value, w, style))
		b.WriteString("\n")
	}

	b.WriteString(th.Header.Render(clipTo(string(v.Project.Name), w)))
	b.WriteString("\n")

	status, style := "stopped", th.NameDim
	switch {
	case v.Running:
		status, style = "running", th.Running
	case !v.PathExists:
		status, style = "not available", th.PathMissing
	}
	line("Status", status, style)

	pathStyle := th.Path
	if !v.PathExists {
		pathStyle = th.PathMissing
	}
	line("Path", contractHome(v.Project.Path), pathStyle)
	if v.Project.GitURL != "" {
		line("Git URL", v.Project.GitURL, th.Path)
	}
	// Only worth saying when it is the reason nothing can start. A running
	// project whose directory has since gone is a different problem, and the
	// red path already says it.
	if !v.PathExists && !v.Running {
		b.WriteString(th.PathMissing.Render(clipTo("Directory is not on this machine", w)))
		b.WriteString("\n")
		// What Enter does about it, as the picker's preview says
		// (os-fzf.sh:326, :338).
		if v.Project.GitURL != "" {
			b.WriteString(th.Meta.Render(clipTo("Enter: clone and open", w)))
		} else {
			b.WriteString(th.PathMissing.Render(clipTo("No git_url recorded to clone it from", w)))
		}
		b.WriteString("\n")
	}

	b.WriteString(m.heading("Targets", w))
	for _, t := range v.Targets {
		b.WriteString(m.detailTarget(t, w))
		b.WriteString("\n")
	}
	for _, ref := range m.attached[v.Project.Name] {
		b.WriteString(th.NameDim.Render(th.Glyphs.Running + " "))
		b.WriteString(th.ProjectName.Render(clipTo(ref.Title, w-12)))
		b.WriteString(th.Meta.Render(" attached"))
		b.WriteString("\n")
	}

	// Every agent, not the worst one the row collapses to: a project with two
	// agents is exactly where the row is not enough.
	if len(v.Agents) > 0 {
		b.WriteString(m.heading("Agents", w))
		for _, a := range v.Agents {
			b.WriteString(m.detailAgent(a, w))
			b.WriteString("\n")
		}
	}

	// What the directory holds. With ninety near-identical names this is what
	// says which checkout the cursor is on.
	if v.PathExists {
		if tree := m.treeFor(v.Project.Path); len(tree) > 0 {
			b.WriteString(m.heading("Project Snapshot", w))
			for _, line := range tree {
				b.WriteString(th.Path.Render(clipTo(line, w)))
				b.WriteString("\n")
			}
		}
	}
	return b.String()
}

// heading opens a section of the pane: a blank line, then the title with a
// rule to the pane's edge, so the sections read as blocks rather than as a
// list of lines that happens to change colour.
func (m Model) heading(title string, w int) string {
	th := m.theme
	rule := w - lipgloss.Width(title) - 1
	if rule < 0 {
		rule = 0
	}
	return "\n" + th.Heading.Render(title) + " " + th.Border.Render(strings.Repeat("─", rule)) + "\n"
}

// detailTarget is one target: whether it is up, its name, its key in the
// spelling the footer uses, and its state. A stopped target says "stopped",
// where it said "-", which read as a value that failed to load.
func (m Model) detailTarget(t revier.TargetView, w int) string {
	th := m.theme
	mark, markStyle := th.Glyphs.Stopped, th.NameDim
	state, stateStyle := "stopped", th.Count
	switch {
	case !t.Available:
		state = "no host here"
	case !t.Ref.IsZero():
		mark, markStyle = th.Glyphs.Running, th.Running
		state, stateStyle = "running", th.Running
	}
	name := th.ProjectName
	if t.Ref.IsZero() {
		name = th.NameDim
	}
	return markStyle.Render(mark+" ") +
		name.Render(pad(clipTo(string(t.Name), detailNameWidth-1), detailNameWidth)) +
		th.Accent.Render(pad(clipTo(keyLabel(t.Key), detailKeyWidth-1), detailKeyWidth)) +
		stateStyle.Render(ellipsis(state, w-detailNameWidth-detailKeyWidth-2))
}

func (m Model) detailAgent(a revier.AgentView, w int) string {
	th := m.theme
	harness := a.State.Harness
	if harness == "" {
		harness = "agent"
	}
	// Indented past the targets' mark column, so the harness sits under the
	// target names; the state glyph is in the label after it.
	head := "  " + th.ProjectName.Render(pad(harness, detailNameWidth)) +
		statusStyle(th, a.State.Status).Render(pad(statusLabel(th, a.State.Status), detailStateWidth))
	return hang(head, a.State.Activity, w, th.Path)
}

// The pane's columns. Narrower than the list's, because the pane is. An
// agent's state column fits its widest label, "◆ needs you", and no more, so
// the activity after it has the room: it is the part of that line worth
// reading.
const (
	detailLabelWidth = 9
	detailNameWidth  = 10
	detailKeyWidth   = 16
	detailStateWidth = 13
)
