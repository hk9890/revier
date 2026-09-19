//go:build integration

// Layer L3: the kitten @ ls parser and the remote-control sequences, against
// recorded output and a recording runner. No kitty, no display. The fixture is
// the shape kitty 0.48 emits, reduced to the fields the host reads and
// sanitised: real output carries the user's window titles and paths.
package kitty_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/hk9890/revier/internal/adapter/kitty"
	"github.com/hk9890/revier/pkg/revier"
)

func fixture(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/ls.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return b
}

// call is one recorded kitten invocation.
type call struct {
	socket string
	args   []string
}

// recorder collects calls; the host queries sockets concurrently.
type recorder struct {
	mu    sync.Mutex
	calls []call
}

func (r *recorder) add(socket string, args []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, call{socket, args})
}

func (r *recorder) all() []call {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]call(nil), r.calls...)
}

// parents is the process tree behind every recorded listing here, child to
// parent. The pids are made up, so the host must not read them from this
// machine's /proc, where they belong to other processes.
var parents = map[int]int{
	4002: 4001, 4003: 4002,
	5002: 5001, 5003: 5002, 5021: 5020,
	6002: 6001, 6003: 6002, 6004: 6003,
	7002: 7001, 7003: 1,
}

// newHost is a Host that reads the process tree from parents.
func newHost() *kitty.Host {
	h := &kitty.Host{}
	h.SetParents(func(pid int) (int, bool) {
		ppid, ok := parents[pid]
		return ppid, ok
	})
	return h
}

// host returns a Host whose kitten answers from the fixture on every socket
// given, recording each call.
func host(t *testing.T, sockets ...string) (*kitty.Host, *recorder) {
	t.Helper()
	h := newHost()
	rec := &recorder{}
	raw := fixture(t)
	h.SetSockets(func() []string { return sockets })
	h.SetRunner(func(_ context.Context, socket, _ string, args ...string) ([]byte, error) {
		rec.add(socket, args)
		if args[0] == "ls" {
			return raw, nil
		}
		return []byte("9\n"), nil
	})
	return h, rec
}

func TestInstancesParseOSWindowsWithPanels(t *testing.T) {
	h, _ := host(t, "unix:@kitty-4000")
	got, err := h.Instances(context.Background())
	if err != nil {
		t.Fatalf("Instances: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d instances, want 2 OS windows", len(got))
	}

	// The first window carries kitty's default name, which identifies
	// nothing; the adapter reports that as no title, and the core learns one
	// from the window manager (decisions.md D22). The second was named by
	// revier and identifies itself.
	legacy, ws := got[0], got[1]
	if legacy.Title != "" || legacy.Ref.Title != "" {
		t.Errorf("unnamed window title = %q, ref title = %q, want both empty",
			legacy.Title, legacy.Ref.Title)
	}
	if ws.Title != "session:revier" {
		t.Errorf("title = %q: the identity is wm_name", ws.Title)
	}
	if legacy.PID != 4000 {
		t.Errorf("unnamed window pid = %d, want 4000: it is how the core pairs it", legacy.PID)
	}
	if ws.Ref.Host != "kitty" || ws.Ref.ID != "@kitty-4000/2" {
		t.Errorf("ref = %+v, want kitty/@kitty-4000/2: the socket rides in the id", ws.Ref)
	}
	if ws.PID != 4000 {
		t.Errorf("pid = %d, want 4000 from the socket name", ws.PID)
	}
	if len(ws.Panels) != 3 {
		t.Fatalf("got %d panels, want 3 across both tabs", len(ws.Panels))
	}
}

// The identity a project matches on must survive the decode, or a workspace
// revier opened is never found again.
func TestDecodedInstancesMatchARealization(t *testing.T) {
	h, _ := host(t, "unix:@kitty-4000")
	got, _ := h.Instances(context.Background())
	m, err := (revier.Match{Title: "^session:revier$"}).Compile()
	if err != nil {
		t.Fatal(err)
	}
	var hits []string
	for _, inst := range got {
		if m.Matches(inst) {
			hits = append(hits, inst.Ref.ID)
		}
	}
	if len(hits) != 1 || hits[0] != "@kitty-4000/2" {
		t.Errorf("matched %v, want only the revier workspace", hits)
	}
}

// The Claude probe reads Vars, Title, and Command; each must arrive.
func TestPanelsCarryWhatTheProbeReads(t *testing.T) {
	h, _ := host(t, "unix:@kitty-4000")
	got, _ := h.Instances(context.Background())
	panels := got[1].Panels

	agent := panels[0]
	if agent.Vars["CS_TAB"] != "1" {
		t.Errorf("CS_TAB did not come through user_vars: %+v", agent.Vars)
	}
	if agent.Kind == revier.PanelShell {
		t.Errorf("kind = shell: claude is in the foreground group")
	}
	if len(agent.Command) == 0 || agent.Command[0] != "claude" {
		t.Errorf("command = %v, want the agent's own argv, not the run-shell wrapper or the git it runs", agent.Command)
	}
	if agent.PID != 5002 {
		t.Errorf("pid = %d, want the agent's 5002", agent.PID)
	}
	if agent.Title != "⠧ Investigating setup" {
		t.Errorf("title = %q", agent.Title)
	}

	if shell := panels[1]; shell.Kind != revier.PanelShell || shell.ID != "3" {
		t.Errorf("shell panel = %+v", shell)
	}
	if tool := panels[2]; tool.Kind != revier.PanelTool || tool.Command[0] != "taskmgr-ui" {
		t.Errorf("tool panel = %+v: the program inside the wrapper is the command", tool)
	}
}

// A harness this adapter was not written with - one a probe declared in
// config exists for - stays the panel's command while it runs a tool, behind
// the run-shell wrapper and a login shell, so its probe keeps claiming it.
// The adapter names no harness: which program is an agent is the probe's.
func TestADeclaredHarnessRunningAToolIsStillTheCommand(t *testing.T) {
	h := newHost()
	h.SetSockets(func() []string { return []string{"unix:@kitty-4000"} })
	h.SetRunner(func(context.Context, string, string, ...string) ([]byte, error) {
		return json.Marshal([]map[string]any{{"id": 1, "wm_name": "session:demo",
			"tabs": []map[string]any{{"windows": []map[string]any{{"id": 1, "pid": 6001,
				"foreground_processes": []map[string]any{
					{"pid": 6001, "cmdline": []string{"/opt/kitty/bin/kitten", "run-shell", "--shell=/usr/bin/zsh", "/usr/bin/sh", "-lc", "myagent"}},
					{"pid": 6002, "cmdline": []string{"/usr/bin/sh", "-lc", "myagent"}},
					{"pid": 6003, "cmdline": []string{"/usr/local/bin/myagent", "--resume"}},
					{"pid": 6004, "cmdline": []string{"rg", "TODO"}},
				}}}}}}})
	})
	got, err := h.Instances(context.Background())
	if err != nil {
		t.Fatalf("Instances: %v", err)
	}
	panel := got[0].Panels[0]
	if !panel.Runs("myagent") || panel.PID != 6003 {
		t.Errorf("panel command = %v, pid %d: want myagent's, 6003, not the rg it runs", panel.Command, panel.PID)
	}
	if panel.Kind == revier.PanelShell {
		t.Error("kind = shell: a shell overrules every probe")
	}
}

// The order kitty listed on a real desktop: a wl-copy Claude Code left holding
// the clipboard, detached from the window's tree, sorted before the agent by
// its lower pid. The agent is still the panel's command; taking wl-copy hid it
// from every probe, so it was neither shown nor saved.
func TestAProcessDetachedFromTheWindowIsNotTheCommand(t *testing.T) {
	windows := func(fg []map[string]any) ([]byte, error) {
		return json.Marshal([]map[string]any{{"id": 1, "wm_name": "session:demo",
			"tabs": []map[string]any{{"windows": []map[string]any{{"id": 38, "pid": 7001, "foreground_processes": fg}}}}}})
	}
	wlCopy := map[string]any{"pid": 7003, "cmdline": []string{"wl-copy", "--type", "text/plain"}}
	wrapper := map[string]any{"pid": 7001, "cmdline": []string{"/opt/kitty/bin/kitten", "run-shell", "--shell=/usr/bin/zsh"}}
	agent := map[string]any{"pid": 7002, "cmdline": []string{"claude"}}
	shell := map[string]any{"pid": 7001, "cmdline": []string{"/usr/bin/zsh", "-i"}}

	cases := []struct {
		name string
		fg   []map[string]any
		kind revier.PanelKind
		cmd  string
		pid  int
	}{
		{"before the agent", []map[string]any{wlCopy, wrapper, agent}, revier.PanelTool, "claude", 7002},
		{"after the agent", []map[string]any{wrapper, agent, wlCopy}, revier.PanelTool, "claude", 7002},
		{"in a shell", []map[string]any{wlCopy, shell}, revier.PanelShell, "/usr/bin/zsh", 7001},
	}
	for _, tc := range cases {
		h := newHost()
		h.SetSockets(func() []string { return []string{"unix:@kitty-4000"} })
		h.SetRunner(func(context.Context, string, string, ...string) ([]byte, error) { return windows(tc.fg) })
		got, err := h.Instances(context.Background())
		if err != nil {
			t.Fatalf("%s: Instances: %v", tc.name, err)
		}
		panel := got[0].Panels[0]
		if panel.Kind != tc.kind || len(panel.Command) == 0 || panel.Command[0] != tc.cmd || panel.PID != tc.pid {
			t.Errorf("%s: panel = %s %v pid %d, want %s %s pid %d", tc.name, panel.Kind, panel.Command, panel.PID, tc.kind, tc.cmd, tc.pid)
		}
	}
}

// A process whose parents cannot be read - gone between the listing and the
// read - keeps kitty's order, after every process placed in the window's tree.
func TestAProcessWithUnreadableParentsComesAfterThePlacedOnes(t *testing.T) {
	h := newHost()
	h.SetSockets(func() []string { return []string{"unix:@kitty-4000"} })
	h.SetRunner(func(context.Context, string, string, ...string) ([]byte, error) {
		return json.Marshal([]map[string]any{{"id": 1, "wm_name": "session:demo",
			"tabs": []map[string]any{{"windows": []map[string]any{{"id": 1, "pid": 7001,
				"foreground_processes": []map[string]any{
					{"pid": 9999, "cmdline": []string{"gone"}},
					{"pid": 7002, "cmdline": []string{"claude"}},
				}}}}}}})
	})
	got, err := h.Instances(context.Background())
	if err != nil {
		t.Fatalf("Instances: %v", err)
	}
	if panel := got[0].Panels[0]; !panel.Runs("claude") {
		t.Errorf("panel command = %v, want claude, which is placed in the tree", panel.Command)
	}
}

// A panel whose group holds only the wrapper and shells has no program of its
// own running: it is a shell, and a probe's marker left in it is stale.
func TestAPanelOfOnlyShellsIsAShell(t *testing.T) {
	h := newHost()
	h.SetSockets(func() []string { return []string{"unix:@kitty-4000"} })
	h.SetRunner(func(context.Context, string, string, ...string) ([]byte, error) {
		return json.Marshal([]map[string]any{{"id": 1, "wm_name": "session:demo",
			"tabs": []map[string]any{{"windows": []map[string]any{{"id": 1, "pid": 6001,
				"foreground_processes": []map[string]any{
					{"pid": 6001, "cmdline": []string{"/opt/kitty/bin/kitten", "run-shell", "--shell=/usr/bin/zsh"}},
					{"pid": 6002, "cmdline": []string{"/usr/bin/zsh", "-i"}},
				}}}}}}})
	})
	got, err := h.Instances(context.Background())
	if err != nil {
		t.Fatalf("Instances: %v", err)
	}
	if panel := got[0].Panels[0]; panel.Kind != revier.PanelShell {
		t.Errorf("panel = %+v, want a shell", panel)
	}
}

// One ls per kitty process, whatever the project count. The socket list is
// the only thing that scales it.
func TestInstancesIssueOneCallPerSocket(t *testing.T) {
	h, rec := host(t, "unix:@kitty-4000", "unix:@kitty-4001", "unix:@kitty-4002")
	got, err := h.Instances(context.Background())
	if err != nil {
		t.Fatalf("Instances: %v", err)
	}
	calls := rec.all()
	if len(calls) != 3 {
		t.Fatalf("kitten invoked %d times, want 3: one ls per socket", len(calls))
	}
	for _, c := range calls {
		if len(c.args) != 1 || c.args[0] != "ls" {
			t.Errorf("unexpected invocation %v", c.args)
		}
	}
	if len(got) != 6 {
		t.Errorf("got %d instances, want 2 per socket", len(got))
	}
}

// A socket that does not answer is a kitty on its way out; the others still
// report.
func TestInstancesSkipASocketThatDoesNotAnswer(t *testing.T) {
	h, _ := host(t, "unix:@kitty-4000", "unix:@kitty-dead")
	raw := fixture(t)
	h.SetRunner(func(_ context.Context, socket, _ string, args ...string) ([]byte, error) {
		if socket == "unix:@kitty-dead" {
			return nil, os.ErrNotExist
		}
		return raw, nil
	})
	got, err := h.Instances(context.Background())
	if err != nil {
		t.Fatalf("Instances: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("got %d instances, want the 2 from the live socket", len(got))
	}
}

func TestFocusedReportsTheOSWindowWithOSFocus(t *testing.T) {
	h, _ := host(t, "unix:@kitty-4000")
	ref, err := h.Focused(context.Background())
	if err != nil {
		t.Fatalf("Focused: %v", err)
	}
	if ref.ID != "@kitty-4000/2" || ref.Title != "session:revier" {
		t.Errorf("ref = %+v", ref)
	}
}

// Focus lands on the active window of the active tab, so the user comes back
// to where they left the workspace.
func TestFocusSelectsTheActiveWindow(t *testing.T) {
	h, rec := host(t, "unix:@kitty-4000")
	if err := h.Focus(context.Background(), revier.TargetRef{Host: "kitty", ID: "@kitty-4000/2"}); err != nil {
		t.Fatalf("Focus: %v", err)
	}
	calls := rec.all()
	last := calls[len(calls)-1]
	want := "focus-window --match id:4"
	if got := strings.Join(last.args, " "); got != want || last.socket != "unix:@kitty-4000" {
		t.Errorf("last call = %s %q, want %q on unix:@kitty-4000", last.socket, got, want)
	}
}

// An agent tab is a group of panels: the first opens the tab, last among the
// OS window's tabs so a save records its agents in the same order, and every
// later one splits into it by the id the tab's launch reported. Each panel
// starts in its own directory and gets its title. No launch keeps kitty's
// focus: the core focuses the panel it wants after.
func TestOpenTabOpensAPanelGroupAsOneTab(t *testing.T) {
	h, rec := host(t, "unix:@kitty-4000")
	panel, err := h.OpenTab(context.Background(), revier.TargetRef{Host: "kitty", ID: "@kitty-4000/2"}, revier.Realization{
		Dir: "/home/user/dev/demo",
		Panels: []revier.PanelSpec{
			{Kind: revier.PanelAgent, Title: "Claude Code", Command: []string{"claude", "--resume", "b"}, Dir: "/home/user/dev/demo/wt"},
			{Kind: revier.PanelShell, Title: "shell", Dir: "/home/user/dev/demo/wt"},
		},
	}, nil)
	if err != nil {
		t.Fatalf("OpenTab: %v", err)
	}
	if panel != "9" {
		t.Errorf("panel = %s, want the id the tab's launch reported", panel)
	}
	var launches, titles []string
	for _, c := range rec.all() {
		switch c.args[0] {
		case "launch":
			launches = append(launches, strings.Join(c.args, " "))
		case "set-window-title":
			titles = append(titles, strings.Join(c.args, " "))
		}
	}
	want := []string{
		"launch --type=tab --location=last --match window_id:4 --hold --cwd /home/user/dev/demo/wt claude --resume b",
		"launch --type=window --match window_id:9 --hold --cwd /home/user/dev/demo/wt",
	}
	if !slices.Equal(launches, want) {
		t.Errorf("launches =\n%s\nwant\n%s", strings.Join(launches, "\n"), strings.Join(want, "\n"))
	}
	if len(titles) != 2 || !strings.HasSuffix(titles[0], "Claude Code") || !strings.HasSuffix(titles[1], "shell") {
		t.Errorf("titles = %q, want each panel's own", titles)
	}
}

// An OS window that is gone has nothing to open a tab in, and nothing is
// launched into another one.
func TestOpenTabRefusesAnOSWindowThatIsGone(t *testing.T) {
	h, rec := host(t, "unix:@kitty-4000")
	_, err := h.OpenTab(context.Background(), revier.TargetRef{Host: "kitty", ID: "@kitty-4000/999"},
		revier.Realization{Panels: []revier.PanelSpec{{Kind: revier.PanelAgent, Command: []string{"claude"}}}}, nil)
	if err == nil {
		t.Fatal("OpenTab into a missing OS window: want an error")
	}
	for _, c := range rec.all() {
		if c.args[0] == "launch" {
			t.Errorf("launched %q into a kitty without the OS window", c.args)
		}
	}
}

// Open is a launch sequence into the running kitty: the first panel opens the
// OS window and carries its identity, later panels split into it. Never a
// session file, which a running kitty answers with a second process.
func TestOpenBuildsTheLayoutWithLaunchSequences(t *testing.T) {
	h := newHost()
	var calls []call
	h.SetSockets(func() []string { return []string{"unix:@kitty-4000"} })
	h.SetRunner(func(_ context.Context, socket, _ string, args ...string) ([]byte, error) {
		calls = append(calls, call{socket, args})
		switch args[0] {
		case "launch":
			if strings.Contains(strings.Join(args, " "), "--type=os-window") {
				return []byte("7\n"), nil
			}
			return []byte("8\n"), nil
		case "set-window-title":
			return nil, nil
		case "ls":
			return json.Marshal([]map[string]any{{
				"id": 3, "wm_name": "session:demo", "wm_class": "kitty",
				"tabs": []map[string]any{{"is_active": true, "windows": []map[string]any{{"id": 7, "is_active": true}, {"id": 8}}}},
			}})
		}
		t.Fatalf("unexpected call %v", args)
		return nil, nil
	})

	ref, err := h.Open(context.Background(), revier.Realization{
		Name: "session:demo", Dir: "/home/user/dev/demo",
		Match: revier.Match{Title: "^session:demo$"},
		Panels: []revier.PanelSpec{
			{Kind: revier.PanelAgent, Title: "agent", Command: []string{"claude"}, Dir: "/home/user/dev/demo"},
			{Kind: revier.PanelShell, Title: "shell", Dir: "/home/user/dev/demo"},
		},
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if ref.ID != "@kitty-4000/3" || ref.Title != "session:demo" {
		t.Errorf("ref = %+v, want the OS window around window 7", ref)
	}

	var seq []string
	for _, c := range calls {
		seq = append(seq, strings.Join(c.args, " "))
	}
	if len(seq) != 6 || seq[0] != "ls" {
		t.Fatalf("got %d calls, want ls, launch, title, launch, title, ls:\n%s", len(seq), strings.Join(seq, "\n"))
	}
	seq = seq[1:]
	for _, want := range []string{
		"launch --type=os-window",
		"--os-window-name session:demo", "--os-window-title session:demo",
		"--os-window-class kitty", "--cwd /home/user/dev/demo", " claude",
	} {
		if !strings.Contains(seq[0], want) {
			t.Errorf("first launch %q lacks %q", seq[0], want)
		}
	}
	// Titles are set afterwards and temporarily: a title pinned at launch
	// would hide the agent's live title from the probe.
	if seq[1] != "set-window-title --temporary --match id:7 agent" {
		t.Errorf("first title call = %q", seq[1])
	}
	for _, want := range []string{"launch --type=window", "--match window_id:7", "--cwd /home/user/dev/demo"} {
		if !strings.Contains(seq[2], want) {
			t.Errorf("second launch %q lacks %q", seq[2], want)
		}
	}
	if seq[3] != "set-window-title --temporary --match id:8 shell" {
		t.Errorf("second title call = %q", seq[3])
	}
	for _, c := range calls {
		if strings.Contains(strings.Join(c.args, " "), "--title ") || strings.Contains(strings.Join(c.args, " "), "--tab-title") {
			t.Errorf("a launch pinned a title: %v", c.args)
		}
	}
	for _, c := range calls {
		for _, a := range c.args {
			if strings.Contains(a, "--session") {
				t.Fatalf("Open used a session file: %v", c.args)
			}
		}
	}

	// Open produces what Match finds: the listing kitty gives back after the
	// launch decodes to an instance the realization's match selects.
	instances, err := h.Instances(context.Background())
	if err != nil {
		t.Fatalf("Instances: %v", err)
	}
	m, err := revier.Match{Title: "^session:demo$"}.Compile()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, inst := range instances {
		if m.Matches(inst) && inst.Ref == ref {
			found = true
		}
	}
	if !found {
		t.Errorf("Match does not find what Open produced: %+v", instances)
	}
}

func TestOpenWithoutPanelsLaunchesTheArgv(t *testing.T) {
	h := newHost()
	var calls []call
	h.SetSockets(func() []string { return []string{"unix:@kitty-4000"} })
	h.SetRunner(func(_ context.Context, socket, _ string, args ...string) ([]byte, error) {
		calls = append(calls, call{socket, args})
		if args[0] == "launch" {
			return []byte("7\n"), nil
		}
		return json.Marshal([]map[string]any{{"id": 1, "wm_name": "tickets:demo",
			"tabs": []map[string]any{{"windows": []map[string]any{{"id": 7}}}}}})
	})
	if _, err := h.Open(context.Background(), revier.Realization{
		Name: "tickets:demo", Launch: []string{"taskmgr-ui", "--all"}, Match: revier.Match{Title: "^tickets:demo$"},
	}); err != nil {
		t.Fatalf("Open: %v", err)
	}
	launched := false
	for _, c := range calls {
		if c.args[0] != "launch" {
			continue
		}
		launched = true
		if got := strings.Join(c.args, " "); !strings.HasSuffix(got, "taskmgr-ui --all") {
			t.Errorf("launch %q should end with the argv", got)
		}
	}
	if !launched {
		t.Error("nothing was launched")
	}
}

// KITTY_LISTEN_ON leads the socket list and outlives its kitty in every
// process started from it. Open launches into the first socket that answers,
// not into the first one listed, or every launch fails on the dead one.
func TestOpenSkipsASocketThatDoesNotAnswer(t *testing.T) {
	h := newHost()
	var launches []call
	h.SetSockets(func() []string { return []string{"unix:@kitty-stale", "unix:@kitty-4000"} })
	h.SetStarter(func(context.Context, ...string) error {
		t.Fatal("started a kitty while one answers")
		return nil
	})
	h.SetRunner(func(_ context.Context, socket, _ string, args ...string) ([]byte, error) {
		if socket == "unix:@kitty-stale" {
			return nil, os.ErrNotExist
		}
		if args[0] == "launch" {
			launches = append(launches, call{socket, args})
			return []byte("7\n"), nil
		}
		return json.Marshal([]map[string]any{{"id": 1, "wm_name": "tickets:demo",
			"tabs": []map[string]any{{"windows": []map[string]any{{"id": 7}}}}}})
	})
	ref, err := h.Open(context.Background(), revier.Realization{
		Name: "tickets:demo", Launch: []string{"taskmgr-ui"}, Match: revier.Match{Title: "^tickets:demo$"},
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if len(launches) != 1 || launches[0].socket != "unix:@kitty-4000" {
		t.Errorf("launches = %+v, want one, into the live unix:@kitty-4000", launches)
	}
	if ref.ID != "@kitty-4000/1" {
		t.Errorf("ref = %+v, want the OS window on the live socket", ref)
	}
}

// With no kitty running, Open starts one on a socket discovery recognises and
// waits for it to answer.
func TestOpenStartsKittyWhenNoneRuns(t *testing.T) {
	h := newHost()
	var started []string
	running := false
	h.SetStarter(func(_ context.Context, args ...string) error {
		started = args
		running = true
		return nil
	})
	h.SetSockets(func() []string {
		if running {
			return []string{"unix:@kitty-5000"}
		}
		return nil
	})
	var calls []call
	h.SetRunner(func(_ context.Context, socket, _ string, args ...string) ([]byte, error) {
		calls = append(calls, call{socket, args})
		switch args[0] {
		case "ls":
			return json.Marshal([]map[string]any{{"id": 1, "wm_name": "session:demo",
				"tabs": []map[string]any{{"is_active": true, "windows": []map[string]any{{"id": 1, "is_active": true}}}}}})
		case "launch":
			return []byte("2\n"), nil
		}
		return nil, nil
	})

	ref, err := h.Open(context.Background(), revier.Realization{
		Name: "session:demo", Dir: "/d", Match: revier.Match{Title: "^session:demo$"},
		Panels: []revier.PanelSpec{{Title: "agent", Command: []string{"claude"}, Dir: "/d"}, {Title: "shell", Dir: "/d"}},
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if ref.ID != "@kitty-5000/1" {
		t.Errorf("ref = %+v", ref)
	}
	joined := strings.Join(started, " ")
	for _, want := range []string{"--detach", "--listen-on unix:@kitty-{kitty_pid}", "--name session:demo", "--title session:demo", "--directory /d", "claude"} {
		if !strings.Contains(joined, want) {
			t.Errorf("kitty started with %q, lacks %q", joined, want)
		}
	}
	var split bool
	for _, c := range calls {
		if c.args[0] == "launch" && strings.Contains(strings.Join(c.args, " "), "--type=window") {
			split = true
		}
	}
	if !split {
		t.Error("the second panel was never launched into the new kitty")
	}
}

func TestOpenRequiresAName(t *testing.T) {
	h, _ := host(t, "unix:@kitty-4000")
	if _, err := h.Open(context.Background(), revier.Realization{Launch: []string{"x"}}); err == nil {
		t.Fatal("want an error: without a name the OS window has no identity to match")
	}
}

// A prompt goes to one window of the kitty process the instance lives in, and
// through stdin: kitty reads an argument for escapes, and a backslash in the
// prompt must arrive as a backslash.
func TestSendTextGoesThroughStdinToTheWindow(t *testing.T) {
	h := newHost()
	var got []call
	var stdin []string
	h.SetSockets(func() []string { return []string{"unix:@kitty-4000"} })
	h.SetRunner(func(_ context.Context, socket, in string, args ...string) ([]byte, error) {
		got = append(got, call{socket, args})
		stdin = append(stdin, in)
		return nil, nil
	})
	var w revier.PanelWriter = h
	ref := revier.TargetRef{Host: "kitty", ID: "@kitty-4001/2"}
	if err := w.SendText(context.Background(), ref, "7", `fix a\b`); err != nil {
		t.Fatalf("SendText: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("kitten invoked %d times, want 1: %+v", len(got), got)
	}
	want := "send-text --match id:7 --stdin"
	if args := strings.Join(got[0].args, " "); args != want || got[0].socket != "unix:@kitty-4001" {
		t.Errorf("call = %s %q, want %q on the instance's own socket unix:@kitty-4001", got[0].socket, args, want)
	}
	if stdin[0] != `fix a\b` {
		t.Errorf("stdin = %q, want the text as it is", stdin[0])
	}
}

// A tab opens after the OS window's tabs, carries the vars the core gives it
// as user vars, and runs the launch argv in the directory given.
func TestOpenTabLaunchesATabWithItsVars(t *testing.T) {
	h, rec := host(t, "unix:@kitty-4000")
	var opener revier.PanelOpener = h
	panel, err := opener.OpenTab(context.Background(), revier.TargetRef{Host: "kitty", ID: "@kitty-4000/2"},
		revier.Realization{Launch: []string{"taskmgr-ui"}, Dir: "/p"}, map[string]string{"revier_target": "tickets"})
	if err != nil {
		t.Fatalf("OpenTab: %v", err)
	}
	if panel != "9" {
		t.Errorf("panel = %s, want the id launch reported", panel)
	}
	calls := rec.all()
	last := calls[len(calls)-1]
	want := "launch --type=tab --location=last --match window_id:4 --hold --var revier_target=tickets --cwd /p taskmgr-ui"
	if got := strings.Join(last.args, " "); got != want || last.socket != "unix:@kitty-4000" {
		t.Errorf("last call = %s %q, want %q on unix:@kitty-4000", last.socket, got, want)
	}
}

func TestFocusPanelFocusesThatWindow(t *testing.T) {
	h, rec := host(t, "unix:@kitty-4000")
	if err := h.FocusPanel(context.Background(), revier.TargetRef{Host: "kitty", ID: "@kitty-4001/2"}, "7"); err != nil {
		t.Fatalf("FocusPanel: %v", err)
	}
	calls := rec.all()
	want := "focus-window --match id:7"
	if len(calls) != 1 || strings.Join(calls[0].args, " ") != want || calls[0].socket != "unix:@kitty-4001" {
		t.Errorf("calls = %+v, want one %q on the instance's own socket", calls, want)
	}
}

func TestFocusedPanelIsTheActiveWindow(t *testing.T) {
	h, _ := host(t, "unix:@kitty-4000")
	panel, err := h.FocusedPanel(context.Background(), revier.TargetRef{Host: "kitty", ID: "@kitty-4000/2"})
	if err != nil {
		t.Fatalf("FocusedPanel: %v", err)
	}
	if panel != "4" {
		t.Errorf("panel = %s, want 4, the window Focus selects", panel)
	}
}

// A kitty window id is one kitty process's: both sockets here hold window 3.
// The id means the window of the kitty the command was started from, which
// KITTY_LISTEN_ON names; with no such kitty answering, the id must be in one
// kitty only.
func TestFindPanelTakesTheWindowOfTheKittyTheCommandRunsIn(t *testing.T) {
	h, _ := host(t, "unix:@kitty-4000", "unix:@kitty-4001")

	h.SetOwnSocket(func() string { return "unix:@kitty-4001" })
	ref, err := h.FindPanel(listed(t, h), "3")
	if err != nil || ref.ID != "@kitty-4001/2" {
		t.Errorf("from kitty 4001: ref = %+v, %v, want @kitty-4001/2", ref, err)
	}
	if ref, err := h.FindPanel(listed(t, h), "99"); err != nil || !ref.IsZero() {
		t.Errorf("unknown window: ref = %+v, %v, want none", ref, err)
	}

	for _, own := range []string{"", "unix:@kitty-9999"} {
		h.SetOwnSocket(func() string { return own })
		if _, err := h.FindPanel(listed(t, h), "3"); err == nil || !strings.Contains(err.Error(), "2 kitty processes") {
			t.Errorf("own socket %q: err = %v, want the id refused as held by two kitties", own, err)
		}
	}

	one, _ := host(t, "unix:@kitty-4000")
	one.SetOwnSocket(func() string { return "" })
	if ref, err := one.FindPanel(listed(t, one), "3"); err != nil || ref.ID != "@kitty-4000/2" {
		t.Errorf("one kitty: ref = %+v, %v, want @kitty-4000/2", ref, err)
	}
}

// listed is the host's own listing, the one the core hands FindPanel.
func listed(t *testing.T, h *kitty.Host) []revier.Instance {
	t.Helper()
	instances, err := h.Instances(context.Background())
	if err != nil {
		t.Fatalf("Instances: %v", err)
	}
	return instances
}

// FindPanel reads the listing it is handed and lists nothing itself: the key
// press has already listed every kitty.
func TestFindPanelListsNothing(t *testing.T) {
	h, rec := host(t, "unix:@kitty-4000", "unix:@kitty-4001")
	h.SetOwnSocket(func() string { return "unix:@kitty-4001" })
	instances := listed(t, h)
	before := len(rec.all())
	if _, err := h.FindPanel(instances, "3"); err != nil {
		t.Fatalf("FindPanel: %v", err)
	}
	if calls := rec.all()[before:]; len(calls) != 0 {
		t.Errorf("FindPanel ran %+v, want no kitten call", calls)
	}
}

// A tab whose second launch fails closes the window its first launch opened:
// the core names that agent as not added, and it must not run on in a tab
// without its shell.
func TestOpenTabClosesWhatItOpenedWhenALaunchFails(t *testing.T) {
	h := newHost()
	rec := &recorder{}
	raw := fixture(t)
	h.SetSockets(func() []string { return []string{"unix:@kitty-4000"} })
	launches := 0
	h.SetRunner(func(_ context.Context, socket, _ string, args ...string) ([]byte, error) {
		rec.add(socket, args)
		switch args[0] {
		case "ls":
			return raw, nil
		case "launch":
			launches++
			if launches == 2 {
				return nil, errors.New("kitty went away")
			}
		}
		return []byte("9\n"), nil
	})
	_, err := h.OpenTab(context.Background(), revier.TargetRef{Host: "kitty", ID: "@kitty-4000/2"}, revier.Realization{
		Panels: []revier.PanelSpec{
			{Kind: revier.PanelAgent, Command: []string{"claude", "--resume", "b"}},
			{Kind: revier.PanelShell},
		},
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "kitty went away") {
		t.Fatalf("err = %v, want the launch's error", err)
	}
	calls := rec.all()
	if got := strings.Join(calls[len(calls)-1].args, " "); got != "close-window --match id:9" {
		t.Errorf("last call = %q, want the agent's window closed", got)
	}
}

// An OS window closes as every kitty window in it, across its tabs, in one
// call on its own process's socket.
func TestCloseClosesEveryWindowOfTheOSWindow(t *testing.T) {
	h, rec := host(t, "unix:@kitty-4000")
	if err := h.Close(context.Background(), revier.TargetRef{Host: "kitty", ID: "@kitty-4000/2"}); err != nil {
		t.Fatalf("Close: %v", err)
	}
	calls := rec.all()
	last := calls[len(calls)-1]
	if got := strings.Join(last.args, " "); last.socket != "unix:@kitty-4000" || got != "close-window --match id:2 or id:3 or id:4" {
		t.Errorf("last call = %q on %s, want windows 2, 3 and 4 closed", got, last.socket)
	}
	if err := h.Close(context.Background(), revier.TargetRef{Host: "kitty", ID: "@kitty-4000/7"}); err == nil {
		t.Error("Close of an OS window ls does not list succeeded")
	}
}

// A panel closes as its one kitty window.
func TestClosePanelClosesOneWindow(t *testing.T) {
	h, rec := host(t, "unix:@kitty-4000")
	if err := h.ClosePanel(context.Background(), revier.TargetRef{Host: "kitty", ID: "@kitty-4000/2"}, "3"); err != nil {
		t.Fatalf("ClosePanel: %v", err)
	}
	calls := rec.all()
	if got := strings.Join(calls[len(calls)-1].args, " "); got != "close-window --match id:3" {
		t.Errorf("last call = %q, want window 3 closed", got)
	}
}

// Each panel carries the id of the kitty tab that holds it, so the core can
// tell a panel group's windows from the other tabs' windows.
func TestPanelsCarryTheirTab(t *testing.T) {
	h, _ := host(t, "unix:@kitty-4000")
	got, err := h.Instances(context.Background())
	if err != nil {
		t.Fatalf("Instances: %v", err)
	}
	var tabs []string
	for _, p := range got[1].Panels {
		tabs = append(tabs, p.ID.String()+"@"+p.Tab)
	}
	if want := []string{"2@2", "3@2", "4@3"}; !slices.Equal(tabs, want) {
		t.Errorf("panels = %v, want %v", tabs, want)
	}
}

// A tab closes as the kitty tab that holds the window, on its own process's
// socket.
func TestCloseTabClosesTheTabOfTheWindow(t *testing.T) {
	h, rec := host(t, "unix:@kitty-4000")
	if err := h.CloseTab(context.Background(), revier.TargetRef{Host: "kitty", ID: "@kitty-4000/2"}, "3"); err != nil {
		t.Fatalf("CloseTab: %v", err)
	}
	calls := rec.all()
	last := calls[len(calls)-1]
	if got := strings.Join(last.args, " "); last.socket != "unix:@kitty-4000" || got != "close-tab --match window_id:3" {
		t.Errorf("last call = %q on %s, want the tab of window 3 closed", got, last.socket)
	}
}

// Every foreground process is walked up to its window's root. A parent the
// walks share is read once per listing, not once per walk.
func TestInstancesReadsEachParentOncePerListing(t *testing.T) {
	h, _ := host(t, "unix:@kitty-4000")
	reads := map[int]int{}
	h.SetParents(func(pid int) (int, bool) {
		reads[pid]++
		ppid, ok := parents[pid]
		return ppid, ok
	})
	listed(t, h)
	if len(reads) == 0 {
		t.Fatal("no parent read: the fixture no longer exercises the walk")
	}
	for pid, n := range reads {
		if n > 1 {
			t.Errorf("parent of %d read %d times in one listing, want once", pid, n)
		}
	}
}
