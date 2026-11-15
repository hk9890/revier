//go:build integration

// Layer L3: the kitty view over a fake hc runtime, against a workspace kitty
// that a test plays from what the view asked of it: ls answers with the
// windows its launches made, in the shape of a recorded kitty 0.48 listing.
// A real view opens windows on a desktop, which no test may reach.
package kitty_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hk9890/revier/internal/adapter/kitty"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/pkg/revier"
)

// The subset of a `kitten @ ls` listing the view reads.
type (
	playedOS struct {
		ID        int         `json:"id"`
		WMName    string      `json:"wm_name"`
		IsFocused bool        `json:"is_focused"`
		Tabs      []playedTab `json:"tabs"`
	}
	playedTab struct {
		ID       int            `json:"id"`
		IsActive bool           `json:"is_active"`
		Windows  []playedWindow `json:"windows"`
	}
	playedWindow struct {
		ID       int               `json:"id"`
		IsActive bool              `json:"is_active"`
		UserVars map[string]string `json:"user_vars"`
	}
)

// playedKitty is the workspace kitty: a launch adds an OS window, a tab or a
// window, which takes the keyboard unless --keep-focus says not to, and
// focus-window makes a window current. lsErr fails the next lsFails ls, as a
// busy kitty times out and then answers, and slow makes an ls of no window
// wait, so two Shows at once both list before either launches.
type playedKitty struct {
	mu      sync.Mutex
	next    int
	windows []playedOS
	lsErr   error
	lsFails int
	slow    time.Duration
	focused []int
}

// played is a workspace kitty with no window, and the state directory every
// view of it shares, which holds the lock its Shows take.
func played(t *testing.T) *playedKitty {
	t.Helper()
	t.Setenv("XDG_RUNTIME_DIR", "/run/user/1000")
	t.Setenv("REVIER_STATE_HOME", t.TempDir())
	return &playedKitty{}
}

// viewOver is a view of the played kitty, as one revier process has.
func (k *playedKitty) viewOver(t *testing.T, rt kitty.Attachable) *kitty.View {
	t.Helper()
	v := &kitty.View{Runtime: rt}
	h := v.Kitty()
	*h = *newHost()
	h.SetSockets(func() []string { return []string{workspace} })
	h.SetOwnSocket(func() string { return workspace })
	h.SetRunner(func(_ context.Context, _, _ string, args ...string) ([]byte, error) {
		return k.run(args)
	})
	return v
}

func (k *playedKitty) run(args []string) ([]byte, error) {
	k.mu.Lock()
	switch args[0] {
	case "ls":
		if k.lsFails > 0 {
			k.lsFails--
			k.mu.Unlock()
			return nil, k.lsErr
		}
		raw, err := json.Marshal(k.windows)
		empty := len(k.windows) == 0
		k.mu.Unlock()
		if empty {
			time.Sleep(k.slow)
		}
		return raw, err
	case "launch":
		defer k.mu.Unlock()
		return []byte(strconv.Itoa(k.launch(args[1:]))), nil
	case "focus-window":
		defer k.mu.Unlock()
		id, _ := strconv.Atoi(strings.TrimPrefix(args[2], "id:"))
		k.focused = append(k.focused, id)
		k.current(id)
		return nil, nil
	}
	k.mu.Unlock()
	return nil, nil
}

func (k *playedKitty) launch(args []string) int {
	k.next++
	win := playedWindow{ID: k.next, UserVars: map[string]string{}}
	kind, at, keep, name := "", 0, false, ""
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case strings.HasPrefix(a, "--type="):
			kind = strings.TrimPrefix(a, "--type=")
		case a == "--match":
			i++
			at, _ = strconv.Atoi(strings.TrimPrefix(args[i], "window_id:"))
		case a == "--var":
			i++
			key, value, _ := strings.Cut(args[i], "=")
			win.UserVars[key] = value
		case a == "--os-window-name":
			i++
			name = args[i]
		case a == "--keep-focus":
			keep = true
		}
	}
	switch kind {
	case "os-window":
		k.windows = append(k.windows, playedOS{ID: k.next, WMName: name, Tabs: []playedTab{{ID: k.next, Windows: []playedWindow{win}}}})
	case "tab":
		o, _ := k.find(at)
		k.windows[o].Tabs = append(k.windows[o].Tabs, playedTab{ID: k.next, Windows: []playedWindow{win}})
	case "window":
		o, t := k.find(at)
		k.windows[o].Tabs[t].Windows = append(k.windows[o].Tabs[t].Windows, win)
	}
	if !keep {
		k.current(win.ID)
	}
	return win.ID
}

// find is the OS window and the tab of the window id.
func (k *playedKitty) find(id int) (int, int) {
	for o, w := range k.windows {
		for t, tab := range w.Tabs {
			if slices.ContainsFunc(tab.Windows, func(win playedWindow) bool { return win.ID == id }) {
				return o, t
			}
		}
	}
	panic("no window " + strconv.Itoa(id))
}

// current makes the window current in its tab, its tab active, and its OS
// window the one with the keyboard.
func (k *playedKitty) current(id int) {
	o, t := k.find(id)
	for i := range k.windows {
		k.windows[i].IsFocused = i == o
	}
	for i := range k.windows[o].Tabs {
		k.windows[o].Tabs[i].IsActive = i == t
		for j := range k.windows[o].Tabs[i].Windows {
			win := &k.windows[o].Tabs[i].Windows[j]
			win.IsActive = i == t && win.ID == id || i != t && win.IsActive
		}
	}
}

// closeAll closes every OS window, as a user quits the workspace's windows.
func (k *playedKitty) closeAll() {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.windows = nil
}

// osWindows is how many OS windows carry the title.
func (k *playedKitty) osWindows(title string) int {
	k.mu.Lock()
	defer k.mu.Unlock()
	n := 0
	for _, w := range k.windows {
		if w.WMName == title {
			n++
		}
	}
	return n
}

// panelOf is the hc panel the kitty window id attaches.
func (k *playedKitty) panelOf(id int) string {
	k.mu.Lock()
	defer k.mu.Unlock()
	o, t := k.find(id)
	for _, win := range k.windows[o].Tabs[t].Windows {
		if win.ID == id {
			return win.UserVars["REVIER_VIEW_PANEL"]
		}
	}
	return ""
}

// Two Shows at once, as two presses of two revier processes, open one OS
// window: the second lists the kitty after the first opened it, and adds
// nothing.
func TestTwoShowsAtOnceOpenOneWindow(t *testing.T) {
	k := played(t)
	k.slow = 50 * time.Millisecond
	views := []*kitty.View{k.viewOver(t, attaching{}), k.viewOver(t, attaching{})}
	opened := make([]bool, len(views))
	errs := make([]error, len(views))
	var wg sync.WaitGroup
	for i, v := range views {
		wg.Go(func() { opened[i], errs[i] = v.Show(t.Context(), demo, true) })
	}
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		t.Fatalf("Show: %v", err)
	}
	if n := k.osWindows(demo.Title); n != 1 || opened[0] == opened[1] {
		t.Errorf("%d OS windows titled %s, opened %v; want one, opened by one Show", n, demo.Title, opened)
	}
}

// A workspace kitty whose ls fails may show the instance, so the Show fails
// and opens no window beside the one it may have, also when the kitty answers
// the next ls, as a busy one does.
func TestAShowWhoseKittyDoesNotAnswerOpensNothing(t *testing.T) {
	k := played(t)
	k.lsErr, k.lsFails = errors.New("kitten @ ls: timed out"), 1
	v := k.viewOver(t, attaching{})
	if opened, err := v.Show(t.Context(), demo, true); err == nil || opened {
		t.Errorf("Show = %v, %v; want it failed", opened, err)
	}
	if n := k.osWindows(demo.Title); n != 0 {
		t.Errorf("%d OS windows, want none", n)
	}
}

// attachingRuntime is the fake hc runtime as the view attaches it.
type attachingRuntime struct{ *hosttest.FakeRuntime }

func (attachingRuntime) AttachPanel(panel revier.PanelID) ([]string, error) {
	return []string{"hc", "attach", panel.String()}, nil
}

// A press on a workspace whose kitty windows were closed opens them again,
// and ends with the active tab current, as the launch left it: each window
// the view draws takes the keyboard, so without the core's focus the last
// tab would be.
func TestAWindowOpenedAgainHasTheActiveTabCurrent(t *testing.T) {
	rt := attachingRuntime{hosttest.NewRuntime("hc")}
	k := played(t)
	c := &core.Core{Runtime: rt, View: k.viewOver(t, rt)}
	p := core.PrepareProject(revier.Project{Name: "demo", Path: "/p", Targets: []revier.Target{
		{Name: "home", Home: true, Runtime: &revier.Realization{
			Name: "demo", Match: revier.Match{Title: "^demo$"},
			Tabs: []revier.TargetName{"agent", "tickets"}, Active: "agent"}},
		{Name: "agent", Runtime: &revier.Realization{Inside: "home", Panels: []revier.PanelSpec{
			{Kind: revier.PanelAgent, Command: []string{"claude"}}, {Kind: revier.PanelShell},
		}}},
		{Name: "tickets", Runtime: &revier.Realization{Inside: "home", Launch: []string{"taskmgr-ui"}}},
	}})
	if _, _, err := c.ActivateWaiting(t.Context(), p, "home", nil); err != nil {
		t.Fatalf("press: %v", err)
	}
	active := k.panelOf(k.focused[len(k.focused)-1])

	k.closeAll()
	k.focused = nil
	if _, _, err := c.ActivateWaiting(t.Context(), p, "home", nil); err != nil {
		t.Fatalf("press again: %v", err)
	}
	if k.osWindows("demo") != 1 || len(k.focused) == 0 {
		t.Fatalf("OS windows %d, focuses %v; want the window again, focused", k.osWindows("demo"), k.focused)
	}
	if got := k.panelOf(k.focused[len(k.focused)-1]); got != active {
		t.Errorf("current panel %s, want the active agent tab's %s, as the launch left it", got, active)
	}
}
