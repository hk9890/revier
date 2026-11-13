// What Open asks of hc and what it leaves behind when a panel fails, against
// a core the test serves itself. No hc, and no process but an sh script in
// hc's place, so the default layer runs it.
package hc_test

import (
	"maps"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hk9890/revier/internal/adapter/hc"
	"github.com/hk9890/revier/internal/adapter/proc"
	"github.com/hk9890/revier/pkg/revier"
)

// Each panel is a terminal with no parent, not kept, removed when its
// program exits, named after the instance and its place, and labelled with the instance, the name, its place
// and the realization's vars.
func TestOpenLabelsAndNamesEachTerminal(t *testing.T) {
	c, sock := newCore(t)
	dir := t.TempDir()
	h := &hc.Host{Socket: sock}
	r := revier.Realization{
		Name: "home", Dir: dir, Match: revier.Match{Title: "^home$"},
		Vars: map[string]string{"revier_target": "home"},
		Panels: []revier.PanelSpec{
			{Kind: revier.PanelAgent, Command: []string{"sh", "-c", "sleep 30"}, Dir: dir},
			{Kind: revier.PanelShell, Dir: dir},
		},
	}

	ref, err := h.Open(t.Context(), r)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if hellos := slices.DeleteFunc(c.called(), func(m string) bool { return m != "hello" }); len(hellos) != 1 {
		t.Errorf("Open said hello %d times, want once: one connection serves the PATH, the check and the runs", len(hellos))
	}
	id, ok := strings.CutPrefix(ref.ID, "core1/")
	if ref.Host != "hc" || ref.Title != "home" || !ok || !regexp.MustCompile(`^[0-9a-f]{16}$`).MatchString(id) {
		t.Fatalf("ref = %+v, want an hc ref core1/<16 hex digits> titled home", ref)
	}
	runs := c.paramsOf("terminal.run")
	if len(runs) != 2 {
		t.Fatalf("ran %d terminals, want 2", len(runs))
	}
	for i, run := range runs {
		labels := asMap(run["labels"])
		if labels["revier.instance"] != id || labels["revier.name"] != "home" || labels["revier.panel"] != []string{"0", "1"}[i] ||
			labels["revier_target"] != "home" || len(labels) != 4 {
			t.Errorf("run %d labels = %v, want the instance, name, place and var", i, labels)
		}
		if run["name"] != "rv-"+id+"-"+[]string{"0", "1"}[i] || run["no_parent"] != true || run["keep"] != false || run["rm"] != true || run["cwd"] != dir {
			t.Errorf("run %d = %v, want named after the instance, no parent, not kept, removed at exit, in %s", i, run, dir)
		}
	}
	// A bare program name could name one of the user's hc templates, which hc
	// would run in its place; a path never does.
	if argv := asList(runs[0]["argv"]); len(argv) != 3 || !filepath.IsAbs(argv[0].(string)) || filepath.Base(argv[0].(string)) != "sh" || runs[1]["argv"] != nil {
		t.Errorf("argv = %v and %v, want the agent's command with sh by its path, and none for the core's shell", argv, runs[1]["argv"])
	}

	instances, err := h.Instances(t.Context())
	if err != nil {
		t.Fatalf("Instances: %v", err)
	}
	if !matched(t, r.Match, instances, ref) {
		t.Errorf("no instance %s matched %q: %+v", ref.ID, r.Match.Title, instances)
	}
}

// A title holds whatever the realization's name holds: a label value is
// free text, so a separator, a newline or an '=' survives as written, and the
// realization's Match finds what Open made. A var's value survives the same
// way.
func TestFreeTextSurvivesTheRoundTrip(t *testing.T) {
	_, sock := newCore(t)
	h := &hc.Host{Socket: sock}
	name := "demo | pipe/slash\nline=ü\ttab"
	r := revier.Realization{
		Name: name, Match: revier.Match{Title: "^" + regexp.QuoteMeta(name) + "$"},
		Vars: map[string]string{"note": "a=b | c/d\nü"},
	}
	ref, err := h.Open(t.Context(), r)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	instances, err := h.Instances(t.Context())
	if err != nil {
		t.Fatalf("Instances: %v", err)
	}
	if !matched(t, r.Match, instances, ref) {
		t.Fatalf("no instance matched %q: %+v", name, instances)
	}
	if v := instances[0].Panels[0].Vars["note"]; v != r.Vars["note"] {
		t.Errorf("var = %q, want %q", v, r.Vars["note"])
	}
}

// A run that fails closes every terminal the Open started: left running, the
// instance would be matched with a panel short.
func TestAFailedRunClosesEveryTerminalOfTheInstance(t *testing.T) {
	c, sock := newCore(t)
	c.failRun = 2
	c.runErr = map[string]any{"code": -32000, "message": "cannot start \"sh\": Permission denied (os error 13)", "data": map[string]string{"kind": "spawn_failed"}}
	_, err := (&hc.Host{Socket: sock}).Open(t.Context(), revier.Realization{Name: "home", Panels: []revier.PanelSpec{
		{Kind: revier.PanelAgent, Command: []string{"sh"}}, {Kind: revier.PanelShell, Command: []string{"sh"}},
	}})
	if err == nil || !strings.Contains(err.Error(), "Permission denied") {
		t.Fatalf("Open = %v, want hc's spawn failure", err)
	}
	if live := c.live(); len(live) != 0 {
		t.Errorf("live terminals = %q, want none", live)
	}
	if kills, rms := c.paramsOf("terminal.kill"), c.paramsOf("terminal.rm"); len(kills) != 1 || len(rms) != 1 || rms[0]["terminal"] != "t1" {
		t.Errorf("killed %v, removed %v; want t1 killed and removed", kills, rms)
	}
}

// The program was found on the core's PATH before the run, so a file hc
// cannot find is another, such as the interpreter a script names: hc's own
// reason follows the panel's name.
func TestASpawnFailureIsThePanelsStartFailureWithHcsReason(t *testing.T) {
	c, sock := newCore(t)
	c.failRun = 1
	c.runErr = map[string]any{"code": -32000, "message": "cannot start \"sh\": No such file or directory (os error 2)", "data": map[string]string{"kind": "spawn_failed"}}
	_, err := (&hc.Host{Socket: sock}).Open(t.Context(), revier.Realization{Name: "home", Panels: []revier.PanelSpec{
		{Kind: revier.PanelAgent, Command: []string{"sh"}},
	}})
	want := `the agent panel's command "sh" did not start: hc terminal.run: cannot start "sh": No such file or directory (os error 2) (spawn_failed)`
	if err == nil || err.Error() != want {
		t.Errorf("Open = %v, want %s", err, want)
	}
}

// Every run sets empty, since hc's env cannot remove a variable, what a
// program must not inherit from whatever started the core: the terminal, the
// multiplexer, the agent session and the revier command it ran in. Of the
// terminal, the multiplexer and the command, only what the core's own
// environment holds is set: a tool that tests a variable for being set takes
// an empty one as set. Every agent session variable is set, held or not:
// Claude Code reads an empty one as unset, and a core after "hc server
// restart" holds a saved environment /proc does not show. hc sets its own
// HC_TERMINAL_ID over the run's, and Claude Code's configuration is the
// user's.
func TestEveryRunClearsTheVariablesTheCoreHolds(t *testing.T) {
	c, sock := newCore(t)
	held := []string{"KITTY_WINDOW_ID", "WINDOWID", "TMUX", "HERDR_TAB_ID", "HC_TERMINAL_ID", proc.TagVar, "AI_AGENT", "CLAUDECODE"}
	environ := "PATH=/bin\x00CLAUDE_CONFIG_DIR=/c\x00GIT_EDITOR=true\x00TMUX_PANES=x\x00"
	for _, name := range held {
		environ += name + "=x\x00"
	}
	h := &hc.Host{Socket: sock}
	h.SetProcRoot(coreEnviron(t, environ))
	if _, err := h.Open(t.Context(), revier.Realization{Name: "home", Panels: []revier.PanelSpec{
		{Kind: revier.PanelAgent, Command: []string{"/bin/sh"}}, {Kind: revier.PanelShell},
	}}); err != nil {
		t.Fatalf("Open: %v", err)
	}
	want := map[string]any{}
	for _, name := range slices.Concat(held, proc.AgentSessionVars) {
		want[name] = ""
	}
	for i, run := range c.paramsOf("terminal.run") {
		if env := asMap(run["env"]); !maps.Equal(env, want) {
			t.Errorf("run %d env = %v, want %v", i, env, want)
		}
	}
}

// A core whose environment cannot be read is given the agent session and
// revier command variables empty alone: a terminal's variable set empty would
// read as set.
func TestARunOfACoreWhoseEnvironmentIsUnreadClearsTheAgentSessionAndCommandAlone(t *testing.T) {
	c, sock := newCore(t)
	h := &hc.Host{Socket: sock}
	h.SetProcRoot(t.TempDir())
	if _, err := h.Open(t.Context(), revier.Realization{Name: "home"}); err != nil {
		t.Fatalf("Open: %v", err)
	}
	want := map[string]any{}
	for _, name := range slices.Concat(proc.AgentSessionVars, proc.CommandVars) {
		want[name] = ""
	}
	if env := asMap(c.paramsOf("terminal.run")[0]["env"]); !maps.Equal(env, want) {
		t.Errorf("run env = %v, want %v", env, want)
	}
}

// A program on the core's PATH alone opens, by its path there: the core
// starts it on its own PATH, not on revier's.
func TestAProgramOnTheCoresPATHAloneOpens(t *testing.T) {
	c, sock := newCore(t)
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "coreonly"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir())
	h := &hc.Host{Socket: sock}
	h.SetProcRoot(coreEnviron(t, "PATH="+bin+"\x00"))
	if _, err := h.Open(t.Context(), revier.Realization{Name: "home", Launch: []string{"coreonly", "-x"}}); err != nil {
		t.Fatalf("Open = %v, want the program found on the core's PATH", err)
	}
	if argv := asList(c.paramsOf("terminal.run")[0]["argv"]); len(argv) != 2 || argv[0] != filepath.Join(bin, "coreonly") {
		t.Errorf("argv = %v, want coreonly by its path on the core's PATH", argv)
	}
}

// A terminal of the instance whose revier.panel label names no place among
// the panels is none of them: the panel it stood for is missing, and the Open
// fails on it.
func TestATerminalWithABadPanelLabelIsNoneOfThePanels(t *testing.T) {
	for _, label := range []string{"2", "-1", "x"} {
		c, sock := newCore(t)
		c.panelOf = map[int]string{2: label}
		_, err := (&hc.Host{Socket: sock}).Open(t.Context(), revier.Realization{Name: "home", Panels: []revier.PanelSpec{
			{Kind: revier.PanelShell}, {Kind: revier.PanelAgent, Command: []string{"sh"}},
		}})
		if err == nil || !strings.Contains(err.Error(), "the agent panel's command \"sh\" exited as soon as it started") {
			t.Errorf("Open with revier.panel=%s = %v, want the agent panel named as exited", label, err)
		}
	}
}

// coreEnviron is a /proc tree in which the fake core, which is this process
// and so the socket's peer, holds environ.
func coreEnviron(t *testing.T, environ string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, strconv.Itoa(os.Getpid()))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "environ"), []byte(environ), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

// The core revier starts takes none of what a run clears, nor what hc sets
// in each of its terminals, nor the GIT_EDITOR=true of a Claude Code
// session, and keeps the rest: Claude Code's configuration, and the socket
// that names the core. The environment given is left as it was.
func TestTheCoreStartsWithoutTheVariablesOfWhatStartedIt(t *testing.T) {
	kept := []string{"PATH=/bin", "HC_SOCKET=/run/s.sock", "CLAUDE_CONFIG_DIR=/c", "CLAUDE_CODE_USE_BEDROCK=1", "ANTHROPIC_MODEL=m"}
	env := []string{"GIT_EDITOR=true"}
	for _, name := range append([]string{
		"KITTY_WINDOW_ID", "KITTY_SHELL_INTEGRATION", "WINDOWID", "TMUX", "HERDR_SESSION", "HERDR_WORKSPACE_ID",
		"HERDR_TAB_ID", "REVIER_TAG", "REVIER_HERDR_OPEN", "HC_TERMINAL_ID", "HC_TERMINAL_SOCKET", "HC_SCOPE",
	}, proc.AgentSessionVars...) {
		env = append(env, name+"=x")
	}
	env = append(env, kept...)
	given := slices.Clone(env)
	if got := hc.CoreEnv(env); !slices.Equal(got, kept) {
		t.Errorf("core env = %q, want %q", got, kept)
	}
	if !slices.Equal(env, given) {
		t.Errorf("CoreEnv changed its input to %q", env)
	}
}

// "hc server start" can exit 0 while the core it started still holds its
// stderr: the start worked, and does not wait for the core.
func TestAStartThatExitedWhileTheCoreHoldsStderrWorked(t *testing.T) {
	bin := t.TempDir()
	started := filepath.Join(bin, "started")
	script := "#!/bin/sh\n/bin/sleep 2 >&2 &\n: > " + started + "\nexit 0\n"
	if err := os.WriteFile(filepath.Join(bin, "herdr-core"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	t.Setenv("REVIER_STATE_HOME", t.TempDir())
	sock := socketFile(t)
	c := newFakeCore()
	// The core answers once the start ran, as hc's does.
	listening := make(chan net.Listener, 1)
	go func() {
		defer close(listening)
		for {
			if _, err := os.Stat(started); err == nil {
				break
			}
			select {
			case <-t.Context().Done():
				return
			case <-time.After(10 * time.Millisecond):
			}
		}
		l, err := net.Listen("unix", sock)
		if err != nil {
			return
		}
		listening <- l
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			go c.serve(conn)
		}
	}()
	t.Cleanup(func() {
		if l, ok := <-listening; ok {
			_ = l.Close()
		}
	})
	if _, err := (&hc.Host{Socket: sock}).Open(t.Context(), revier.Realization{Name: "home"}); err != nil {
		t.Errorf("Open = %v, want the started core to open the instance", err)
	}
}

// A start that fails is reported with what "hc server start" said, not as a
// socket the core answers elsewhere. What it said stays in a file under
// revier's state directory, which the next start empties.
func TestAFailedCoreStartGivesWhatHcSaid(t *testing.T) {
	dir := t.TempDir()
	write := func(said string) {
		script := "#!/bin/sh\necho '" + said + "' >&2\nexit 1\n"
		if err := os.WriteFile(filepath.Join(dir, "herdr-core"), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	stateDir := t.TempDir()
	t.Setenv("PATH", dir)
	t.Setenv("REVIER_STATE_HOME", stateDir)
	h := &hc.Host{Socket: filepath.Join(t.TempDir(), "none.sock")}

	write("hc: cannot start core: no room")
	_, err := h.Open(t.Context(), revier.Realization{Name: "home"})
	if err == nil || !strings.Contains(err.Error(), "hc: cannot start core: no room") || strings.Contains(err.Error(), "HC_SOCKET") {
		t.Errorf("Open = %v, want hc's own reason and no socket hint", err)
	}
	write("second")
	if _, err := h.Open(t.Context(), revier.Realization{Name: "home"}); err == nil || strings.Contains(err.Error(), "no room") {
		t.Errorf("second Open = %v, want what the second start said alone", err)
	}
	if said, err := os.ReadFile(filepath.Join(stateDir, "hc-server-start.log")); err != nil || string(said) != "second\n" {
		t.Errorf("start log = %q, %v; want what the last start said alone", said, err)
	}
}

// Two starts at the same time, by two revier processes or one, each report
// what their own "hc server start" said: the second does not empty the start
// log before the first has read it back.
func TestTwoFailedCoreStartsAtOnceEachGiveTheirOwnReason(t *testing.T) {
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	script := "#!/bin/sh\necho \"said $$\" >&2\n" + sleep + " 0.3\nexit 1\n"
	if err := os.WriteFile(filepath.Join(dir, "herdr-core"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("REVIER_STATE_HOME", t.TempDir())
	h := &hc.Host{Socket: filepath.Join(t.TempDir(), "none.sock")}
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i := range errs {
		wg.Go(func() { _, errs[i] = h.Open(t.Context(), revier.Realization{Name: "home"}) })
	}
	wg.Wait()
	for _, err := range errs {
		if err == nil || strings.Count(err.Error(), "said ") != 1 {
			t.Fatalf("Open = %v, want the reason of one start", err)
		}
	}
	if errs[0].Error() == errs[1].Error() {
		t.Errorf("both Opens = %v, want each its own start's reason", errs[0])
	}
}

// A panel whose program ended before the listing that follows the runs fails
// the Open, and its instance is closed. hc removed the terminal at its exit,
// so it is missing from the listing.
func TestAPanelThatExitedAtOnceFailsTheOpen(t *testing.T) {
	c, sock := newCore(t)
	c.exitRun = 2
	_, err := (&hc.Host{Socket: sock}).Open(t.Context(), revier.Realization{Name: "home", Panels: []revier.PanelSpec{
		{Kind: revier.PanelShell}, {Kind: revier.PanelAgent, Command: []string{"sh", "-c", "exit 1"}}, {Kind: revier.PanelShell},
	}})
	if err == nil || !strings.Contains(err.Error(), `the agent panel's command "sh -c exit 1" exited as soon as it started`) {
		t.Errorf("Open = %v, want the agent panel named as exited", err)
	}
	if records := c.records(); len(records) != 0 {
		t.Errorf("records = %q, want none", records)
	}
	if kills := c.paramsOf("terminal.kill"); len(kills) != 2 || kills[0]["terminal"] == "t2" || kills[1]["terminal"] == "t2" {
		t.Errorf("killed %v, want t1 and t3, the panels that still ran", kills)
	}
}

// What hc would not keep as written, or a directory that is not there, is
// refused before anything runs.
func TestOpenRefusesWhatHcWouldNotKeep(t *testing.T) {
	c, sock := newCore(t)
	h := &hc.Host{Socket: sock}
	for name, r := range map[string]revier.Realization{
		"no name":         {},
		"a key with =":    {Name: "home", Vars: map[string]string{"a=b": "x"}},
		"an empty value":  {Name: "home", Vars: map[string]string{"k": ""}},
		"revier's label":  {Name: "home", Vars: map[string]string{"revier.name": "x"}},
		"no such dir":     {Name: "home", Dir: filepath.Join(t.TempDir(), "gone")},
		"not UTF-8 value": {Name: "home", Vars: map[string]string{"k": "\xff"}},
	} {
		if _, err := h.Open(t.Context(), r); err == nil {
			t.Errorf("%s: Open passed", name)
		}
	}
	if runs := c.paramsOf("terminal.run"); len(runs) != 0 {
		t.Errorf("ran %d terminals, want none", len(runs))
	}
}
