package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"

	"github.com/hk9890/revier/internal/theme"
	"github.com/hk9890/revier/pkg/revier"
)

// The list's width bounds, the least the pane needs beside it, and the pane
// width at which the pane lays its sections side by side (decisions.md D39).
//
// The list is a table of two columns - the project, and its agent's state -
// and stops at the width that holds both whole. Past that every column goes
// to the pane, which is where a wide terminal has something to show. Below
// the least the two need together, there is no pane.
const (
	minListWidth  = 56
	maxListWidth  = 110
	minPaneWidth  = 44
	widePaneWidth = 130
	maxFactsWidth = 80
)

// paneWidth is what the detail pane gets, or zero when the terminal is too
// narrow to give both the list and the pane their least. The list takes half
// up to its cap, as the picker split its preview (os-fzf.sh:782), and the
// pane takes the rest.
func (m Model) paneWidth() int {
	inner, _ := m.inner()
	if inner < minListWidth+minPaneWidth {
		return 0
	}
	list := min(max(inner/2, minListWidth), maxListWidth)
	return inner - list
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
	switch m.level {
	case levelHosts:
		m.detail.SetContent("")
		return
	case levelRemote:
		m.detail.SetContent(m.remoteDetail())
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
// agents are doing, then what the directory holds.
//
// A narrow pane stacks the sections, and the snapshot takes the rows the
// others leave. A wide pane puts the snapshot beside the rest, so both fill
// the height and neither waits under the other (decisions.md D39).
func (m *Model) detailContent(v revier.ProjectView) string {
	w := m.paneWidth() - paneChrome
	_, h := m.inner()
	if m.paneWidth() < widePaneWidth {
		facts := m.facts(v, w)
		return facts + m.snapshot(v, w, h-strings.Count(facts, "\n"))
	}
	// The facts take half, up to what a repository URL and an agent's line
	// need whole; the snapshot takes the rest.
	left := min((w-gridGap)/2, maxFactsWidth)
	right := w - gridGap - left
	// The snapshot starts level with the name: its heading's blank line is a
	// separator from a section above it, and there is none here.
	return lipgloss.JoinHorizontal(lipgloss.Top,
		lipgloss.NewStyle().Width(left).Render(m.facts(v, left)),
		strings.Repeat(" ", gridGap),
		strings.TrimPrefix(m.snapshot(v, right, h+1), "\n"))
}

// facts is the pane's first part: what this project is, then what is up,
// then what the agents are doing.
//
// A path and an activity line wrap, as the preview does (os-fzf.sh:788,
// --preview-window=...,wrap): they are the fields worth reading whole, and a
// cut takes exactly the end that says which checkout or which step.
func (m *Model) facts(v revier.ProjectView, w int) string {
	th := m.theme
	var b strings.Builder

	line := func(label, value string, style lipgloss.Style) {
		b.WriteString(hang(th.Meta.Render(pad(label, detailLabelWidth)), value, w, style))
		b.WriteString("\n")
	}

	b.WriteString(th.Header.Render(clipTo(string(v.Project.Name), w)))
	b.WriteString("\n")

	status, style := "stopped", th.NameDim
	switch {
	case v.Unreachable != "":
		status, style = "unreachable", th.PathMissing
	case v.Running:
		status, style = "running", th.Running
	case !v.PathExists:
		status, style = "not available", th.PathMissing
	}
	line("Status", status, style)
	if v.Project.Remote != nil {
		line("Host", v.Project.Remote.Host, th.Path)
	}

	// A host that did not answer has said nothing about the checkout.
	pathStyle := th.Path
	if !v.PathExists && v.Unreachable == "" {
		pathStyle = th.PathMissing
	}
	line("Path", contractHome(v.Project.Path), pathStyle)
	if v.Project.GitURL != "" {
		line("Git URL", v.Project.GitURL, th.Path)
	}
	// A host that did not answer is said in full: the row has room for the
	// fact, and this is where the reason is read.
	if v.Unreachable != "" {
		b.WriteString(hang("", v.Unreachable, w, th.PathMissing))
		b.WriteString("\n")
	}
	// Only worth saying when it is the reason nothing can start. A running
	// project whose directory has since gone is a different problem, and the
	// red path already says it. A remote project's host clones it on open
	// (decisions.md D40), so Enter is the same answer there.
	if !v.PathExists && !v.Running && v.Unreachable == "" {
		where, clone := "this machine", "Enter: clone and open"
		if v.Project.Remote != nil {
			where, clone = v.Project.Remote.Host, "Enter: clone there and open"
		}
		b.WriteString(th.PathMissing.Render(clipTo("Directory is not on "+where, w)))
		b.WriteString("\n")
		// What Enter does about it, as the picker's preview says
		// (os-fzf.sh:326, :338).
		if v.Project.GitURL != "" {
			b.WriteString(th.Meta.Render(clipTo(clone, w)))
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
	return b.String()
}

// snapshot is what the directory holds, in the rows it is given: with ninety
// near-identical names this is what says which checkout the cursor is on.
// Tree rows are cut, not wrapped, because a wrapped tree row loses its
// indentation. rows counts the heading; a listing that does not fit ends in
// an ellipsis on its last row.
func (m *Model) snapshot(v revier.ProjectView, w, rows int) string {
	if !v.PathExists || v.Project.Remote != nil {
		return ""
	}
	tree := m.treeFor(v.Project.Path)
	room := rows - 2 // the heading and the blank line before it
	if len(tree) == 0 || room < 1 {
		return ""
	}
	if len(tree) > room {
		tree = append(tree[:room-1:room-1], "...")
	}
	var b strings.Builder
	b.WriteString(m.heading("Project Snapshot", w))
	for _, line := range tree {
		b.WriteString(m.theme.Path.Render(clipTo(line, w)))
		b.WriteString("\n")
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
