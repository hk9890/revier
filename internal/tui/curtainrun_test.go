package tui_test

import (
	"errors"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/curtain"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/internal/tui"
	"github.com/hk9890/revier/pkg/revier"
)

// loop runs a model as a bubbletea program does: each command in a goroutine
// of its own, and each message it answers fed to Update in turn. A walk under
// a curtain waits on the curtain's socket beside the walk, which the batch
// helpers, running a batch in order, would wait on forever.
type loop struct {
	t    *testing.T
	m    tui.Model
	msgs chan tea.Msg
	done chan struct{}
	quit bool
}

func loopOf(t *testing.T, m tui.Model, cmd tea.Cmd) *loop {
	t.Helper()
	l := &loop{t: t, m: m, msgs: make(chan tea.Msg), done: make(chan struct{})}
	t.Cleanup(func() { close(l.done) })
	l.exec(cmd)
	return l
}

func (l *loop) exec(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	go func() {
		select {
		case l.msgs <- cmd():
		case <-l.done:
		}
	}()
}

// until feeds messages until cond holds, and fails the test after a while.
func (l *loop) until(what string, cond func(tui.Model) bool) {
	l.t.Helper()
	deadline := time.After(5 * time.Second)
	for !cond(l.m) {
		select {
		case msg := <-l.msgs:
			l.feed(msg)
		case <-time.After(10 * time.Millisecond):
		case <-deadline:
			l.t.Fatalf("waited 5s for %s", what)
		}
	}
}

func (l *loop) feed(msg tea.Msg) {
	switch msg := msg.(type) {
	case nil:
		return
	case tea.BatchMsg:
		for _, c := range msg {
			l.exec(c)
		}
		return
	case tea.QuitMsg:
		l.quit = true
		return
	}
	next, cmd := l.m.Update(msg)
	l.m = next.(tui.Model)
	l.exec(cmd)
}

// watcher is a curtain's end of the socket: every state it was sent, and
// whether the socket ended.
type watcher struct {
	client *curtain.Client
	mu     sync.Mutex
	states []curtain.State
	ended  bool
}

func watch(t *testing.T, path string) *watcher {
	t.Helper()
	c, err := curtain.Dial(path)
	if err != nil {
		t.Errorf("dial the curtain's socket: %v", err)
		return &watcher{ended: true}
	}
	t.Cleanup(c.Close)
	w := &watcher{client: c}
	go func() {
		for st := range c.States() {
			w.mu.Lock()
			w.states = append(w.states, st)
			w.mu.Unlock()
		}
		w.mu.Lock()
		w.ended = true
		w.mu.Unlock()
	}()
	return w
}

// last is the latest state, and whether the socket has ended.
func (w *watcher) last() (curtain.State, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.states) == 0 {
		return curtain.State{}, w.ended
	}
	return w.states[len(w.states)-1], w.ended
}

// waitFor waits until a state the curtain was sent holds cond.
func (w *watcher) waitFor(t *testing.T, what string, cond func(curtain.State) bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if st, _ := w.last(); cond(st) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Errorf("waited 5s for a curtain state with %s", what)
}

// curtained is a world whose window host has two monitors, the primary one
// second, and a popup window to raise; and the launch a curtain gets, which
// hands each socket it is given to the test.
func curtained(t *testing.T, n int) (*hosttest.FakeRuntime, *hosttest.Fake, *core.Core, []core.Project, chan string) {
	t.Helper()
	dir, err := os.MkdirTemp("", "rc")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	t.Setenv("XDG_RUNTIME_DIR", dir)
	rt, wm, c, projects := world(t, n)
	wm.MonitorList = []revier.Monitor{{Index: 0}, {Index: 1, Primary: true}}
	wm.Add("revier", core.PopupClass)
	return rt, wm, c, projects, make(chan string, 8)
}

// underCurtain is the popup's surface with a curtain's launch that hands its
// socket on.
func underCurtain(m tui.Model, sockets chan<- string) tui.Model {
	return m.WithPopup().WithCurtain(func(_ revier.Monitor, socket string, main bool) []string {
		sockets <- socket
		return []string{"curtain", socket}
	})
}

// socketOf is the socket the first curtain was launched with. It runs on the
// walk's goroutine too, so it fails the test without stopping it.
func socketOf(t *testing.T, sockets <-chan string) string {
	t.Helper()
	select {
	case s := <-sockets:
		return s
	case <-time.After(5 * time.Second):
		t.Error("no curtain was launched")
		return ""
	}
}

// curtainRefs are the curtain windows the host lists, and whether each is
// minimized.
func curtainRefs(t *testing.T, wm *hosttest.Fake) map[string]bool {
	t.Helper()
	windows, err := wm.Instances(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	for _, w := range windows {
		if w.Class == core.CurtainClass {
			out[w.Ref.ID] = w.Minimized
		}
	}
	return out
}

func covered(ops []string) int {
	n := 0
	for _, op := range ops {
		if strings.HasPrefix(op, "cover ") {
			n++
		}
	}
	return n
}

// A full shutdown from the popup covers every monitor before its first close,
// sends each step to the curtains, and lifts them once it went well
// (decisions.md D192).
func TestAFullShutdownFromThePopupRunsUnderACurtain(t *testing.T) {
	rt, wm, c, projects, sockets := curtained(t, 2)
	root := stateWith(t, nil)
	m := underCurtain(resize(refreshed(t, c, projects, root, nil), 150, 30), sockets)
	m, _ = press(m, "alt+q")
	m, cmd := press(m, "enter")
	m = run(m, cmd)

	coversAtClose := -1
	var seen *watcher
	rt.OnClose = func(revier.TargetRef) {
		coversAtClose = covered(wm.Ops())
		seen = watch(t, socketOf(t, sockets))
		seen.waitFor(t, "the close running", func(st curtain.State) bool {
			return st.Mode == "shutdown" && slices.ContainsFunc(st.Rows, func(r curtain.Row) bool {
				return r.Project == "project-01" && r.Label == "home" && r.State == curtain.RowRunning
			})
		})
	}
	m, cmd = press(m, "enter")
	l := loopOf(t, m, cmd)
	l.until("the shutdown's result", func(m tui.Model) bool { return strings.Contains(strings.Join(rows(m), "\n"), "Shutdown complete") })

	if seen == nil {
		t.Fatal("the shutdown closed nothing")
	}
	if coversAtClose != 2 {
		t.Errorf("covers at the first close = %d, want both monitors covered before it", coversAtClose)
	}
	seen.waitFor(t, "the end", func(st curtain.State) bool { return st.End != nil })
	l.until("the curtains lifted", func(tui.Model) bool { _, ended := seen.last(); return ended })
	if st, _ := seen.last(); !st.End.OK || st.End.Title != "Shutdown done" || st.N != st.Of {
		t.Errorf("last state = %+v, end %+v; want a shutdown done with every step walked", st, st.End)
	}
}

// restoreUnder starts the restore of the newest session from the sessions
// screen of a popup under a curtain.
func restoreUnder(t *testing.T, c *core.Core, projects []core.Project, root string, sockets chan<- string) *loop {
	t.Helper()
	m := underCurtain(resize(refreshed(t, c, projects, root, nil), 150, 30), sockets)
	m, _ = press(m, "alt+s")
	m, cmd := press(m, "enter")
	return loopOf(t, m, cmd)
}

// restoring reports a surface whose restore still runs.
func restoring(m tui.Model) bool { return strings.Contains(barLine(m), "Sessions") }

// A restore from the popup opens under the curtains, gives the main curtain
// the keyboard back once the walk is done, lifts them, and then raises the
// popup.
func TestARestoreFromThePopupRunsUnderACurtainAndRaisesThePopupAfter(t *testing.T) {
	rt, wm, c, projects, sockets := curtained(t, 3)
	root := t.TempDir()
	saved(t, root, "", 10, homeOf("project-00"), homeOf("project-01"))
	var seen *watcher
	rt.OnOpen = func(revier.Realization) {
		if seen == nil {
			seen = watch(t, socketOf(t, sockets))
		}
	}

	l := restoreUnder(t, c, projects, root, sockets)
	l.until("the restore's end", func(m tui.Model) bool { return !restoring(m) })
	l.until("the curtains lifted", func(tui.Model) bool { _, ended := seen.last(); return ended })
	l.until("the popup raised", func(tui.Model) bool {
		ops := wm.Ops()
		return len(ops) > 0 && ops[len(ops)-1] == "focus 1"
	})

	st, _ := seen.last()
	if st.End == nil || !st.End.OK || st.End.Title != "Restore done" || st.End.Summary != "2 opened" {
		t.Errorf("end = %+v, want a restore done with 2 opened", st.End)
	}
	if len(st.Rows) != 2 || st.Rows[0].Label != "home" || st.Rows[0].State != curtain.RowDone {
		t.Errorf("rows = %+v, want both homes done", st.Rows)
	}
	main := ""
	for id := range curtainRefs(t, wm) {
		if covers := wm.Ops(); slices.Contains(covers, "cover "+id+" 1") {
			main = id
		}
	}
	focuses := 0
	for _, op := range wm.Ops() {
		if op == "focus "+main {
			focuses++
		}
	}
	if main == "" || focuses != 2 {
		t.Errorf("window calls = %q, want the main curtain focused at the open and again after the walk", wm.Ops())
	}
}

// A restore with a target that fails keeps the curtains up with the summary,
// brings them back over everything, and raises the popup only once the user
// closed the summary.
func TestACurtainStaysOverARestoreThatFailedUntilItIsClosed(t *testing.T) {
	rt, wm, c, projects, sockets := curtained(t, 2)
	rt.OpenErr = errors.New("kitty refused")
	root := t.TempDir()
	saved(t, root, "", 10, homeOf("project-00"))
	var seen *watcher
	rt.OnOpen = func(revier.Realization) { seen = watch(t, socketOf(t, sockets)) }

	l := restoreUnder(t, c, projects, root, sockets)
	l.until("the restore's end", func(m tui.Model) bool { return !restoring(m) })
	seen.waitFor(t, "the summary", func(st curtain.State) bool { return st.End != nil })
	st, ended := seen.last()
	if ended || st.End.OK || st.End.Title != "Restore finished with problems" || st.End.Summary != "0 opened · 1 failed" {
		t.Errorf("end = %+v, ended %v; want the summary of a failed step, and the curtains up", st.End, ended)
	}
	if len(st.Rows) != 1 || st.Rows[0].State != curtain.RowFailed || !strings.Contains(st.Rows[0].Note, "kitty refused") {
		t.Errorf("rows = %+v, want the failed step with why", st.Rows)
	}
	l.until("the curtains shown again", func(tui.Model) bool {
		ops := wm.Ops()
		return len(ops) > 0 && strings.HasPrefix(ops[len(ops)-1], "above ") && strings.HasSuffix(ops[len(ops)-1], " on")
	})
	if ops := wm.Ops(); slices.Contains(ops, "focus 1") {
		t.Errorf("window calls = %q, want no raise of the popup under the summary", ops)
	}

	if err := seen.client.Send(curtain.Close); err != nil {
		t.Fatal(err)
	}
	l.until("the curtains closed", func(tui.Model) bool { _, ended := seen.last(); return ended })
	l.until("the popup raised", func(tui.Model) bool { return slices.Contains(wm.Ops(), "focus 1") })
}

// A cancel on the curtain is the first ctrl+c: the walk stops after the step
// that runs, the curtain says so, and its summary stays.
func TestACancelOnTheCurtainCancelsTheWalk(t *testing.T) {
	rt, _, c, projects, sockets := curtained(t, 3)
	root := t.TempDir()
	saved(t, root, "", 10, homeOf("project-00"), homeOf("project-01"))
	var seen *watcher
	rt.OnOpen = func(revier.Realization) {
		if seen != nil {
			return
		}
		seen = watch(t, socketOf(t, sockets))
		if err := seen.client.Send(curtain.Cancel); err != nil {
			t.Error(err)
		}
		seen.waitFor(t, "the cancel taken", func(st curtain.State) bool { return st.Cancelling })
	}

	l := restoreUnder(t, c, projects, root, sockets)
	l.until("the restore's end", func(m tui.Model) bool { return !restoring(m) })
	seen.waitFor(t, "the summary", func(st curtain.State) bool { return st.End != nil })
	st, ended := seen.last()
	if ended || st.End.Title != "Restore cancelled" || st.End.Summary != "1 opened · 1 not reached" {
		t.Errorf("end = %+v, ended %v; want the cancel's summary, and the curtains up", st.End, ended)
	}
	if foot := footer(l.m); !strings.Contains(foot, "cancelled: 1 target not reached") {
		t.Errorf("footer = %q, want the surface's own word on the cancel", foot)
	}
}

// Esc on the curtain hides every curtain and leaves the walk running.
func TestAHideOnTheCurtainHidesThemAndTheWalkGoesOn(t *testing.T) {
	rt, wm, c, projects, sockets := curtained(t, 2)
	root := t.TempDir()
	saved(t, root, "", 10, homeOf("project-00"))
	var seen *watcher
	rt.OnOpen = func(revier.Realization) {
		seen = watch(t, socketOf(t, sockets))
		if err := seen.client.Send(curtain.Hide); err != nil {
			t.Error(err)
		}
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			refs := curtainRefs(t, wm)
			if len(refs) == 2 && !slices.Contains(mapValues(refs), false) {
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Error("the curtains were not hidden while the walk ran")
	}

	l := restoreUnder(t, c, projects, root, sockets)
	l.until("the restore's end", func(m tui.Model) bool { return !restoring(m) })
	l.until("the curtains lifted", func(tui.Model) bool { _, ended := seen.last(); return ended })
	if foot := footer(l.m); l.quit || faulted(foot) {
		t.Errorf("quit %v, footer %q; want the restore done and the surface up", l.quit, foot)
	}
}

// faulted reports a footer that says a walk did not go well.
func faulted(foot string) bool {
	return strings.Contains(foot, "failed") || strings.Contains(foot, "cancelled") || strings.Contains(foot, "did not open")
}

func mapValues(m map[string]bool) []bool {
	var out []bool
	for _, v := range m {
		out = append(out, v)
	}
	return out
}

// A popup with no curtain's launch opens no curtain.
func TestARestoreWithoutACurtainOpensNone(t *testing.T) {
	_, wm, c, projects, _ := curtained(t, 2)
	root := t.TempDir()
	saved(t, root, "", 10, homeOf("project-00"))
	m := resize(refreshed(t, c, projects, root, nil), 150, 30).WithPopup()
	m, _ = press(m, "alt+s")
	m, cmd := press(m, "enter")
	m = run(m, cmd)

	if restoring(m) || len(curtainRefs(t, wm)) != 0 || covered(wm.Ops()) != 0 {
		t.Errorf("window calls = %q, want the restore done and no curtain", wm.Ops())
	}
}

// A curtain that does not open costs the restore nothing: it runs without
// one and raises the popup at its end.
func TestACurtainThatFailsToOpenDoesNotFailTheRestore(t *testing.T) {
	_, wm, c, projects, sockets := curtained(t, 2)
	wm.MonitorsErr = errors.New("wctl: not running")
	root := t.TempDir()
	saved(t, root, "", 10, homeOf("project-00"))

	l := restoreUnder(t, c, projects, root, sockets)
	l.until("the restore's end", func(m tui.Model) bool { return !restoring(m) })
	l.until("the popup raised", func(tui.Model) bool { return slices.Contains(wm.Ops(), "focus 1") })
	if foot := footer(l.m); faulted(foot) || len(sockets) != 0 {
		t.Errorf("footer %q, curtains launched %d; want a restore with no curtain and no error", foot, len(sockets))
	}
}

// fullShutdownUnder starts a full shutdown from the popup under a curtain.
// The first close dials the curtain and hands it to first; seen is the
// curtain once it was dialed, nil before.
func fullShutdownUnder(t *testing.T, rt *hosttest.FakeRuntime, c *core.Core, projects []core.Project, sockets chan string, first func(*watcher)) (l *loop, seen func() *watcher) {
	t.Helper()
	root := stateWith(t, nil)
	m := underCurtain(resize(refreshed(t, c, projects, root, nil), 150, 30), sockets)
	m, _ = press(m, "alt+q")
	m, cmd := press(m, "enter")
	m = run(m, cmd)
	var mu sync.Mutex
	var dialed *watcher
	rt.OnClose = func(revier.TargetRef) {
		mu.Lock()
		if dialed != nil {
			mu.Unlock()
			return
		}
		dialed = watch(t, socketOf(t, sockets))
		w := dialed
		mu.Unlock()
		first(w)
	}
	m, cmd = press(m, "enter")
	return loopOf(t, m, cmd), func() *watcher {
		mu.Lock()
		defer mu.Unlock()
		return dialed
	}
}

// summarized waits for the curtain's end and returns it.
func summarized(l *loop, seen func() *watcher) (*watcher, curtain.End) {
	l.t.Helper()
	var end curtain.End
	l.until("the curtain's summary", func(tui.Model) bool {
		w := seen()
		if w == nil {
			return false
		}
		st, _ := w.last()
		if st.End != nil {
			end = *st.End
		}
		return st.End != nil
	})
	return seen(), end
}

// A full shutdown whose close failed keeps its summary up, and the popup
// quits only once the user closed it: the quit would end the curtains with
// it.
func TestAFullShutdownWithProblemsQuitsOnlyOnceItsSummaryIsClosed(t *testing.T) {
	rt, _, c, projects, sockets := curtained(t, 2)
	rt.CloseErr = errors.New("kitty refused")
	l, seen := fullShutdownUnder(t, rt, c, projects, sockets, func(*watcher) {})
	w, end := summarized(l, seen)
	if end.OK || end.Title != "Shutdown finished with problems" {
		t.Fatalf("end = %+v, want a shutdown with problems", end)
	}
	start := time.Now()
	l.until("the wait a full shutdown quits after", func(tui.Model) bool { return time.Since(start) > 2*time.Second })
	if _, ended := w.last(); l.quit || ended {
		t.Fatalf("quit %v, curtains ended %v; want both held by the summary", l.quit, ended)
	}

	if err := w.client.Send(curtain.Close); err != nil {
		t.Fatal(err)
	}
	l.until("the popup quit", func(tui.Model) bool { return l.quit })
}

// A full shutdown cancelled on the curtain keeps the popup: once the user
// closed the summary, the popup comes back from under the curtains' focus to
// show its own result.
func TestACancelledFullShutdownRaisesThePopupOnceItsSummaryIsClosed(t *testing.T) {
	rt, wm, c, projects, sockets := curtained(t, 3)
	rt.Add("session:project-00", "kitty", revier.Panel{ID: "2", Kind: revier.PanelTool, Title: "claude"})
	l, seen := fullShutdownUnder(t, rt, c, projects, sockets, func(w *watcher) {
		if err := w.client.Send(curtain.Cancel); err != nil {
			t.Error(err)
		}
		w.waitFor(t, "the cancel taken", func(st curtain.State) bool { return st.Cancelling })
	})
	w, end := summarized(l, seen)
	if end.Title != "Shutdown cancelled" {
		t.Fatalf("end = %+v, want the cancel's", end)
	}
	if ops := wm.Ops(); slices.Contains(ops, "focus 1") {
		t.Errorf("window calls = %q, want no raise of the popup under the summary", ops)
	}

	if err := w.client.Send(curtain.Close); err != nil {
		t.Fatal(err)
	}
	l.until("the popup raised", func(tui.Model) bool { return slices.Contains(wm.Ops(), "focus 1") })
	if l.quit {
		t.Error("the popup quit after a cancelled shutdown, want it kept")
	}
}

// Curtains that all went some other way than the summary's Close, closed by
// the window manager or crashed, end their run: the popup comes back, and
// the socket is free for the next walk.
func TestCurtainsThatAllWentEndTheirRun(t *testing.T) {
	rt, wm, c, projects, sockets := curtained(t, 2)
	rt.OpenErr = errors.New("kitty refused")
	root := t.TempDir()
	saved(t, root, "", 10, homeOf("project-00"))
	var seen *watcher
	rt.OnOpen = func(revier.Realization) { seen = watch(t, socketOf(t, sockets)) }

	l := restoreUnder(t, c, projects, root, sockets)
	l.until("the restore's end", func(m tui.Model) bool { return !restoring(m) })
	seen.waitFor(t, "the summary", func(st curtain.State) bool { return st.End != nil })
	seen.client.Close()
	l.until("the popup raised", func(tui.Model) bool { return slices.Contains(wm.Ops(), "focus 1") })
	next, err := curtain.Listen()
	if err != nil {
		t.Fatalf("Listen after the curtains went: %v, want the socket free", err)
	}
	next.Close()
}
