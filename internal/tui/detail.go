package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"

	"github.com/hk9890/revier/internal/theme"
	"github.com/hk9890/revier/pkg/revier"
)

// The pane's width, and the width below which there is no pane at all. A
// narrow terminal gets the list and nothing else: half of forty columns is
// two columns of text and two of border.
const (
	minSplitWidth = 90
	minPaneWidth  = 36
	maxPaneWidth  = 90
)

// paneWidth is what the detail pane gets, or zero when the terminal is too
// narrow to split.
func (m Model) paneWidth() int {
	if m.width < minSplitWidth {
		return 0
	}
	// Half, as the picker gives its preview 55% (os-fzf.sh:782). A fixed cap
	// left the pane at 28% of a 200-column terminal, which is where the paths
	// and the tree it holds are longest.
	w := m.width / 2
	if w > maxPaneWidth {
		w = maxPaneWidth
	}
	if w < minPaneWidth {
		w = minPaneWidth
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
// changes on a survey.
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
	b.WriteString("\n\n")

	status, statusStyle := "stopped", th.NameDim
	switch {
	case v.Running:
		status, statusStyle = "running", th.Running
	case !v.PathExists:
		status, statusStyle = "not available", th.PathMissing
	}
	line("Status", status, statusStyle)

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

	b.WriteString("\n")
	b.WriteString(th.Meta.Render("Targets"))
	b.WriteString("\n")
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
		b.WriteString("\n")
		b.WriteString(th.Meta.Render("Agents"))
		b.WriteString("\n")
		for _, a := range v.Agents {
			b.WriteString(m.detailAgent(a, w))
			b.WriteString("\n")
		}
	}

	// What the directory holds. With ninety near-identical names this is what
	// says which checkout the cursor is on.
	if v.PathExists {
		if tree := m.treeFor(v.Project.Path); len(tree) > 0 {
			b.WriteString("\n")
			b.WriteString(th.Meta.Render("Project Snapshot"))
			b.WriteString("\n")
			for _, line := range tree {
				b.WriteString(th.Path.Render(clipTo(line, w)))
				b.WriteString("\n")
			}
		}
	}
	return b.String()
}

func (m Model) detailTarget(t revier.TargetView, w int) string {
	th := m.theme
	mark, markStyle := th.Glyphs.Stopped, th.NameDim
	state, stateStyle := "-", th.NameDim
	switch {
	case !t.Available:
		state = "unavailable"
	case !t.Ref.IsZero():
		mark, markStyle = th.Glyphs.Running, th.Running
		state, stateStyle = "running", th.Running
	}
	name := th.ProjectName
	if t.Ref.IsZero() {
		name = th.NameDim
	}
	return markStyle.Render(mark+" ") +
		name.Render(pad(string(t.Name), detailNameWidth)) +
		th.Accent.Render(pad(t.Key, detailKeyWidth)) +
		stateStyle.Render(truncate(state, w-detailNameWidth-detailKeyWidth-2))
}

func (m Model) detailAgent(a revier.AgentView, w int) string {
	th := m.theme
	var s lipgloss.Style
	switch a.State.Status {
	case revier.StatusAttention:
		s = th.Attention
	case revier.StatusRunning:
		s = th.Running
	case revier.StatusIdle:
		s = th.Idle
	default:
		s = th.NameDim
	}
	harness := a.State.Harness
	if harness == "" {
		harness = "agent"
	}
	head := th.ProjectName.Render(pad(harness, detailNameWidth)) +
		s.Render(pad(a.State.Status.String(), detailKeyWidth))
	return hang(head, a.State.Activity, w, th.Path)
}

// The pane's columns. Narrower than the list's, because the pane is.
const (
	detailLabelWidth = 9
	detailNameWidth  = 10
	detailKeyWidth   = 15
)
