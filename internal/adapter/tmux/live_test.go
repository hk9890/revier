//go:build live

// Layer L4: a real tmux server, headless.
//
// Every test here runs against a private server on its own socket (`tmux -L`),
// started and killed by the test. Nothing attaches a client, nothing reaches a
// display, and the user's own tmux sessions are never visible to it. This is
// the layer that proves an adapter actually drives its tool, without which L2
// only proves the core is self-consistent.
package tmux_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hk9890/revier/internal/adapter/tmux"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/pkg/revier"
)

// server returns a Host on a socket private to this test, and kills the server
// when the test ends.
func server(t *testing.T) *tmux.Host {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed; L4 needs it")
	}
	socket := "revier-test-" + t.Name()
	h := &tmux.Host{Socket: socket}
	t.Cleanup(func() { killServer(t, socket) })
	return h
}

// killServer stops the server on a socket and returns once it has gone.
// kill-server returns before the server exits, and a client that connects in
// between - the next test on this socket, or the next command of this one -
// reaches a server on its way out and fails with "server exited
// unexpectedly".
func killServer(t *testing.T, socket string) {
	t.Helper()
	_ = exec.Command("tmux", "-L", socket, "kill-server").Run()
	deadline := time.Now().Add(5 * time.Second)
	for {
		out, _ := exec.Command("tmux", "-L", socket, "list-sessions").CombinedOutput()
		if strings.Contains(string(out), "no server running") || strings.Contains(string(out), "error connecting") {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the server on %s still answers after kill-server: %s", socket, out)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func ctx(t *testing.T) context.Context {
	c, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	return c
}

func TestProbeFindsTmux(t *testing.T) {
	if err := server(t).Probe(ctx(t)); err != nil {
		t.Fatalf("Probe: %v", err)
	}
}

// A server with nothing on it is an empty list, not an error: the survey has
// to render on a machine where nothing has been opened yet.
func TestInstancesOnAnEmptyServer(t *testing.T) {
	got, err := server(t).Instances(ctx(t))
	if err != nil {
		t.Fatalf("Instances: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d instances, want 0", len(got))
	}
}

// With no server nothing is focused, which is not an error: every keypress
// asks, and a machine with no tmux session open is an ordinary one.
func TestFocusedWithNoServer(t *testing.T) {
	got, err := server(t).Focused(ctx(t))
	if err != nil {
		t.Fatalf("Focused: %v", err)
	}
	if !got.IsZero() {
		t.Errorf("Focused = %+v, want a zero ref", got)
	}
}

// The invariant every host owes the core: what Open creates, Match finds.
func TestOpenThenMatchFindsIt(t *testing.T) {
	h, c := server(t), ctx(t)
	real := revier.Realization{
		Name:   "diff",
		Launch: []string{"sh", "-c", "sleep 30"},
		Match:  revier.Match{Title: "^diff$"},
	}

	ref, err := h.Open(c, real)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if ref.Host != "tmux" || ref.ID == "" {
		t.Fatalf("ref = %+v, want a tmux ref with an id", ref)
	}

	instances, err := h.Instances(c)
	if err != nil {
		t.Fatalf("Instances: %v", err)
	}
	m, err := real.Match.Compile()
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	for _, inst := range instances {
		if m.Matches(inst) {
			return
		}
	}
	t.Fatalf("no instance matched %q; got %+v", real.Match.Title, instances)
}

func TestFocusAndFocusedRoundTrip(t *testing.T) {
	h, c := server(t), ctx(t)
	first, err := h.Open(c, revier.Realization{
		Name: "home", Launch: []string{"sh", "-c", "sleep 30"},
		Match: revier.Match{Title: "^home$"},
	})
	if err != nil {
		t.Fatalf("Open home: %v", err)
	}
	second, err := h.Open(c, revier.Realization{
		Name: "diff", Launch: []string{"sh", "-c", "sleep 30"},
		Match: revier.Match{Title: "^diff$"},
	})
	if err != nil {
		t.Fatalf("Open diff: %v", err)
	}

	for _, want := range []revier.TargetRef{first, second, first} {
		if err := h.Focus(c, want); err != nil {
			t.Fatalf("Focus %s: %v", want.ID, err)
		}
		got, err := h.Focused(c)
		if err != nil {
			t.Fatalf("Focused: %v", err)
		}
		if got.ID != want.ID {
			t.Fatalf("Focused = %s, want %s", got.ID, want.ID)
		}
	}
}

// tmux writes every non-ASCII character as '_' when LANG and LC_* name no
// UTF-8 locale - cron, a container, ssh without locale forwarding - unless
// it is told its output may carry UTF-8. A project name and the glyph a
// Claude title leads with have to survive that.
func TestNonASCIISurvivesALocaleWithoutUTF8(t *testing.T) {
	for _, name := range []string{"LANG", "LC_ALL", "LC_CTYPE"} {
		t.Setenv(name, "C")
	}
	t.Setenv("TMUX", "") // restored afterwards; unset below, as outside tmux
	_ = os.Unsetenv("TMUX")
	h, c := server(t), ctx(t)
	real := revier.Realization{
		Name:   "session:münchen",
		Panels: []revier.PanelSpec{{Title: "⠧ Working", Command: []string{"sh", "-c", "sleep 30"}}},
		Match:  revier.Match{Title: "^session:münchen$"},
	}
	if _, err := h.Open(c, real); err != nil {
		t.Fatalf("Open: %v", err)
	}
	instances, err := h.Instances(c)
	if err != nil {
		t.Fatalf("Instances: %v", err)
	}
	if len(instances) != 1 || instances[0].Title != "session:münchen" {
		t.Fatalf("instances = %+v, want the window listed as session:münchen", instances)
	}
	if got := instances[0].Panels[0].Title; got != "⠧ Working" {
		t.Errorf("pane title = %q, want the spinner glyph kept", got)
	}
}

// A window linked into a second session - a session group, link-window - is
// listed by list-panes -a once per session. Its panes are still one set:
// counted twice, every agent in them would be two.
func TestALinkedWindowListsItsPanesOnce(t *testing.T) {
	h, c := server(t), ctx(t)
	if _, err := h.Open(c, revier.Realization{
		Name: "home", Launch: []string{"sh", "-c", "sleep 30"},
		Match: revier.Match{Title: "^home$"},
	}); err != nil {
		t.Fatalf("Open: %v", err)
	}
	// Grouped under a name that sorts first, so list-panes -a lists the view
	// before the workspace.
	if out, err := exec.Command("tmux", "-L", h.Socket, "new-session", "-d", "-t", "home", "-s", "a-view").CombinedOutput(); err != nil {
		t.Fatalf("new-session -t: %v\n%s", err, out)
	}
	instances, err := h.Instances(c)
	if err != nil {
		t.Fatalf("Instances: %v", err)
	}
	for _, inst := range instances {
		want := 0
		if inst.Title == "home" {
			want = 1
		}
		if len(inst.Panels) != want {
			t.Errorf("%s holds %d panes, want %d: the pane is the workspace's, listed once", inst.Title, len(inst.Panels), want)
		}
	}
}

// A panel is focused in the instance's own session, even when a session
// grouped onto it resolves the pane first.
func TestFocusPanelSwitchesTheInstancesSessionInAGroup(t *testing.T) {
	h, c := server(t), ctx(t)
	ref, err := h.Open(c, revier.Realization{Name: "work", Launch: []string{"sh", "-c", "sleep 30"}})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if out, err := exec.Command("tmux", "-L", h.Socket, "new-session", "-d", "-t", "work", "-s", "a-view").CombinedOutput(); err != nil {
		t.Fatalf("new-session -t: %v\n%s", err, out)
	}
	tab, err := h.OpenTab(c, ref, revier.Realization{Launch: []string{"sh", "-c", "sleep 30"}}, nil)
	if err != nil {
		t.Fatalf("OpenTab: %v", err)
	}
	if err := h.FocusPanel(c, ref, tab); err != nil {
		t.Fatalf("FocusPanel: %v", err)
	}
	if cur, err := h.FocusedPanel(c, ref); err != nil || cur != tab {
		t.Errorf("FocusedPanel = %s, %v, want %s current in the workspace's session", cur, err, tab)
	}
}

// Two workspaces are two sessions, so a switch made in one leaves the other
// where it was: each instance is attached by its own session id, and each
// session keeps its own current window.
func TestTwoWorkspacesAreTwoSessions(t *testing.T) {
	h, c := server(t), ctx(t)
	a, err := h.Open(c, revier.Realization{Name: "session:a", Launch: []string{"sh", "-c", "sleep 30"}})
	if err != nil {
		t.Fatalf("Open a: %v", err)
	}
	b, err := h.Open(c, revier.Realization{Name: "session:b", Launch: []string{"sh", "-c", "sleep 30"}})
	if err != nil {
		t.Fatalf("Open b: %v", err)
	}
	attachA, _ := h.AttachCommand(a)
	attachB, _ := h.AttachCommand(b)
	if attachA[len(attachA)-1] == attachB[len(attachB)-1] {
		t.Fatalf("both workspaces attach %s", attachA[len(attachA)-1])
	}
	bTab, err := h.OpenTab(c, b, revier.Realization{Launch: []string{"sh", "-c", "sleep 30"}}, nil)
	if err != nil {
		t.Fatalf("OpenTab b: %v", err)
	}
	if err := h.FocusPanel(c, b, bTab); err != nil {
		t.Fatalf("FocusPanel b: %v", err)
	}
	aTab, err := h.OpenTab(c, a, revier.Realization{Launch: []string{"sh", "-c", "sleep 30"}}, nil)
	if err != nil {
		t.Fatalf("OpenTab a: %v", err)
	}
	for _, focus := range []revier.PanelID{aTab, aTab} {
		if err := h.FocusPanel(c, a, focus); err != nil {
			t.Fatalf("FocusPanel a: %v", err)
		}
	}
	if cur, err := h.FocusedPanel(c, b); err != nil || cur != bTab {
		t.Errorf("b's current panel = %s, %v, want its tab %s kept after a switch in a", cur, err, bTab)
	}
}

// A session name is what a terminal shows, and the identity is @revier-name:
// "C#" is not cut to "C", and a name a session already has still opens.
func TestOpenGivesEveryNameASession(t *testing.T) {
	h, c := server(t), ctx(t)
	if out, err := exec.Command("tmux", "-L", h.Socket, "new-session", "-d", "-s", "taken", "sleep", "30").CombinedOutput(); err != nil {
		t.Fatalf("new-session: %v\n%s", err, out)
	}
	for _, name := range []string{"C#", "C", "taken"} {
		if _, err := h.Open(c, revier.Realization{Name: name, Launch: []string{"sh", "-c", "sleep 30"}}); err != nil {
			t.Fatalf("Open %q: %v", name, err)
		}
	}
	out, err := exec.Command("tmux", "-L", h.Socket, "list-sessions", "-F", "#{session_name}").Output()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "C#\n") {
		t.Errorf("sessions = %q, want one named C#", out)
	}
	instances, err := h.Instances(c)
	if err != nil {
		t.Fatalf("Instances: %v", err)
	}
	if len(instances) != 4 {
		t.Errorf("instances = %+v, want the hand-made session and the three opened", instances)
	}
}

// attach puts a terminal onto a session of the test's server, as a user's ssh
// pane is, and returns its client name once the server lists it.
func attach(t *testing.T, h *tmux.Host, session string) string {
	t.Helper()
	if _, err := exec.LookPath("script"); err != nil {
		t.Skip("script not installed; a client needs a terminal")
	}
	before := clientNames(t, h)
	cmd := exec.Command("script", "-qfc", "tmux -L "+h.Socket+" attach-session -t "+session, "/dev/null")
	// A CI runner has no TERM, and tmux will not attach a terminal it cannot
	// clear.
	cmd.Env = append(os.Environ(), "TMUX=", "TERM=xterm")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, name := range clientNames(t, h) {
			if !slices.Contains(before, name) {
				return name
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("no client attached to %s within 5s", session)
	return ""
}

func clientNames(t *testing.T, h *tmux.Host) []string {
	t.Helper()
	out, _ := exec.Command("tmux", "-L", h.Socket, "list-clients", "-F", "#{client_name}").Output()
	return strings.Fields(string(out))
}

// clientSession is the session a client shows.
func clientSession(t *testing.T, h *tmux.Host, name string) string {
	t.Helper()
	out, err := exec.Command("tmux", "-L", h.Socket, "list-clients", "-F", "#{session_name} #{client_name}").Output()
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if session, client, _ := strings.Cut(line, " "); client == name {
			return session
		}
	}
	t.Fatalf("no client %s in %q", name, out)
	return ""
}

// Run from outside tmux, Focus switches the one terminal attached, and
// Focused reports what it shows.
func TestFocusSwitchesTheOneAttachedTerminal(t *testing.T) {
	t.Setenv("TMUX", "")
	h, c := server(t), ctx(t)
	if _, err := h.Open(c, revier.Realization{Name: "a", Launch: []string{"sh", "-c", "sleep 30"}}); err != nil {
		t.Fatalf("Open a: %v", err)
	}
	b, err := h.Open(c, revier.Realization{Name: "b", Launch: []string{"sh", "-c", "sleep 30"}})
	if err != nil {
		t.Fatalf("Open b: %v", err)
	}
	term := attach(t, h, "a")

	if err := h.Focus(c, b); err != nil {
		t.Fatalf("Focus: %v", err)
	}
	if got := clientSession(t, h, term); got != "b" {
		t.Errorf("the terminal shows %s, want b", got)
	}
	if got, err := h.Focused(c); err != nil || got.ID != b.ID {
		t.Errorf("Focused = %+v, %v, want b", got, err)
	}
}

// Run from outside tmux with two terminals attached, Focus moves neither -
// which one is meant is not tmux's to know - and Focused reports the focus
// it recorded.
func TestFocusMovesNoTerminalOfSeveral(t *testing.T) {
	t.Setenv("TMUX", "")
	h, c := server(t), ctx(t)
	for _, name := range []string{"a", "b"} {
		if _, err := h.Open(c, revier.Realization{Name: name, Launch: []string{"sh", "-c", "sleep 30"}}); err != nil {
			t.Fatalf("Open %s: %v", name, err)
		}
	}
	target, err := h.Open(c, revier.Realization{Name: "target", Launch: []string{"sh", "-c", "sleep 30"}})
	if err != nil {
		t.Fatalf("Open target: %v", err)
	}
	first, second := attach(t, h, "a"), attach(t, h, "b")

	if err := h.Focus(c, target); err != nil {
		t.Fatalf("Focus: %v", err)
	}
	if clientSession(t, h, first) != "a" || clientSession(t, h, second) != "b" {
		t.Errorf("terminals show %s and %s, want a and b unmoved", clientSession(t, h, first), clientSession(t, h, second))
	}
	if got, err := h.Focused(c); err != nil || got.ID != target.ID {
		t.Errorf("Focused = %+v, %v, want the recorded target", got, err)
	}
}

// Run in a pane - a key pressed there - Focus switches the terminal attached
// to that pane's session and no other; run in a pane of a session nobody is
// attached to, it switches none and does not fail.
func TestFocusFromAPaneSwitchesThatPanesTerminal(t *testing.T) {
	h, c := server(t), ctx(t)
	var panes []revier.PanelID
	for _, name := range []string{"mine", "other", "detached"} {
		ref, err := h.Open(c, revier.Realization{Name: name, Launch: []string{"sh", "-c", "sleep 30"}})
		if err != nil {
			t.Fatalf("Open %s: %v", name, err)
		}
		pane, err := h.FocusedPanel(c, ref)
		if err != nil {
			t.Fatal(err)
		}
		panes = append(panes, pane)
	}
	target, err := h.Open(c, revier.Realization{Name: "target", Launch: []string{"sh", "-c", "sleep 30"}})
	if err != nil {
		t.Fatalf("Open target: %v", err)
	}
	mine, other := attach(t, h, "mine"), attach(t, h, "other")
	socket, err := exec.Command("tmux", "-L", h.Socket, "display-message", "-p", "#{socket_path}").Output()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMUX", strings.TrimSpace(string(socket))+",1,0")

	t.Setenv("TMUX_PANE", panes[2].String())
	if err := h.Focus(c, target); err != nil {
		t.Fatalf("Focus from a detached session's pane: %v", err)
	}
	if clientSession(t, h, mine) != "mine" || clientSession(t, h, other) != "other" {
		t.Fatalf("a Focus from a detached pane moved a terminal")
	}

	t.Setenv("TMUX_PANE", panes[0].String())
	if err := h.Focus(c, target); err != nil {
		t.Fatalf("Focus: %v", err)
	}
	if got := clientSession(t, h, mine); got != "target" {
		t.Errorf("the pane's terminal shows %s, want target", got)
	}
	if got := clientSession(t, h, other); got != "other" {
		t.Errorf("the other terminal shows %s, want it unmoved", got)
	}
}

// ClosePanel ends one pane and leaves the session; Close ends the session.
func TestCloseEndsAPaneThenTheSession(t *testing.T) {
	h, c := server(t), ctx(t)
	ref, err := h.Open(c, revier.Realization{Name: "session:demo", Panels: []revier.PanelSpec{
		{Kind: revier.PanelShell, Command: []string{"sh", "-c", "sleep 30"}},
		{Kind: revier.PanelAgent, Command: []string{"sh", "-c", "sleep 30"}},
	}})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	other, err := h.Open(c, revier.Realization{Name: "other", Launch: []string{"sh", "-c", "sleep 30"}})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	panelsOf := func() []revier.Panel {
		instances, err := h.Instances(c)
		if err != nil {
			t.Fatalf("Instances: %v", err)
		}
		for _, inst := range instances {
			if inst.Ref.ID == ref.ID {
				return inst.Panels
			}
		}
		t.Fatalf("instances = %+v, want %s among them", instances, ref.ID)
		return nil
	}
	panels := panelsOf()
	if len(panels) != 2 {
		t.Fatalf("panels = %+v, want two", panels)
	}

	if err := h.ClosePanel(c, ref, panels[1].ID); err != nil {
		t.Fatalf("ClosePanel: %v", err)
	}
	if left := panelsOf(); len(left) != 1 || left[0].ID != panels[0].ID {
		t.Fatalf("after ClosePanel = %+v, want the session with its first pane", left)
	}

	if err := h.Close(c, ref); err != nil {
		t.Fatalf("Close: %v", err)
	}
	instances, _ := h.Instances(c)
	if len(instances) != 1 || instances[0].Ref.ID != other.ID {
		t.Errorf("after Close = %+v, want the other session alone", instances)
	}

	// The same ref against a server that restarted names another session:
	// the id is refused rather than killing whatever holds it now.
	stale := revier.TargetRef{Host: "tmux", ID: refIDOf(t, "1", other.ID)}
	if err := h.Close(c, stale); err == nil {
		t.Error("Close of a ref from another server succeeded")
	}
	if instances, _ = h.Instances(c); len(instances) != 1 {
		t.Errorf("after the refused Close = %+v, want the other session still there", instances)
	}
}

// refIDOf is an instance id for a session on another server: the pid of the
// ref, replaced.
func refIDOf(t *testing.T, serverPID, id string) string {
	t.Helper()
	return serverPID + id[strings.LastIndex(id, "/"):]
}

// A tab is a window of the instance's session, after every window it holds
// even when an earlier one was closed: a save records agents in listing order.
// Its panels are split beside the first, the vars land on the first, and the
// panel returned is the first.
func TestOpenTabAppendsAWindowToTheSession(t *testing.T) {
	h, c := server(t), ctx(t)
	dir := t.TempDir()
	ref, err := h.Open(c, revier.Realization{Name: "session:demo", Launch: []string{"sh", "-c", "sleep 30"}})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	gap, err := h.OpenTab(c, ref, revier.Realization{Launch: []string{"sh", "-c", "sleep 30"}}, nil)
	if err != nil {
		t.Fatalf("OpenTab: %v", err)
	}
	if _, err := h.OpenTab(c, ref, revier.Realization{Launch: []string{"sh", "-c", "sleep 30"}}, nil); err != nil {
		t.Fatalf("OpenTab: %v", err)
	}
	if out, err := exec.Command("tmux", "-L", h.Socket, "kill-window", "-t", gap.String()).CombinedOutput(); err != nil {
		t.Fatalf("kill-window: %v\n%s", err, out)
	}

	first, err := h.OpenTab(c, ref, revier.Realization{Panels: []revier.PanelSpec{
		{Kind: revier.PanelAgent, Title: "agent", Command: []string{"sh", "-c", "sleep 30"}, Dir: dir},
		{Kind: revier.PanelShell, Title: "shell", Command: []string{"sh", "-c", "sleep 30"}, Dir: dir},
	}}, map[string]string{"REVIER_TARGET": "notes"})
	if err != nil {
		t.Fatalf("OpenTab: %v", err)
	}
	instances, err := h.Instances(c)
	if err != nil {
		t.Fatalf("Instances: %v", err)
	}
	if len(instances) != 1 {
		t.Fatalf("instances = %+v, want the tab inside the one session", instances)
	}
	panels := instances[0].Panels
	if len(panels) != 4 {
		t.Fatalf("panels = %+v, want the first window, the second tab, and the new tab's two panes", panels)
	}
	agent, shell := panels[2], panels[3]
	if agent.ID != first || agent.Title != "agent" || shell.Title != "shell" {
		t.Errorf("last two panels = %+v, %+v, want the new tab's agent (%s) then its shell", agent, shell, first)
	}
	if agent.Vars["REVIER_TARGET"] != "notes" || shell.Vars != nil {
		t.Errorf("vars = %v and %v, want them on the first panel only", agent.Vars, shell.Vars)
	}
	if got := paneValue(t, h, shell.ID, "#{window_id}"); got != paneValue(t, h, agent.ID, "#{window_id}") {
		t.Errorf("the shell is in window %s, want it split into the agent's", got)
	}
	if got := paneValue(t, h, shell.ID, "#{pane_current_path}"); got != dir {
		t.Errorf("shell cwd = %q, want %q", got, dir)
	}
}

// A tab opens without becoming current, and FocusPanel makes it current in
// its session: the window and the pane inside it.
func TestFocusPanelSwitchesTabAndPane(t *testing.T) {
	h, c := server(t), ctx(t)
	ref, err := h.Open(c, revier.Realization{Name: "session:demo", Launch: []string{"sh", "-c", "sleep 30"}})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	home, err := h.FocusedPanel(c, ref)
	if err != nil {
		t.Fatalf("FocusedPanel: %v", err)
	}
	tab, err := h.OpenTab(c, ref, revier.Realization{Panels: []revier.PanelSpec{
		{Command: []string{"sh", "-c", "sleep 30"}},
		{Command: []string{"sh", "-c", "sleep 30"}},
	}}, nil)
	if err != nil {
		t.Fatalf("OpenTab: %v", err)
	}
	if cur, _ := h.FocusedPanel(c, ref); cur != home {
		t.Fatalf("current = %s after OpenTab, want %s kept", cur, home)
	}
	for _, want := range []revier.PanelID{tab, home} {
		if err := h.FocusPanel(c, ref, want); err != nil {
			t.Fatalf("FocusPanel %s: %v", want, err)
		}
		if cur, err := h.FocusedPanel(c, ref); err != nil || cur != want {
			t.Fatalf("FocusedPanel = %s, %v, want %s", cur, err, want)
		}
	}
}

// A focus left on a session that has since closed is no focus.
func TestFocusedIsZeroOnceTheFocusedSessionCloses(t *testing.T) {
	h, c := server(t), ctx(t)
	gone, err := h.Open(c, revier.Realization{Name: "gone", Launch: []string{"sh", "-c", "sleep 30"}})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := h.Open(c, revier.Realization{Name: "kept", Launch: []string{"sh", "-c", "sleep 30"}}); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := h.Focus(c, gone); err != nil {
		t.Fatalf("Focus: %v", err)
	}
	if out, err := exec.Command("tmux", "-L", h.Socket, "kill-session", "-t", "gone").CombinedOutput(); err != nil {
		t.Fatalf("kill-session: %v\n%s", err, out)
	}
	got, err := h.Focused(c)
	if err != nil || !got.IsZero() {
		t.Errorf("Focused = %+v, %v, want a zero ref", got, err)
	}
}

func TestInstancesReportPanels(t *testing.T) {
	h, c := server(t), ctx(t)
	if _, err := h.Open(c, revier.Realization{
		Name: "home", Launch: []string{"sh", "-c", "sleep 30"},
		Match: revier.Match{Title: "^home$"},
	}); err != nil {
		t.Fatalf("Open: %v", err)
	}

	instances, err := h.Instances(c)
	if err != nil {
		t.Fatalf("Instances: %v", err)
	}
	if len(instances) != 1 {
		t.Fatalf("got %d instances, want 1", len(instances))
	}
	if len(instances[0].Panels) == 0 {
		t.Fatal("want at least one panel: a probe reads nothing else")
	}
	if instances[0].Panels[0].ID == "" {
		t.Error("panel has no id")
	}
}

// The whole product against a real substrate: run-or-raise opens the target the
// first time, raises it the second, and toggles home on the third.
func TestCoreRunOrRaiseAgainstRealTmux(t *testing.T) {
	h, c := server(t), ctx(t)
	cr := &core.Core{Runtime: h}
	p := revier.Project{
		Name: "revier",
		Targets: []revier.Target{
			{Name: "home", Home: true, Runtime: &revier.Realization{
				Name: "home", Launch: []string{"sh", "-c", "sleep 30"},
				Match: revier.Match{Title: "^home$"},
			}},
			{Name: "diff", Key: "ctrl-shift-d", Runtime: &revier.Realization{
				Name: "diff", Launch: []string{"sh", "-c", "sleep 30"},
				Match: revier.Match{Title: "^diff$"},
			}},
		},
	}

	prepared, err := core.PrepareProject(p)
	if err != nil {
		t.Fatalf("PrepareProject: %v", err)
	}
	homeRef, err := cr.Go(c, prepared, "home", nil)
	if err != nil {
		t.Fatalf("open home: %v", err)
	}
	diffRef, err := cr.Go(c, prepared, "diff", nil)
	if err != nil {
		t.Fatalf("open diff: %v", err)
	}
	if diffRef.Ref.ID == homeRef.Ref.ID {
		t.Fatal("diff and home must be different windows")
	}

	// Opening left diff focused, so the second press is the round trip home.
	back, err := cr.Go(c, prepared, "diff", nil)
	if err != nil {
		t.Fatalf("toggle back: %v", err)
	}
	if back.Ref.ID != homeRef.Ref.ID {
		t.Fatalf("toggle-back returned %s, want home %s", back.Ref.ID, homeRef.Ref.ID)
	}

	// Third press raises the existing window rather than opening a duplicate.
	again, err := cr.Go(c, prepared, "diff", nil)
	if err != nil {
		t.Fatalf("raise diff: %v", err)
	}
	if again.Ref.ID != diffRef.Ref.ID {
		t.Errorf("raise returned %s, want the existing %s", again.Ref.ID, diffRef.Ref.ID)
	}
	instances, err := h.Instances(c)
	if err != nil {
		t.Fatalf("Instances: %v", err)
	}
	if len(instances) != 2 {
		t.Fatalf("got %d windows, want 2: run-or-raise must not duplicate", len(instances))
	}
}

// tmux escapes non-printable bytes in format output on some versions and not
// others: 3.4 renders a raw \x1f as the literal text "\037" while 3.7 emits the
// byte. A control-character delimiter therefore parsed on the author's machine
// and failed in CI. The separator is printable now, and free text is the last
// field of its query so it may contain the separator itself.
func TestFreeTextContainingTheSeparatorSurvives(t *testing.T) {
	h, c := server(t), ctx(t)
	if _, err := h.Open(c, revier.Realization{
		Name: "a|b", Launch: []string{"sh", "-c", "sleep 30"},
		Match: revier.Match{Title: `^a\|b$`},
	}); err != nil {
		t.Fatalf("Open: %v", err)
	}

	instances, err := h.Instances(c)
	if err != nil {
		t.Fatalf("Instances: %v", err)
	}
	if len(instances) != 1 {
		t.Fatalf("got %d instances, want 1", len(instances))
	}
	if instances[0].Title != "a|b" {
		t.Errorf("window title = %q, want %q", instances[0].Title, "a|b")
	}

	// A pane title carrying the separator must arrive byte-exact: the Claude
	// probe reads it as the agent's activity line.
	title := "⠧ Working on a|b now"
	if _, err := h.Focused(c); err != nil {
		t.Fatalf("Focused: %v", err)
	}
	if err := exec.Command("tmux", "-L", h.Socket, "select-pane",
		"-t", instances[0].Panels[0].ID.String(), "-T", title).Run(); err != nil {
		t.Fatalf("set pane title: %v", err)
	}
	again, err := h.Instances(c)
	if err != nil {
		t.Fatalf("Instances: %v", err)
	}
	if got := again[0].Panels[0].Title; got != title {
		t.Errorf("pane title = %q, want %q", got, title)
	}
}

// A home target is its panels: each becomes a pane of the one window, in the
// project directory, with its title and its command, and the probe can tell
// the agent pane from the shell beside it.
func TestOpenBuildsThePanels(t *testing.T) {
	h, c := server(t), ctx(t)
	dir := t.TempDir()
	ref, err := h.Open(c, revier.Realization{
		Name: "session:demo", Dir: dir, Match: revier.Match{Title: "^session:demo$"},
		Panels: []revier.PanelSpec{
			{Kind: revier.PanelAgent, Title: "agent", Command: []string{"sh", "-c", "sleep 30"}, Dir: dir},
			{Kind: revier.PanelShell, Title: "shell", Command: []string{"sh", "-c", "sleep 30"}, Dir: dir},
		},
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	instances, err := h.Instances(c)
	if err != nil {
		t.Fatalf("Instances: %v", err)
	}
	if len(instances) != 1 || instances[0].Ref.ID != ref.ID {
		t.Fatalf("instances = %+v, want the one window opened", instances)
	}
	panels := instances[0].Panels
	if len(panels) != 2 {
		t.Fatalf("got %d panels, want 2: one pane per panel spec", len(panels))
	}
	if panels[0].Title != "agent" || panels[1].Title != "shell" {
		t.Errorf("titles = %q, %q", panels[0].Title, panels[1].Title)
	}
	out, err := exec.Command("tmux", "-L", h.Socket, "display-message", "-p", "-t", panels[1].ID.String(), "#{pane_current_path}").Output()
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(out)); got != dir {
		t.Errorf("pane cwd = %q, want the panel's dir %q", got, dir)
	}
}

// Each panel starts in its own directory, which need not be the realization's:
// the core fills a panel's directory, and the host only follows it.
func TestOpenStartsAPanelInItsOwnDirectory(t *testing.T) {
	h, c := server(t), ctx(t)
	dir, own := t.TempDir(), t.TempDir()
	_, err := h.Open(c, revier.Realization{
		Name: "session:demo", Dir: dir,
		Panels: []revier.PanelSpec{
			{Kind: revier.PanelAgent, Command: []string{"sh", "-c", "sleep 30"}, Dir: own},
			{Kind: revier.PanelShell, Command: []string{"sh", "-c", "sleep 30"}, Dir: dir},
		},
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	instances, err := h.Instances(c)
	if err != nil {
		t.Fatalf("Instances: %v", err)
	}
	if len(instances) != 1 || len(instances[0].Panels) != 2 {
		t.Fatalf("instances = %+v, want one window of two panes", instances)
	}
	for i, want := range []string{own, dir} {
		if got := paneValue(t, h, instances[0].Panels[i].ID, "#{pane_current_path}"); got != want {
			t.Errorf("pane %d starts in %q, want %q", i, got, want)
		}
	}
}

// paneValue is one format of one pane.
func paneValue(t *testing.T, h *tmux.Host, pane revier.PanelID, format string) string {
	t.Helper()
	out, err := exec.Command("tmux", "-L", h.Socket, "display-message", "-p", "-t", pane.String(), format).Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

// Text arrives in the pane as typed, whatever it holds: a leading dash that
// send-keys would read as a flag, a word that is a tmux key name, quotes, a
// backslash, the format separator. The "\r" sent after it is the Enter that
// submits it.
func TestSendTextTypesIntoThePane(t *testing.T) {
	h, c := server(t), ctx(t)
	out := filepath.Join(t.TempDir(), "typed")
	if _, err := h.Open(c, revier.Realization{
		Name: "agent", Match: revier.Match{Title: "^agent$"},
		Launch: []string{"sh", "-c", `IFS= read -r line; printf '%s' "$line" > "$0"; sleep 30`, out},
	}); err != nil {
		t.Fatalf("Open: %v", err)
	}
	instances, err := h.Instances(c)
	if err != nil || len(instances) != 1 {
		t.Fatalf("Instances = %+v, %v", instances, err)
	}
	inst := instances[0]

	var w revier.PanelWriter = h
	text := `-t Enter "it's" a\b|c`
	for _, s := range []string{text, "\r"} {
		if err := w.SendText(c, inst.Ref, inst.Panels[0].ID, s); err != nil {
			t.Fatalf("SendText %q: %v", s, err)
		}
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		got, err := os.ReadFile(out)
		if err == nil && len(got) > 0 {
			if string(got) != text {
				t.Fatalf("pane read %q, want %q", got, text)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the pane read nothing within 5s: the Enter did not submit the line")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// tmux expands -n, -c and -T as formats. A '#' in a project name, a pane
// title or a directory has to arrive as written: "C#" otherwise names the
// window "C", which the project's match never finds, and a directory with a
// '#' starts the pane in $HOME. tmux keeps a run of '#' before '[' as it is,
// so that case is here as well.
func TestHashSurvivesNameTitleAndDirectory(t *testing.T) {
	h, c := server(t), ctx(t)
	for i, name := range []string{"session:C#", "a#{b}", "x#[y]", "p##[q", "##", "e#"} {
		dir := filepath.Join(t.TempDir(), name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		ref, err := h.Open(c, revier.Realization{
			Name: name, Dir: dir,
			Panels: []revier.PanelSpec{{Title: name, Command: []string{"sh", "-c", "sleep 30"}, Dir: dir}},
		})
		if err != nil {
			t.Fatalf("Open %q: %v", name, err)
		}
		instances, err := h.Instances(c)
		if err != nil {
			t.Fatalf("Instances: %v", err)
		}
		if len(instances) != i+1 {
			t.Fatalf("got %d instances, want %d", len(instances), i+1)
		}
		var inst revier.Instance
		for _, in := range instances {
			if in.Ref.ID == ref.ID {
				inst = in
			}
		}
		if inst.Title != name {
			t.Errorf("window name = %q, want %q", inst.Title, name)
		}
		if got := inst.Panels[0].Title; got != name {
			t.Errorf("pane title = %q, want %q", got, name)
		}
		out, err := exec.Command("tmux", "-L", h.Socket, "display-message", "-p", "-t", inst.Panels[0].ID.String(), "#{pane_current_path}").Output()
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.TrimSpace(string(out)); got != dir {
			t.Errorf("pane cwd = %q, want %q", got, dir)
		}
	}
}

// A one-element launch is an argv like any other: tmux would hand a single
// argument to the shell as a command line, where a space, a '&' or a ';' in
// the program's path breaks it.
func TestOneElementLaunchRunsWithoutAShell(t *testing.T) {
	h, c := server(t), ctx(t)
	dir := filepath.Join(t.TempDir(), "a b&c;d")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	ran := filepath.Join(dir, "ran")
	program := filepath.Join(dir, "run me")
	script := "#!/bin/sh\ntouch '" + ran + "'\nsleep 30\n"
	if err := os.WriteFile(program, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Open(c, revier.Realization{
		Name: "one", Launch: []string{program}, Match: revier.Match{Title: "^one$"},
	}); err != nil {
		t.Fatalf("Open: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(ran); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%q did not run within 5s", program)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// tmux numbers windows per server from @0, so a new server hands out the ids
// an old one used. A binding kept from before a restart must not raise the
// unrelated window that now holds its old number.
func TestABindingDoesNotOutliveItsServer(t *testing.T) {
	h, c := server(t), ctx(t)
	cr := &core.Core{Runtime: h}
	p, err := core.PrepareProject(revier.Project{
		Name: "revier",
		Targets: []revier.Target{
			{Name: "home", Home: true, Runtime: &revier.Realization{
				Name: "home", Launch: []string{"sh", "-c", "sleep 30"},
				Match: revier.Match{Title: "^home$"},
			}},
			{Name: "diff", Runtime: &revier.Realization{
				Name: "diff", Launch: []string{"sh", "-c", "sleep 30"},
				Match: revier.Match{Title: "^diff$"},
			}},
		},
	})
	if err != nil {
		t.Fatalf("PrepareProject: %v", err)
	}
	diff, err := cr.Go(c, p, "diff", nil)
	if err != nil {
		t.Fatalf("open diff: %v", err)
	}
	killServer(t, h.Socket)
	if _, err := cr.Go(c, p, "home", nil); err != nil {
		t.Fatalf("open home on the new server: %v", err)
	}

	res, err := cr.Go(c, p, "diff", core.Bindings{"diff": diff.Ref})
	if err != nil {
		t.Fatalf("go diff: %v", err)
	}
	if !res.Launched {
		t.Fatalf("go diff raised %q: the binding from the old server landed on another window", res.Ref.Title)
	}
}
