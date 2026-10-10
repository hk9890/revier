// The world, the keys and the screen readers the tests of this package share.
package tui_test

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/internal/state"
	"github.com/hk9890/revier/internal/theme"
	"github.com/hk9890/revier/internal/tui"
	"github.com/hk9890/revier/pkg/revier"
)

// world is a runtime, a window host, and n projects; the last project's
// workspace is running with an agent that wants the human.
func world(t *testing.T, n int) (*hosttest.FakeRuntime, *hosttest.Fake, *core.Core, []core.Project) {
	t.Helper()
	rt := hosttest.NewRuntime("rt")
	wm := hosttest.New("wm")
	c := &core.Core{Runtime: rt, Window: wm, Probes: []revier.AgentProbe{&hosttest.FakeProbe{
		Harness: "claude", Marker: "claude",
		State: revier.AgentState{Harness: "claude", Status: revier.StatusAttention, Activity: "needs a decision"},
	}}}
	var raw []revier.Project
	for i := 0; i < n; i++ {
		name := revier.ProjectName(fmt.Sprintf("project-%02d", i))
		raw = append(raw, revier.Project{Name: name, Path: "/p/" + string(name), Targets: []revier.Target{
			{Name: "home", Home: true, Key: "ctrl-shift-u", Runtime: &revier.Realization{
				Name: "session:" + string(name), Launch: []string{"x"}, Match: revier.Match{Title: "^session:" + string(name) + "$"}}},
			{Name: "editor", Key: "ctrl-shift-o", Window: &revier.Realization{
				Launch: []string{"code"}, Match: revier.Match{Class: "^code-" + string(name) + "$"}}},
		}})
	}
	last := raw[n-1].Name
	rt.Add("session:"+string(last), "kitty", revier.Panel{ID: "1", Kind: revier.PanelTool, Title: "claude"})
	projects := core.Prepare(raw)
	return rt, wm, c, projects
}

// stateWith writes a state file holding the given attachments and returns
// its root.
func stateWith(t *testing.T, attached map[revier.ProjectName][]revier.TargetRef) string {
	t.Helper()
	root := t.TempDir()
	st := &state.State{Attached: attached}
	if err := st.Save(root); err != nil {
		t.Fatal(err)
	}
	return root
}

// refreshed builds the model over a state root and applies one survey, as the
// timer does.
func refreshed(t *testing.T, c *core.Core, projects []core.Project, root string, actions []config.Action) tui.Model {
	t.Helper()
	m := tui.New(c, projects, root, &config.Config{Actions: actions}, time.Second, theme.Default(), "").StaticCursors()
	return survey(m)
}

// survey applies one whole refresh: the survey of this machine, then what
// the linked hosts say.
func survey(m tui.Model) tui.Model {
	next, _ := m.Update(m.Survey()())
	next, _ = next.Update(next.(tui.Model).AskRemotes()())
	return next.(tui.Model)
}

func press(m tui.Model, key string) (tui.Model, tea.Cmd) {
	var msg tea.KeyMsg
	switch key {
	case "enter":
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	case "tab":
		msg = tea.KeyMsg{Type: tea.KeyTab}
	case "shift+tab":
		msg = tea.KeyMsg{Type: tea.KeyShiftTab}
	case "esc":
		msg = tea.KeyMsg{Type: tea.KeyEsc}
	case "down":
		msg = tea.KeyMsg{Type: tea.KeyDown}
	case "up":
		msg = tea.KeyMsg{Type: tea.KeyUp}
	case "left":
		msg = tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		msg = tea.KeyMsg{Type: tea.KeyRight}
	case "backspace":
		msg = tea.KeyMsg{Type: tea.KeyBackspace}
	case "home":
		msg = tea.KeyMsg{Type: tea.KeyHome}
	case "end":
		msg = tea.KeyMsg{Type: tea.KeyEnd}
	case "pgup":
		msg = tea.KeyMsg{Type: tea.KeyPgUp}
	case "pgdown":
		msg = tea.KeyMsg{Type: tea.KeyPgDown}
	case "delete":
		msg = tea.KeyMsg{Type: tea.KeyDelete}
	case "alt+delete":
		msg = tea.KeyMsg{Type: tea.KeyDelete, Alt: true}
	default:
		letter, alt := strings.CutPrefix(key, "alt+")
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(letter), Alt: alt}
	}
	next, cmd := m.Update(msg)
	return next.(tui.Model), cmd
}

// lines is the surface's content, with the margin taken off: its blank rows
// dropped and each line right-trimmed. Tests assert on what the surface says,
// not on where it sits in the terminal.
//
// The left margin stays on the line. Every line of the surface carries a
// gutter space of its own, so a test that cared where a line starts would
// have to count either way, and margins reads the margin off the render.
func lines(m tui.Model) []string {
	out := strings.Split(m.View(), "\n")
	for i := range out {
		out[i] = strings.TrimRight(out[i], " ")
	}
	for len(out) > 0 && out[0] == "" {
		out = out[1:]
	}
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return out
}

// The chrome lines, in the order View writes them.
func barLine(m tui.Model) string { return lines(m)[0] }

func query(m tui.Model) string { return lines(m)[2] }

func ruleLine(m tui.Model) string { return lines(m)[3] }

// footer is the last content line: the key legend, or the last failure.
func footer(m tui.Model) string {
	l := lines(m)
	return l[len(l)-1]
}

// rows are the project rows, without the header, the query line and the rule.
func rows(m tui.Model) []string {
	l := lines(m)
	if len(l) < chromeLines {
		return nil
	}
	return l[chromeLines:]
}

// The action bar, the line under it, the query line and the rule sit above
// the list.
const chromeLines = 4

// column is the screen column text sits in, which is not its byte offset: a
// rule is drawn out of three-byte dashes.
func column(line, text string) int {
	at := strings.Index(line, text)
	if at < 0 {
		return -1
	}
	return lipgloss.Width(line[:at])
}

// paneCursor is the pane row carrying the cursor bar, or nothing when the
// cursor is on the list.
func paneCursor(m tui.Model) string {
	for _, line := range strings.Split(pane(m), "\n") {
		if strings.Contains(line, theme.Default().Glyphs.Cursor) {
			return line
		}
	}
	return ""
}

// paneCell is the terminal cell where the text starts on the pane line that
// carries it. The column is the rendered width of what stands before the
// text, not its byte offset: a glyph is three bytes for one cell, and a
// colour profile puts escape sequences in the line.
func paneCell(t *testing.T, m tui.Model, text string) (x, y int) {
	t.Helper()
	for y, raw := range strings.Split(m.View(), "\n") {
		// The list, the pane's border, the pane.
		if parts := strings.Split(raw, "│"); len(parts) > 1 {
			if at := strings.Index(parts[1], text); at >= 0 {
				return paneBorder(t, m) + 1 + lipgloss.Width(parts[1][:at]), y
			}
		}
	}
	t.Fatalf("no pane line carries %q:\n%s", text, m.View())
	return 0, 0
}

// selectedRow is the row the cursor is on, found by the cursor glyph the
// delegate renders into it.
func selectedRow(t *testing.T, m tui.Model) string {
	t.Helper()
	for _, line := range lines(m) {
		if strings.Contains(line, theme.Default().Glyphs.Cursor) {
			return strings.TrimSpace(line)
		}
	}
	t.Fatalf("no row is selected:\n%s", m.View())
	return ""
}

// resize is the size message a terminal sends. The default model is 80
// columns, which is too narrow to split, so a test that wants the detail pane
// has to ask for the room.
func resize(m tui.Model, w, h int) tui.Model {
	next, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return next.(tui.Model)
}

// pane is the detail pane: whatever is right of the border column on each
// line. The two panes are joined horizontally, so this is how a test reads
// one without the other.
func pane(m tui.Model) string {
	var out []string
	for _, line := range lines(m) {
		if _, right, ok := strings.Cut(line, "│"); ok {
			out = append(out, strings.TrimSpace(right))
		}
	}
	return strings.Join(out, "\n")
}

// send is one key as bubbletea's input reader delivers it.
func send(m tui.Model, msg tea.KeyMsg) (tui.Model, tea.Cmd) {
	next, cmd := m.Update(msg)
	return next.(tui.Model), cmd
}

// longActivity is an agent's activity line longer than any pane.
const longActivity = "Reading internal/tui/detail.go and working out why the activity line ends in an ellipsis where fzf wraps it"

// longWorld is one running project at path whose agent reports longActivity,
// and a second project after it.
func longWorld(t *testing.T, path string) (*core.Core, []core.Project) {
	t.Helper()
	rt := hosttest.NewRuntime("rt")
	c := &core.Core{Runtime: rt, Probes: []revier.AgentProbe{&hosttest.FakeProbe{
		Harness: "claude", Marker: "claude",
		State: revier.AgentState{Harness: "claude", Status: revier.StatusRunning, Activity: longActivity},
	}}}
	home := func(name string) revier.Target {
		return revier.Target{Name: "home", Home: true, Runtime: &revier.Realization{
			Name: "session:" + name, Launch: []string{"x"}, Match: revier.Match{Title: "^session:" + name + "$"}}}
	}
	projects := core.Prepare([]revier.Project{
		{Name: "long", Path: path, Targets: []revier.Target{home("long")}},
		{Name: "short", Path: "/p/short", Targets: []revier.Target{home("short")}},
	})
	rt.Add("session:long", "kitty", revier.Panel{ID: "1", Kind: revier.PanelTool, Title: "claude"})
	return c, projects
}

// wheel is one notch of the mouse wheel at a column.
func wheel(m tui.Model, x int, b tea.MouseButton) tui.Model {
	next, _ := m.Update(tea.MouseMsg{X: x, Y: 5, Button: b, Action: tea.MouseActionPress})
	return next.(tui.Model)
}

// paneBorder is the terminal column of the border between the list and the
// pane, read off the rendered surface: the rendered width of what stands
// before it, so a colour profile's escape sequences do not count.
func paneBorder(t *testing.T, m tui.Model) int {
	t.Helper()
	for _, raw := range strings.Split(m.View(), "\n") {
		if strings.Count(raw, "│") == 1 {
			return lipgloss.Width(raw[:strings.Index(raw, "│")])
		}
	}
	t.Fatalf("no line with a pane:\n%s", m.View())
	return 0
}

// each runs a command and hands what it answers to do. A batch answers one
// message per command, in order, as the program delivers them; a nil command
// answers nothing.
func each(cmd tea.Cmd, do func(tea.Msg)) {
	if cmd == nil {
		return
	}
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		do(msg)
		return
	}
	for _, c := range batch {
		each(c, do)
	}
}

// deliver runs a command and feeds what it answers back, and returns the
// commands the model answers with: a close that needs no confirm plans, then
// runs. run is deliver for a caller that wants less back; runAll walks the
// same commands and feeds nothing back.
func deliver(m tui.Model, cmd tea.Cmd) (tui.Model, tea.Cmd) {
	if cmd == nil {
		// A test that delivers a command expects one: with none, what it
		// asserts next would pass without the message ever arriving.
		panic("deliver: the model returned no command")
	}
	var outs []tea.Cmd
	each(cmd, func(msg tea.Msg) {
		next, out := m.Update(msg)
		m = next.(tui.Model)
		outs = append(outs, out)
	})
	return m, tea.Batch(outs...)
}

// run is deliver without the commands the model answers with.
func run(m tui.Model, cmd tea.Cmd) tui.Model {
	m, _ = deliver(m, cmd)
	return m
}

// runAll runs a command for what it does to the world; its messages are
// dropped.
func runAll(cmd tea.Cmd) {
	each(cmd, func(tea.Msg) {})
}

// clickCell is one press and release of the left button on a terminal cell,
// with the command either returned.
func clickCell(m tui.Model, x, y int) (tui.Model, tea.Cmd) {
	next, pressed := m.Update(tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	next, released := next.Update(tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease})
	return next.(tui.Model), tea.Batch(pressed, released)
}

// barCell is a terminal cell inside the "new" button of the action bar: the
// bar is the surface's first line, and "new" is the first button after the
// switch between the two lists.
func barCell(t *testing.T, m tui.Model) (x, y int) {
	t.Helper()
	mr, _ := margins(m)
	x = column(barLine(m), "new")
	if x < 0 {
		t.Fatalf("bar = %q, want the new button on it", barLine(m))
	}
	return x, mr
}

// typeInto types text into the screen's field, a rune at a time.
func typeInto(m tui.Model, text string) tui.Model {
	for _, r := range text {
		m, _ = press(m, string(r))
	}
	return m
}

// rowTop is the terminal row the first row of the list is on.
func rowTop(m tui.Model) int {
	mr, _ := margins(m)
	return mr + chromeLines
}

// listed is one agent of listedWorld: the project a panel of which shows it,
// its state, what it is on, what it said last and when, the directory it
// works in, and what its panel shows.
type listed struct {
	project string
	status  revier.Status
	on      string
	said    string
	at      time.Time
	dir     string
	screen  string
}

// listedWorld is listedSurface with the agent list in view and its asks
// answered.
func listedWorld(t *testing.T, width, height int, agents ...listed) (tui.Model, *hosttest.FakeRuntime, []*hosttest.FakeDetailedProbe) {
	t.Helper()
	m, rt, fakes := listedSurface(t, width, height, agents...)
	return switched(m).Said().Mirrored(), rt, fakes
}

// listedSurface is the given agents in running projects, one probe to each,
// surveyed, with the project list in view and nothing read of what an agent
// said. The projects are named as given and stand in that order in the
// configuration. Agent i is panel i+1 of its project's workspace.
func listedSurface(t *testing.T, width, height int, agents ...listed) (tui.Model, *hosttest.FakeRuntime, []*hosttest.FakeDetailedProbe) {
	t.Helper()
	rt := hosttest.NewRuntime("rt")
	rt.Screens = map[revier.PanelID]string{}
	var probes []revier.AgentProbe
	var fakes []*hosttest.FakeDetailedProbe
	var names []string
	panels := map[string][]revier.Panel{}
	for i, a := range agents {
		marker := fmt.Sprintf("agent-%d", i)
		id := revier.PanelID(fmt.Sprint(i + 1))
		p := hosttest.NewDetailedProbe("claude", marker)
		p.State = revier.AgentState{Harness: "claude", Status: a.status, Activity: a.on, Dir: a.dir}
		p.Said[id] = revier.AgentDetail{Message: a.said, At: a.at}
		probes, fakes = append(probes, p), append(fakes, p)
		if !slices.Contains(names, a.project) {
			names = append(names, a.project)
		}
		panels[a.project] = append(panels[a.project], revier.Panel{ID: id, Kind: revier.PanelTool, Title: "claude " + marker})
		rt.Screens[id] = a.screen
	}
	var raw []revier.Project
	for _, name := range names {
		raw = append(raw, revier.Project{Name: revier.ProjectName(name), Path: "/p/" + name, GitURL: "https://example.com/" + name + ".git",
			Targets: []revier.Target{{Name: "home", Home: true, Runtime: &revier.Realization{
				Name: "session:" + name, Launch: []string{"x"}, Match: revier.Match{Title: "^session:" + name + "$"}}}}})
		rt.Add("session:"+name, "kitty", panels[name]...)
	}
	c := &core.Core{Runtime: rt, Probes: probes}
	return resize(refreshed(t, c, core.Prepare(raw), stateWith(t, nil), nil), width, height), rt, fakes
}

// switched is the model after the key that switches between the two lists.
func switched(m tui.Model) tui.Model {
	m, _ = send(m, tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}, Alt: true})
	return m
}

// listedRows is the first line of every row of the list in view, in order:
// the line a row's state and summary are on.
func listedRows(m tui.Model) []string {
	var out []string
	all := lines(m)
	for i := chromeLines; i < len(all)-1; i += 2 {
		left, _, _ := strings.Cut(all[i], "│")
		if strings.TrimSpace(left) != "" {
			out = append(out, strings.TrimSpace(left))
		}
	}
	return out
}

// clearField deletes what a form field holds.
func clearField(m tui.Model) tui.Model {
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	return next.(tui.Model)
}

// configRoot points the configuration at a scratch directory holding text as
// config.toml.
func configRoot(t *testing.T, text string) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv("REVIER_CONFIG_HOME", root)
	if err := os.WriteFile(config.File(root), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func screen(m tui.Model) string { return strings.Join(lines(m), "\n") }

const sharedTargets = `[[target]]
name = "home"
home = true
key = "ctrl-shift-u"
  [target.runtime]
  name = "session:{{.Name}}"
  match = { title = "^session:{{.Name}}$" }
    # the agent
    [[target.runtime.panels]]
    kind = "agent"
    title = "Claude Code"
    command = ["claude"]
    [[target.runtime.panels]]
    kind = "shell"
    title = "shell"

[[target]]
name = "editor"
key = "ctrl-shift-o"
  [target.window]
  launch = ["idea", "{{.Path}}"] # IntelliJ
  match = { class = "^jetbrains-idea" }
`

func downs(m tui.Model, n int) tui.Model {
	for range n {
		m, _ = press(m, "down")
	}
	return m
}

func wantSelected(t *testing.T, m tui.Model, name, why string) {
	t.Helper()
	if row := selectedRow(t, m); !strings.Contains(row, name) {
		t.Errorf("selected %q, want %s: %s", row, name, why)
	}
}

const fileProject = `
path = "%PATH%"
%GIT%
[[target]]
name = "home"
home = true
  [target.runtime]
  name = "session:{{.Name}}"
  launch = ["sh"]
  match = { title = "^session:{{.Name}}$" }
`

// onDisk writes one project file per name, loads them the way the CLI does,
// and returns the loaded projects and the directory the files are in. path
// maps a name to its directory; a name not in it gets a directory that
// exists.
func onDisk(t *testing.T, names []string, path map[string]string, gitURL map[string]string) ([]core.Project, string) {
	t.Helper()
	dir := t.TempDir()
	for _, n := range names {
		p, ok := path[n]
		if !ok {
			p = t.TempDir()
		}
		git := ""
		if u := gitURL[n]; u != "" {
			git = `git_url = "` + u + `"`
		}
		body := strings.NewReplacer("%PATH%", p, "%GIT%", git).Replace(fileProject)
		if err := os.WriteFile(filepath.Join(dir, n+".toml"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	projects, err := config.LoadProjects(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	return projects, dir
}

// linkWorld is a surface with an ssh configuration naming buildbox and
// farbox, a scratch configuration root to write links into, and a fake
// buildbox that has the named projects.
func linkWorld(t *testing.T, projects []core.Project, onHost ...string) (tui.Model, *hosttest.FakeRemote, string) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("REVIER_CONFIG_HOME", root)
	sshConfig := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(sshConfig, []byte("Host buildbox\n  HostName 10.0.0.7\nHost farbox\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("REVIER_SSH_CONFIG", sshConfig)

	var views []revier.ProjectView
	for _, n := range onHost {
		views = append(views, revier.ProjectView{Project: revier.Project{Name: revier.ProjectName(n), Path: "/home/user/dev/" + n}, PathExists: true})
	}
	remote := hosttest.NewRemote("buildbox", views...)
	c := &core.Core{Runtime: hosttest.NewRuntime("rt"), NewRemote: func(string) revier.Remote { return remote }}
	return resize(refreshed(t, c, projects, stateWith(t, nil), nil), 80, 20), remote, root
}

const demoProject = `# demo
path = "/tmp/demo"

[[target]]
name = "editor"
key = "ctrl-o"
`

// projectSurface is the surface over a configuration root with the shared
// targets of sharedTargets and one project, demo, written as body; the
// runtime is rt. It returns the project file and the state root too.
func projectSurface(t *testing.T, body string, rt *hosttest.FakeRuntime) (tui.Model, string, string) {
	t.Helper()
	return projectSurfaceOver(t, sharedTargets, body, rt)
}

// projectSurfaceOver is projectSurface with shared as config.toml.
func projectSurfaceOver(t *testing.T, shared, body string, rt *hosttest.FakeRuntime) (tui.Model, string, string) {
	t.Helper()
	root := configRoot(t, shared)
	dir := filepath.Join(root, "projects")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "demo.toml")
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, projects, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	stateRoot := stateWith(t, nil)
	m := tui.New(&core.Core{Runtime: rt}, projects, stateRoot, cfg, time.Second, theme.Default(), "")
	next, _ := m.Update(m.Survey()())
	return resize(next.(tui.Model), 140, 60), file, stateRoot
}

func fileText(t *testing.T, file string) string {
	t.Helper()
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// remoteFile is a project on buildbox: its path is in that machine's terms,
// and the home target here is the ssh pane onto the workspace there.
const remoteFile = `
[remote]
host = "buildbox"
`

// remoteOnDisk writes one remote project file and loads it the way the CLI
// does.
func remoteOnDisk(t *testing.T, name string) []core.Project {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, name+".toml"), []byte(remoteFile), 0o644); err != nil {
		t.Fatal(err)
	}
	projects, err := config.LoadProjects(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	return projects
}

// hostSays is what the revier on buildbox answers about a project: the
// checkout is there, and its one agent is in the given state. The agent is
// named by the tag a panel of machine box on pid 4242 gave it, which is what
// openHere opens.
func hostSays(name string, status revier.Status) revier.ProjectView {
	return revier.ProjectView{
		Project:    revier.Project{Name: revier.ProjectName(name), Path: "/home/user/dev/" + name},
		PathExists: true,
		Agents:     []revier.AgentView{{Panel: "box.4242", State: revier.AgentState{Harness: "claude", Status: status}}},
	}
}

// openHere is the link's workspace open on this machine: one panel running
// the ssh, on the pid the host tags what that panel started with.
func openHere(name string) *hosttest.FakeRuntime {
	rt := hosttest.NewRuntime("rt")
	rt.Add("session:"+name, "kitty", revier.Panel{ID: "9", Kind: revier.PanelTool, PID: 4242,
		Command: []string{"ssh", "-t", "buildbox"}})
	return rt
}

// clickAt is one press of the left button on a terminal cell.
func clickAt(m tui.Model, x, y int) tui.Model {
	m, _ = clickCell(m, x, y)
	return m
}

// margins reads the frame's margin off the rendered surface: the rows above
// its top border and the columns left of it.
func margins(m tui.Model) (rows, cols int) {
	for i, line := range strings.Split(m.View(), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		// Every line of the surface starts with a gutter space of its own,
		// which is not margin.
		return i, len(line) - len(strings.TrimLeft(line, " ")) - 1
	}
	return 0, 0
}

// probeOf is the world's one agent probe, whose state a test changes between
// the plan and the confirm.
func probeOf(c *core.Core) *hosttest.FakeProbe { return c.Probes[0].(*hosttest.FakeProbe) }
