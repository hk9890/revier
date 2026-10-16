package tui_test

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/theme"
	"github.com/hk9890/revier/internal/tui"
	"github.com/hk9890/revier/pkg/revier"
)

// With no built-in among the projects, alt+a hands the terminal over: the
// press answers with the command that runs the assistant, and opens no
// screen of the surface's own.
func TestAltAHandsTheTerminalToTheAssistant(t *testing.T) {
	_, _, c, projects := world(t, 2)
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 160, 20)

	if bar := barLine(m); !strings.Contains(bar, "assist alt+a") {
		t.Errorf("bar = %q, want it to name the assist button and its key", bar)
	}
	before := drawn(m)
	m, cmd := press(m, "alt+a")
	if cmd == nil {
		t.Fatal("alt+a returned no command")
	}
	if drawn(m) != before {
		t.Error("alt+a changed the surface before the hand-over")
	}
}

// With the built-in among the projects, alt+a raises it beside the surface,
// as a target key raises a tab, and hands nothing over.
func TestAltARaisesTheBuiltinAssistant(t *testing.T) {
	rt, _, c, projects := world(t, 2)
	builtin := config.Builtin(t.TempDir(), "/bin/revier")
	m := resize(refreshed(t, c, config.WithBuiltin(projects, builtin), stateWith(t, nil), nil), 160, 20)

	m, cmd := press(m, "alt+a")
	if cmd == nil {
		t.Fatal("alt+a returned no command")
	}
	cmd()
	if len(rt.Opened) != 1 || rt.Opened[0].Name != string(config.Assistant) {
		t.Fatalf("runtime Opened = %v, want the assistant's workspace", rt.Opened)
	}
	if body := strings.Join(rows(m), "\n"); !strings.Contains(body, string(config.Assistant)) {
		t.Errorf("the surface does not list the assistant:\n%s", body)
	}
}

// The assistant's row says it is revier's own, where another row says where
// its checkout is; a file that takes the name gets a folder like any other.
func TestTheBuiltinAssistantRowHasItsOwnGlyph(t *testing.T) {
	_, _, c, projects := world(t, 2)
	builtin := config.Builtin(t.TempDir(), "/bin/revier")
	cog := theme.Default().Glyphs.Builtin
	m := resize(refreshed(t, c, config.WithBuiltin(projects, builtin), stateWith(t, nil), nil), 120, 20)

	for _, row := range rows(m) {
		if strings.Contains(row, cog) != strings.Contains(row, string(config.Assistant)) {
			t.Errorf("row %q: want the built-in glyph %q on the assistant's row alone", row, cog)
		}
	}

	// The pane offers no edit: there is no file to open.
	m, _ = press(m, "end")
	if top, _, _ := strings.Cut(pane(m), "\n"); strings.Contains(top, "alt+e") || !strings.Contains(top, "built in") {
		t.Errorf("pane top = %q, want the built-in named as such and no edit key", top)
	}

	mine := core.PrepareProject(revier.Project{Name: config.Assistant, Path: "/mine"})
	mine.File = "/cfg/projects/" + string(config.Assistant) + ".toml"
	m = resize(refreshed(t, c, config.WithBuiltin(append(projects, mine), builtin), stateWith(t, nil), nil), 120, 20)
	if body := strings.Join(rows(m), "\n"); strings.Contains(body, cog) {
		t.Errorf("a file of the built-in's name drew the built-in glyph:\n%s", body)
	}
}

// A file of the built-in's name read after the start takes its row. alt+a
// then opens no workspace the list does not show: it hands the terminal over.
func TestAltAHandsOverWhenAFileTookTheBuiltinsName(t *testing.T) {
	root := t.TempDir()
	t.Setenv("REVIER_CONFIG_HOME", root)
	rt, _, c, projects := world(t, 2)
	builtin := config.Builtin(t.TempDir(), "/bin/revier")
	m := resize(refreshed(t, c, config.WithBuiltin(projects, builtin), stateWith(t, nil), nil), 160, 20)
	if err := os.MkdirAll(filepath.Join(root, "projects"), 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "projects", string(config.Assistant)+".toml")
	if err := os.WriteFile(file, []byte("path = \"/p/mine\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, cmd := m.Assisted(nil)
	m, _ = deliver(m, cmd)

	if _, cmd = press(m, "alt+a"); cmd == nil {
		t.Fatal("alt+a returned no command")
	}
	cmd()
	if len(rt.Opened) != 0 {
		t.Errorf("runtime Opened = %v, want nothing: the built-in is not listed", rt.Opened)
	}
}

// The files do not hold the assistant, so a read of them keeps it.
func TestTheBuiltinAssistantOutlivesAReload(t *testing.T) {
	t.Setenv("REVIER_CONFIG_HOME", t.TempDir())
	_, _, c, projects := world(t, 2)
	builtin := config.Builtin(t.TempDir(), "/bin/revier")
	m := resize(refreshed(t, c, config.WithBuiltin(projects, builtin), stateWith(t, nil), nil), 120, 20)

	m, cmd := m.Assisted(nil)
	m, _ = deliver(m, cmd)
	if body := strings.Join(rows(m), "\n"); !strings.Contains(body, string(config.Assistant)) {
		t.Errorf("the reload dropped the assistant:\n%s", body)
	}
}

// The assistant is told the project of the row under the cursor. On the agent
// list that row is an agent, and the project list's cursor is off the screen:
// its project is not the one the user is looking at.
func TestTheAssistantIsToldTheProjectUnderTheCursor(t *testing.T) {
	_, _, c, projects := world(t, 2)
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 160, 20)

	// The one agent is project-01's, which leads the list for it.
	m, _ = press(m, "down")
	if got, want := m.AssistArgs(), []string{"assist", "-p", "project-00"}; !slices.Equal(got, want) {
		t.Fatalf("on the project list: %q, want %q", got, want)
	}
	m = switched(m)
	if got, want := m.AssistArgs(), []string{"assist", "-p", "project-01"}; !slices.Equal(got, want) {
		t.Errorf("on the agent list: %q, want %q, the project of the agent under the cursor", got, want)
	}
}

// What the assistant wrote is on the surface when it comes back: the files
// are read again, as on the raise of the popup.
func TestTheSurfaceReadsTheFilesAgainAfterTheAssistant(t *testing.T) {
	root := t.TempDir()
	t.Setenv("REVIER_CONFIG_HOME", root)
	_, _, c, projects := world(t, 2)
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 120, 20)

	if err := os.MkdirAll(filepath.Join(root, "projects"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "projects", "written-by-the-assistant.toml"), []byte("path = \"/p/written\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, cmd := m.Assisted(nil)
	if cmd == nil {
		t.Fatal("the assistant's exit read nothing")
	}
	m, _ = deliver(m, cmd)
	if body := strings.Join(rows(m), "\n"); !strings.Contains(body, "written-by-the-assistant") {
		t.Errorf("the surface lists no project the assistant wrote:\n%s", body)
	}
}

// A config.toml the assistant left that does not load is said under the list
// as it was: the user is back to read what the assistant wrote.
func TestAConfigurationTheAssistantBrokeIsSaid(t *testing.T) {
	root := t.TempDir()
	t.Setenv("REVIER_CONFIG_HOME", root)
	_, _, c, projects := world(t, 2)
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 200, 20)

	if err := os.WriteFile(filepath.Join(root, "config.toml"), []byte("this is not = = toml"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, cmd := m.Assisted(nil)
	m, _ = deliver(m, cmd)
	if got := footer(m); !strings.Contains(got, "config.toml") {
		t.Errorf("footer = %q, want it to name the file that does not load", got)
	}
	if body := strings.Join(rows(m), "\n"); !strings.Contains(body, "project-00") {
		t.Errorf("the list did not stand over the broken configuration:\n%s", body)
	}
}

// The surface takes the terminal back from the assistant with the mouse as
// it had it: its view asks for every motion, and bubbletea sets the modes the
// view asks for again when a hand-over ends
// (TestATerminalHandedBackHasTheMouseAgain).
func TestTheSurfaceTakesTheMouseBackAfterTheAssistant(t *testing.T) {
	t.Setenv("REVIER_CONFIG_HOME", t.TempDir())
	_, _, c, projects := world(t, 2)
	m := refreshed(t, c, projects, stateWith(t, nil), nil)

	m, _ = m.Assisted(nil)
	if got := m.View().MouseMode; got != tea.MouseModeAllMotion {
		t.Errorf("mouse mode after the assistant = %v, want every motion", got)
	}
}

// An action and a clone have the terminal as the assistant has, and give it
// back the same way. Without the mouse a drag is the terminal's own
// selection, which the next redraw clears.
func TestTheSurfaceTakesTheMouseBackAfterAnActionAndAClone(t *testing.T) {
	_, _, c, projects := world(t, 2)
	m := refreshed(t, c, projects, stateWith(t, nil), nil)

	acted, _ := m.Acted(nil)
	cloned, _ := m.Cloned(projects[0], nil)
	failed, _ := m.Cloned(projects[0], errors.New("no such repository"))
	for name, back := range map[string]tui.Model{"an action": acted, "a clone": cloned, "a clone that failed": failed} {
		if got := back.View().MouseMode; got != tea.MouseModeAllMotion {
			t.Errorf("mouse mode after %s = %v, want every motion", name, got)
		}
	}
}

// handOver is a program whose view asks for every motion. It hands the
// terminal to a command once its first frame is drawn, and quits when the
// terminal is back.
type handOver struct{ run tea.ExecCommand }

type handOverNow struct{}

func (h handOver) Init() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg { return handOverNow{} })
}

func (h handOver) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if _, now := msg.(handOverNow); now {
		return h, tea.Exec(h.run, func(error) tea.Msg { return tea.QuitMsg{} })
	}
	return h, nil
}

func (h handOver) View() tea.View {
	v := tea.NewView("surface")
	v.AltScreen = true
	v.MouseMode = tea.MouseModeAllMotion
	return v
}

// marked is a command that writes a mark on the terminal it is handed.
type marked struct{ out io.Writer }

func (c *marked) Run() error {
	_, err := io.WriteString(c.out, "handed-over")
	return err
}
func (c *marked) SetStdin(io.Reader)      {}
func (c *marked) SetStdout(out io.Writer) { c.out = out }
func (c *marked) SetStderr(io.Writer)     {}

// lockedBuffer is a terminal's output, which the renderer and the command
// both write to.
type lockedBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// A terminal handed to a command comes back with the pointer reports the
// view asks for: bubbletea turns them off for the command and on again
// after it, so the surface asks for nothing itself.
func TestATerminalHandedBackHasTheMouseAgain(t *testing.T) {
	var out lockedBuffer
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	program := tea.NewProgram(handOver{run: &marked{}}, tea.WithContext(ctx), tea.WithoutSignalHandler(),
		tea.WithInput(nil), tea.WithOutput(&out))
	if _, err := program.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	_, after, ok := strings.Cut(out.String(), "handed-over")
	if !ok {
		t.Fatalf("the command never had the terminal:\n%q", out.String())
	}
	if !strings.Contains(after, ansi.SetModeMouseAnyEvent) {
		t.Errorf("the terminal came back without every motion reported:\n%q", after)
	}
}
