//go:build integration

// Layer L3: the kitten @ ls parser and the remote-control sequences, against
// recorded output and a recording runner. No kitty, no display. The fixture is
// the shape kitty 0.48 emits, reduced to the fields the host reads and
// sanitised: real output carries the user's window titles and paths.
package kitty_test

import (
	"context"
	"encoding/json"
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

// host returns a Host whose kitten answers from the fixture on every socket
// given, recording each call.
func host(t *testing.T, sockets ...string) (*kitty.Host, *recorder) {
	t.Helper()
	h := &kitty.Host{}
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
	h := &kitty.Host{}
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

// A panel whose group holds only the wrapper and shells has no program of its
// own running: it is a shell, and a probe's marker left in it is stale.
func TestAPanelOfOnlyShellsIsAShell(t *testing.T) {
	h := &kitty.Host{}
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

// A panel a restore adds beside the layout asks for a tab: it opens as a tab
// of the same OS window, found by the first panel's window id, and in its own
// directory. The OS window is then left on its first tab, the workspace.
func TestOpenPutsATabPanelInATabOfTheSameOSWindow(t *testing.T) {
	h := &kitty.Host{}
	var seq []string
	h.SetSockets(func() []string { return []string{"unix:@kitty-4000"} })
	h.SetRunner(func(_ context.Context, _, _ string, args ...string) ([]byte, error) {
		seq = append(seq, strings.Join(args, " "))
		switch args[0] {
		case "launch":
			if strings.Contains(strings.Join(args, " "), "--type=os-window") {
				return []byte("7\n"), nil
			}
			return []byte("9\n"), nil
		case "ls":
			return json.Marshal([]map[string]any{{
				"id": 3, "wm_name": "session:demo",
				"tabs": []map[string]any{
					{"windows": []map[string]any{{"id": 7}}},
					{"is_active": true, "windows": []map[string]any{{"id": 9, "is_active": true}}},
				},
			}})
		}
		return nil, nil
	})

	_, err := h.Open(context.Background(), revier.Realization{
		Name: "session:demo", Dir: "/home/user/dev/demo",
		Panels: []revier.PanelSpec{
			{Kind: revier.PanelAgent, Command: []string{"claude", "--resume", "a"}, Dir: "/home/user/dev/demo/wt"},
			{Kind: revier.PanelAgent, Command: []string{"claude", "--resume", "b"}, Dir: "/home/user/dev/demo/other", Tab: true},
		},
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	launches := []string{}
	for _, c := range seq {
		if strings.HasPrefix(c, "launch ") {
			launches = append(launches, c)
		}
	}
	if len(launches) != 2 {
		t.Fatalf("launches = %q, want two", launches)
	}
	if !strings.Contains(launches[0], "--cwd /home/user/dev/demo/wt ") {
		t.Errorf("first launch %q does not start in the panel's own directory", launches[0])
	}
	for _, want := range []string{"launch --type=tab --match window_id:7 ", "--cwd /home/user/dev/demo/other ", "claude --resume b"} {
		if !strings.Contains(launches[1], want) {
			t.Errorf("tab launch %q lacks %q", launches[1], want)
		}
	}
	focus := -1
	for i, c := range seq {
		if c == "focus-window --match id:7" {
			focus = i
		}
	}
	if focus < 0 || focus < slices.Index(seq, launches[1]) {
		t.Errorf("the OS window was not left on the workspace's first tab:\n%s", strings.Join(seq, "\n"))
	}
}

// Open is a launch sequence into the running kitty: the first panel opens the
// OS window and carries its identity, later panels split into it. Never a
// session file, which a running kitty answers with a second process.
func TestOpenBuildsTheLayoutWithLaunchSequences(t *testing.T) {
	h := &kitty.Host{}
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
			{Kind: revier.PanelAgent, Title: "agent", Command: []string{"claude"}},
			{Kind: revier.PanelShell, Title: "shell"},
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
	h := &kitty.Host{}
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
	h := &kitty.Host{}
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
	h := &kitty.Host{}
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
		Panels: []revier.PanelSpec{{Title: "agent", Command: []string{"claude"}}, {Title: "shell"}},
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
	h := &kitty.Host{}
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
