//go:build integration

// Layer L3: the kitty view over hc against a `kitten @ ls` of the workspace
// kitty that shows an hc instance, and a recording runner. The listing is
// built from the recorded kitty 0.48 one in ls.json, with the windows that
// attach hc terminals and the vars the view gives them: a real view opens
// windows on a desktop, which no test may reach.
package kitty_test

import (
	"context"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/hk9890/revier/internal/adapter/kitty"
	"github.com/hk9890/revier/pkg/revier"
)

// workspace is the socket of the workspace kitty the fixture lists.
const workspace = "unix:/run/user/1000/revier-kitty-6000"

// demo is the hc instance the fixture shows: a tickets tab of t1, and an
// agent tab of t2 and t3.
var demo = revier.Instance{
	Ref:   revier.TargetRef{Host: "hc", ID: "core1/0a1b2c3d4e5f6a7b", Title: "session:demo"},
	Title: "session:demo",
	Panels: []revier.Panel{
		{ID: "t1", Tab: "a"}, {ID: "t2", Tab: "b"}, {ID: "t3", Tab: "b"},
	},
}

// attaching is the hc runtime as the view sees it: its name, and the attach
// of a terminal by its address.
type attaching struct{}

func (attaching) Name() string { return "hc" }

func (attaching) AttachPanel(panel revier.PanelID) ([]string, error) {
	return []string{"env", "HC_SOCKET=/s.sock", "/bin/hc", "attach", panel.String()}, nil
}

// viewOn is a view whose workspace kitty answers ls with the file and every
// launch with the next window id from 10, recording each call.
func viewOn(t *testing.T, file string) (*kitty.View, *recorder) {
	t.Helper()
	t.Setenv("XDG_RUNTIME_DIR", "/run/user/1000")
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	v := &kitty.View{Runtime: attaching{}}
	k := v.Kitty()
	*k = *newHost()
	rec := &recorder{}
	var mu sync.Mutex
	next := 10
	k.SetSockets(func() []string { return []string{"unix:/run/user/1000/kitty-4000", workspace} })
	k.SetOwnSocket(func() string { return workspace })
	k.SetRunner(func(_ context.Context, socket, _ string, args ...string) ([]byte, error) {
		rec.add(socket, args)
		switch args[0] {
		case "ls":
			if socket != workspace {
				return []byte("[]"), nil
			}
			return raw, nil
		case "launch":
			mu.Lock()
			defer mu.Unlock()
			next++
			return []byte(strconv.Itoa(next-1) + "\n"), nil
		}
		return nil, nil
	})
	return v, rec
}

// hiddenOnly is a workspace kitty that shows nothing yet: its hidden window
// alone.
func hiddenOnly(t *testing.T) string {
	t.Helper()
	path := t.TempDir() + "/ls.json"
	raw, err := os.ReadFile("testdata/ls-hc-view.json")
	if err != nil {
		t.Fatal(err)
	}
	end := strings.Index(string(raw), "\n  },\n  {")
	if err := os.WriteFile(path, []byte(string(raw[:end])+"\n  }\n]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// launches is the arguments of every launch, in order.
func launches(rec *recorder) [][]string {
	var out [][]string
	for _, c := range rec.all() {
		if c.args[0] == "launch" {
			if c.socket != workspace {
				return append(out, []string{"on another kitty: " + c.socket})
			}
			out = append(out, c.args[1:])
		}
	}
	return out
}

// An instance no window shows gets one OS window titled as the instance, a
// kitty tab per tab and a kitty window per panel, each attaching its
// terminal under the vars that map it back, and none held: the attach ends
// with its terminal (decisions.md D180).
func TestShowOpensAnOSWindowWithATabPerTabAndAWindowPerPanel(t *testing.T) {
	v, rec := viewOn(t, hiddenOnly(t))
	opened, err := v.Show(t.Context(), demo, true)
	if err != nil || !opened {
		t.Fatalf("Show = %v, %v; want a window opened", opened, err)
	}
	vars := func(panel string) []string {
		return []string{"--var", "REVIER_VIEW_PANEL=" + panel, "--var", "REVIER_VIEW_INSTANCE=core1/0a1b2c3d4e5f6a7b", "env", "HC_SOCKET=/s.sock", "/bin/hc", "attach", panel}
	}
	want := [][]string{
		slices.Concat([]string{"--type=os-window", "--os-window-name", "session:demo", "--os-window-title", "session:demo"}, vars("t1")),
		slices.Concat([]string{"--type=tab", "--location=last", "--match", "window_id:10"}, vars("t2")),
		slices.Concat([]string{"--type=window", "--match", "window_id:11"}, vars("t3")),
	}
	got := launches(rec)
	if len(got) != len(want) {
		t.Fatalf("launches = %q, want %q", got, want)
	}
	for i := range want {
		if !slices.Equal(got[i], want[i]) {
			t.Errorf("launch %d = %q, want %q", i, got[i], want[i])
		}
		if slices.Contains(got[i], "--hold") {
			t.Errorf("launch %d holds its window: %q", i, got[i])
		}
	}
}

// Without open, an instance no window shows opens none: a script's tab opens
// no window (decisions.md D118).
func TestShowWithoutOpenOpensNoWindow(t *testing.T) {
	v, rec := viewOn(t, hiddenOnly(t))
	if opened, err := v.Show(t.Context(), demo, false); err != nil || opened {
		t.Fatalf("Show = %v, %v; want nothing opened", opened, err)
	}
	if got := launches(rec); len(got) != 0 {
		t.Errorf("launches = %q, want none", got)
	}
}

// A window that shows the instance gets only what it lacks: a tab of no
// shown panel opens last, beside the OS window's current window, and a panel
// of a shown tab splits into that tab. The kitty has the keyboard, so it is
// kept where it is.
func TestShowAddsOnlyWhatTheWindowLacks(t *testing.T) {
	v, rec := viewOn(t, "testdata/ls-hc-view.json")
	more := demo
	more.Panels = append(slices.Clone(demo.Panels), revier.Panel{ID: "t9", Tab: "b"}, revier.Panel{ID: "t4", Tab: "c"}, revier.Panel{ID: "t5", Tab: "c"})
	opened, err := v.Show(t.Context(), more, true)
	if err != nil || opened {
		t.Fatalf("Show = %v, %v; want the window it has", opened, err)
	}
	got := launches(rec)
	heads := [][]string{
		{"--type=window", "--match", "window_id:3", "--keep-focus"},
		{"--type=tab", "--location=last", "--match", "window_id:3", "--keep-focus"},
		{"--type=window", "--match", "window_id:11", "--keep-focus"},
	}
	if len(got) != len(heads) {
		t.Fatalf("launches = %q, want t9 split into the agent tab, then a tab of t4 and t5", got)
	}
	for i, head := range heads {
		if !slices.Equal(got[i][:len(head)], head) {
			t.Errorf("launch %d = %q, want it to open with %q", i, got[i], head)
		}
	}
	if !slices.Contains(got[0], "REVIER_VIEW_PANEL=t9") || !slices.Contains(got[2], "REVIER_VIEW_PANEL=t5") {
		t.Errorf("launches = %q, want t9, t4 and t5", got)
	}
}

// Every call takes and returns hc's refs and addresses, read from the vars
// of the kitty windows: the focused OS window is the hc instance, the
// current window its terminal, and a terminal its window.
func TestTheViewMapsHcRefsAndKittyWindowsBothWays(t *testing.T) {
	v, rec := viewOn(t, "testdata/ls-hc-view.json")
	ctx := t.Context()

	if got, err := v.Focused(ctx); err != nil || got != demo.Ref {
		t.Errorf("Focused = %+v, %v; want %+v", got, err, demo.Ref)
	}
	if got, err := v.FocusedPanel(ctx, demo.Ref); err != nil || got != "t2" {
		t.Errorf("FocusedPanel = %s, %v; want t2, the agent tab's current window", got, err)
	}
	focuses := func() []string {
		var out []string
		for _, c := range rec.all() {
			if c.args[0] == "focus-window" {
				out = append(out, c.socket+" "+strings.Join(c.args[1:], " "))
			}
		}
		return out
	}
	if err := v.FocusPanel(ctx, demo.Ref, "t3"); err != nil {
		t.Fatalf("FocusPanel: %v", err)
	}
	if err := v.Focus(ctx, demo.Ref); err != nil {
		t.Fatalf("Focus: %v", err)
	}
	if got, want := focuses(), []string{workspace + " --match id:4", workspace + " --match id:3"}; !slices.Equal(got, want) {
		t.Errorf("focuses = %q, want %q", got, want)
	}

	other := revier.TargetRef{Host: "hc", ID: "core1/ffffffffffffffff"}
	if err := v.Focus(ctx, other); err == nil {
		t.Error("Focus of an instance no window shows passed")
	}
	if err := v.FocusPanel(ctx, demo.Ref, "t9"); err == nil {
		t.Error("FocusPanel of a terminal no window shows passed")
	}
}

// A key passes the kitty window id; the view maps it to the hc terminal that
// window attaches, in the instance that holds it. kitty's own window beside
// them attaches nothing, and is outside every workspace (decisions.md D183).
func TestFindPanelMapsAKittyWindowToTheTerminalItAttaches(t *testing.T) {
	v, _ := viewOn(t, "testdata/ls-hc-view.json")
	ctx := t.Context()
	instances := []revier.Instance{{Ref: revier.TargetRef{Host: "hc", ID: "core1/1111111111111111"}}, demo}

	ref, panel, err := v.FindPanel(ctx, instances, "4")
	if err != nil || ref != demo.Ref || panel != "t3" {
		t.Errorf("FindPanel(4) = %+v, %s, %v; want %+v, t3", ref, panel, err, demo.Ref)
	}
	if ref, _, err := v.FindPanel(ctx, instances, "6"); err != nil || !ref.IsZero() {
		t.Errorf("FindPanel(6) = %+v, %v; want none: kitty's own tab", ref, err)
	}
	if ref, _, err := v.FindPanel(ctx, instances[:1], "4"); err != nil || !ref.IsZero() {
		t.Errorf("FindPanel of a terminal hc no longer lists = %+v, %v; want none", ref, err)
	}
	v.Kitty().SetOwnSocket(func() string { return "" })
	if _, _, err := v.FindPanel(ctx, instances, "4"); err == nil {
		t.Error("FindPanel from no kitty passed")
	}
}

// A tab outside a workspace is kitty's own, beside the key's window, in the
// kitty the key was pressed in: a shell unheld, an agent held as the kitty
// runtime holds a panel's command.
func TestOpenBesideIsAKittyTabBesideTheKeysWindow(t *testing.T) {
	v, rec := viewOn(t, "testdata/ls-hc-view.json")
	ctx := t.Context()
	if err := v.OpenBeside(ctx, "6", revier.PanelSpec{Kind: revier.PanelShell, Dir: "/home/user/dev/demo"}); err != nil {
		t.Fatalf("OpenBeside: %v", err)
	}
	if err := v.OpenBeside(ctx, "6", revier.PanelSpec{Kind: revier.PanelAgent, Command: []string{"claude"}}); err != nil {
		t.Fatalf("OpenBeside agent: %v", err)
	}
	got := launches(rec)
	want := [][]string{
		{"--type=tab", "--match", "window_id:6", "--cwd", "/home/user/dev/demo"},
		{"--type=tab", "--match", "window_id:6", "--hold", "claude"},
	}
	if len(got) != 2 || !slices.Equal(got[0], want[0]) || !slices.Equal(got[1], want[1]) {
		t.Errorf("launches = %q, want %q", got, want)
	}
}
