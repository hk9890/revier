//go:build live

// Layer L4: the hc host against a real core of the test's own, headless.
// Each test starts its core through Open, as a user's first press does.
package hc_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/hk9890/revier/internal/adapter/hc"
	"github.com/hk9890/revier/internal/adapter/proc"
	"github.com/hk9890/revier/pkg/revier"
)

func TestOpenThenMatchFindsIt(t *testing.T) {
	h, c := world(t), ctx(t)
	real := revier.Realization{
		Name:   "diff",
		Launch: []string{"sh", "-c", "sleep 30"},
		Match:  revier.Match{Title: "^diff$"},
	}

	ref, err := h.Open(c, real)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if ref.Host != "hc" || ref.ID == "" {
		t.Fatalf("ref = %+v, want an hc ref with an id", ref)
	}
	if err := h.Probe(c); err != nil {
		t.Errorf("Probe of the core Open started: %v", err)
	}

	instances, err := h.Instances(c)
	if err != nil {
		t.Fatalf("Instances: %v", err)
	}
	if !matched(t, real.Match, instances, ref) {
		t.Fatalf("no instance matched %q; got %+v", real.Match.Title, instances)
	}
}

// Each panel is listed with the process in its foreground, which the Claude
// probe finds an agent by, and with the realization's vars.
func TestInstancesAfterOpenNameEachPanelsProcess(t *testing.T) {
	h, c := world(t), ctx(t)
	dir := t.TempDir()
	ref, err := h.Open(c, revier.Realization{
		Name: "home", Dir: dir, Vars: map[string]string{"revier_target": "home"},
		Panels: []revier.PanelSpec{
			{Kind: revier.PanelAgent, Command: []string{"sh", "-c", "exec sleep 30"}, Dir: dir},
			{Kind: revier.PanelShell, Dir: dir},
		},
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	got := waitFor(t, h, func(in []revier.Instance) bool {
		return len(in) == 1 && len(in[0].Panels) == 2 && slices.Equal(in[0].Panels[0].Command, []string{"sleep", "30"})
	})
	home := got[0]
	if home.Ref != ref || home.Title != "home" {
		t.Errorf("instance = %+v, want %+v", home.Ref, ref)
	}
	agent, shell := home.Panels[0], home.Panels[1]
	if cmdline, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(agent.PID), "cmdline")); err != nil || string(cmdline) != "sleep\x0030\x00" {
		t.Errorf("agent pid %d runs %q, %v; want sleep 30", agent.PID, cmdline, err)
	}
	if agent.Kind != revier.PanelTool || agent.Dir != dir || agent.Vars["revier_target"] != "home" {
		t.Errorf("agent = %+v, want a tool in %s with its var", agent, dir)
	}
	if shell.Kind != revier.PanelShell || shell.Vars["revier_target"] != "home" {
		t.Errorf("shell = %+v, want the core's shell with its var", shell)
	}
}

// A program Open starts carries no variable of the terminal that started
// the core: not of the one revier ran in when Open started the core, and,
// set empty, not of the one a user started the core from by hand, nor of the
// Claude Code session a core restarted by hand saved. A core Open starts has
// none of them, nor the GIT_EDITOR=true a Claude Code session sets, so its
// programs are given none of the terminal's, not even empty, and the agent
// session's empty, which Claude Code reads as unset.
func TestTheCoreOpenStartsHasNoneOfTheTerminalsOwnEnvironment(t *testing.T) {
	envOf := func(t *testing.T, h *hc.Host) string {
		out := filepath.Join(t.TempDir(), "env")
		if _, err := h.Open(ctx(t), revier.Realization{Name: "env", Launch: []string{"sh", "-c", "env > " + out + "; exec sleep 30"}}); err != nil {
			t.Fatalf("Open: %v", err)
		}
		return "\n" + waitForFile(t, out, func(s string) bool { return strings.Contains(s, "REVIER_HC_KEPT=yes") })
	}

	t.Run("a core Open started", func(t *testing.T) {
		h := world(t)
		t.Setenv("KITTY_WINDOW_ID", "99")
		t.Setenv("TMUX", "/tmp/tmux-1/default,1,0")
		t.Setenv("TMUX_PANE", "%9")
		for _, name := range proc.AgentSessionVars {
			t.Setenv(name, "leaked")
		}
		t.Setenv("GIT_EDITOR", "true")
		t.Setenv("REVIER_HC_KEPT", "yes")
		env := envOf(t, h)
		if strings.Contains(env, "KITTY_WINDOW_ID=99") || strings.Contains(env, "TMUX_PANE=%9") ||
			strings.Contains(env, "=leaked\n") || strings.Contains(env, "GIT_EDITOR=true\n") {
			t.Errorf("the program's environment carries the terminal's own:\n%s", env)
		}
		if strings.Contains(env, "\nTMUX=") {
			t.Errorf("the program's environment sets TMUX, which the core does not hold:%s", env)
		}
		if !strings.Contains(env, "\nAI_AGENT=\n") {
			t.Errorf("the program's environment does not set AI_AGENT empty:%s", env)
		}
		pid, err := h.CorePID(ctx(t))
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range append([]string{"GIT_EDITOR", "KITTY_WINDOW_ID", "TMUX_PANE"}, proc.AgentSessionVars...) {
			if v, ok := proc.EnvIn("/proc", pid, name); ok {
				t.Errorf("the core was started with %s=%s", name, v)
			}
		}
	})

	t.Run("a core started by hand", func(t *testing.T) {
		h := world(t)
		bin, err := hc.Program()
		if err != nil {
			t.Fatal(err)
		}
		start := exec.Command(bin, "server", "start")
		start.Env = append(os.Environ(), "TMUX=/tmp/tmux-1/default,1,0", "KITTY_WINDOW_ID=99", "REVIER_TAG=t9", "REVIER_HC_KEPT=yes")
		for _, name := range proc.AgentSessionVars {
			start.Env = append(start.Env, name+"=leaked")
		}
		start.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		start.WaitDelay = time.Second
		if out, err := start.CombinedOutput(); err != nil {
			t.Fatalf("hc server start: %v: %s", err, out)
		}
		env := envOf(t, h)
		for _, kv := range []string{"TMUX=/tmp/tmux-1/default,1,0", "KITTY_WINDOW_ID=99", "REVIER_TAG=t9", "=leaked\n"} {
			if strings.Contains(env, kv) {
				t.Errorf("the program's environment carries %s of the terminal the core was started from:\n%s", kv, env)
			}
		}
		for _, name := range []string{"TMUX", "REVIER_TAG", "AI_AGENT"} {
			if !strings.Contains(env, "\n"+name+"=\n") {
				t.Errorf("the program's environment does not set %s empty:%s", name, env)
			}
		}
	})

	t.Run("a core restarted by hand", func(t *testing.T) {
		h := world(t)
		bin, err := hc.Program()
		if err != nil {
			t.Fatal(err)
		}
		run := func(env []string, args ...string) {
			cmd := exec.Command(bin, args...)
			cmd.Env = env
			cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
			cmd.WaitDelay = time.Second
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("hc %s: %v: %s", strings.Join(args, " "), err, out)
			}
		}
		started := append(os.Environ(), "REVIER_HC_KEPT=yes")
		for _, name := range proc.AgentSessionVars {
			started = append(started, name+"=leaked")
		}
		run(started, "server", "start")
		// The restart runs without the session; the core sets the
		// environment it saved, which /proc does not show.
		run(hc.CoreEnv(os.Environ()), "server", "restart")
		pid, err := h.CorePID(ctx(t))
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := proc.EnvIn("/proc", pid, "AI_AGENT"); ok {
			t.Logf("the restarted core shows AI_AGENT in /proc: this run does not prove the saved environment is cleared")
		}
		env := envOf(t, h)
		if strings.Contains(env, "=leaked\n") {
			t.Errorf("the program's environment carries the saved session:%s", env)
		}
		for _, name := range []string{"AI_AGENT", "CLAUDECODE", "CLAUDE_CODE_SESSION_ID"} {
			if !strings.Contains(env, "\n"+name+"=\n") {
				t.Errorf("the program's environment does not set %s empty:%s", name, env)
			}
		}
	})
}

// Where there is a service manager, the core Open starts runs as the service
// of the socket the host dials, with that socket and the session it is for,
// which the manager's environment does not name, so its programs get the
// manager's environment and none of revier's: a venv or a credential of the
// shell revier ran in does not reach them. A systemd-run first on PATH stands
// in for the manager: it records how it was called and runs the command with
// an environment of its own, as the manager does, and only that.
func TestTheCoreStartsAsAServiceWithTheManagersEnvironment(t *testing.T) {
	h := world(t)
	name, err := hc.Program()
	if err != nil {
		t.Fatal(err)
	}
	bin, err := exec.LookPath(name)
	if err != nil {
		t.Fatal(err)
	}
	fake, argv := t.TempDir(), filepath.Join(t.TempDir(), "argv")
	var manager []string
	for _, v := range []string{"PATH", "HOME", "XDG_CONFIG_HOME", "XDG_STATE_HOME", "XDG_RUNTIME_DIR", "SHELL"} {
		manager = append(manager, v+"='"+os.Getenv(v)+"'")
	}
	script := `#!/bin/sh
echo "$@" > '` + argv + `'
set --  "$@" --end
env=
while [ "$1" != "--" ]; do
	case "$1" in
	--setenv=*) env="$env ${1#--setenv=}" ;;
	--property=StandardError=append:*) exec 2>>"${1#--property=StandardError=append:}" ;;
	esac
	shift
done
shift
cmd=
while [ "$1" != "--end" ]; do cmd="$cmd '$1'"; shift; done
eval exec env -i ` + strings.Join(manager, " ") + ` REVIER_HC_MANAGER=yes $env $cmd
`
	if err := os.WriteFile(filepath.Join(fake, "systemd-run"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fake+":"+os.Getenv("PATH"))
	t.Setenv("REVIER_HC_LEAK", "yes")
	t.Setenv("HC_SESSION", "rv")

	out := filepath.Join(t.TempDir(), "env")
	if _, err := h.Open(ctx(t), revier.Realization{Name: "env", Launch: []string{"sh", "-c", "env > " + out + "; exec sleep 30"}}); err != nil {
		t.Fatalf("Open: %v", err)
	}
	env := "\n" + waitForFile(t, out, func(s string) bool { return strings.Contains(s, "REVIER_HC_MANAGER=yes") })
	if strings.Contains(env, "REVIER_HC_LEAK") {
		t.Errorf("the program's environment carries revier's own:%s", env)
	}
	called, err := os.ReadFile(argv)
	if err != nil {
		t.Fatalf("systemd-run never ran: %v", err)
	}
	log := filepath.Join(os.Getenv("REVIER_STATE_HOME"), "hc-server-start.log")
	want := "--user --quiet --collect --unit=" + hc.CoreUnit(h.Socket) + " --property=Type=forking --property=StandardOutput=null" +
		" --property=StandardError=append:" + log + " --setenv=HC_SOCKET=" + h.Socket + " --setenv=HC_SESSION=rv -- " + bin + " server start\n"
	if string(called) != want {
		t.Errorf("systemd-run %q, want %q", called, want)
	}
}

// The core of each socket is a service of its own: while the core of one hc
// session runs, the first Open on another session's socket starts that
// session's core, and neither takes the other's service for its own. A
// systemd-run and a systemctl first on PATH stand in for the manager: a
// second start of a unit that runs fails, and is-active passes for a unit
// that runs.
func TestTheCoreOfEachSocketStartsAsAServiceOfItsOwn(t *testing.T) {
	h := world(t)
	other := &hc.Host{Socket: filepath.Join(filepath.Dir(h.Socket), "b.sock")}
	t.Cleanup(func() { stop(other) })
	fake, units := t.TempDir(), filepath.Join(t.TempDir(), "units")
	manager := map[string]string{
		"systemd-run": `#!/bin/sh
env=
while [ "$1" != "--" ]; do
	case "$1" in
	--unit=*) unit=${1#--unit=} ;;
	--setenv=*) env="$env ${1#--setenv=}" ;;
	esac
	shift
done
shift
grep -qx "$unit" '` + units + `' 2>/dev/null && exit 1
echo "$unit" >> '` + units + `'
exec env $env "$@"
`,
		"systemctl": `#!/bin/sh
for unit; do :; done
grep -qx "$unit" '` + units + `'
`,
	}
	for name, script := range manager {
		if err := os.WriteFile(filepath.Join(fake, name), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", fake+":"+os.Getenv("PATH"))

	for i, host := range []*hc.Host{h, other} {
		t.Setenv("HC_SESSION", "rv"+strconv.Itoa(i))
		if _, err := host.Open(ctx(t), revier.Realization{Name: "s", Launch: []string{"sh", "-c", "exec sleep 30"}}); err != nil {
			t.Fatalf("Open on %s: %v", host.Socket, err)
		}
	}
	got, err := os.ReadFile(units)
	if err != nil {
		t.Fatal(err)
	}
	if want := hc.CoreUnit(h.Socket) + "\n" + hc.CoreUnit(other.Socket) + "\n"; string(got) != want {
		t.Errorf("units started = %q, want one per socket, %q", got, want)
	}
	first, err := h.CorePID(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	second, err := other.CorePID(ctx(t))
	if err != nil || second == first {
		t.Errorf("the second socket's core = %d, %v; want a core of its own, not %d", second, err, first)
	}
}

// The core Open starts can run on a PATH that is not revier's, as one that
// "hc server start" hands to a systemd user unit does: a program on the
// core's PATH alone opens.
func TestAProgramOnTheStartedCoresPATHAloneOpens(t *testing.T) {
	h := world(t)
	name, err := hc.Program()
	if err != nil {
		t.Fatal(err)
	}
	bin, err := exec.LookPath(name)
	if err != nil {
		t.Fatal(err)
	}
	coreOnly, wrapper := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(coreOnly, "coreonly"), []byte("#!/bin/sh\nexec sleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	wrap := "#!/bin/sh\nPATH=" + coreOnly + ":$PATH exec " + bin + " \"$@\"\n"
	// Named herdr-core, which the host runs before any hc.
	if err := os.WriteFile(filepath.Join(wrapper, "herdr-core"), []byte(wrap), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", wrapper+":"+os.Getenv("PATH"))
	if _, err := h.Open(ctx(t), revier.Realization{Name: "core", Launch: []string{"coreonly"}}); err != nil {
		t.Errorf("Open = %v, want the program found on the core's PATH", err)
	}
}

// The bytes arrive as given: no Enter is added, and an escape that ends a
// bracketed paste is not taken out.
func TestSendTextDeliversTheExactBytes(t *testing.T) {
	h, c := world(t), ctx(t)
	out := filepath.Join(t.TempDir(), "typed")
	ref, err := h.Open(c, revier.Realization{Name: "typed", Launch: []string{"sh", "-c", "stty raw -echo; exec cat > " + out}})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	panel := waitFor(t, h, func(in []revier.Instance) bool {
		return len(in) == 1 && slices.Equal(in[0].Panels[0].Command, []string{"cat"})
	})[0].Panels[0].ID

	text := "fix it\x1b[201~ ä"
	if err := h.SendText(c, ref, panel, text); err != nil {
		t.Fatalf("SendText: %v", err)
	}
	waitForFile(t, out, func(s string) bool { return s == text })
	// An Enter added after a pause, as hc's own send does, would arrive here.
	time.Sleep(300 * time.Millisecond)
	if got, _ := os.ReadFile(out); string(got) != text {
		t.Errorf("typed %q, want %q", got, text)
	}
}

func TestReadPanelReadsTheScreen(t *testing.T) {
	h, c := world(t), ctx(t)
	ref, err := h.Open(c, revier.Realization{Name: "read", Launch: []string{"sh", "-c", "printf 'first line\\nready marker\\n'; exec sleep 30"}})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	panel := waitFor(t, h, func(in []revier.Instance) bool { return len(in) == 1 })[0].Panels[0].ID
	deadline := time.Now().Add(5 * time.Second)
	for {
		screen, err := h.ReadPanel(c, ref, panel, false)
		if err != nil {
			t.Fatalf("ReadPanel: %v", err)
		}
		if strings.HasPrefix(screen, "first line\nready marker") && !strings.Contains(screen, "\x1b") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("screen = %q, want the two lines as plain text", screen)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if all, err := h.ReadPanel(c, ref, panel, true); err != nil || !strings.Contains(all, "ready marker") {
		t.Errorf("ReadPanel with scrollback = %q, %v", all, err)
	}
}

// ClosePanel ends one terminal and leaves its instance; Close ends the rest,
// and neither leaves a record behind.
func TestClosePanelThenClose(t *testing.T) {
	h, c := world(t), ctx(t)
	sleep := []string{"sh", "-c", "exec sleep 30"}
	ref, err := h.Open(c, revier.Realization{Name: "two", Panels: []revier.PanelSpec{{Command: sleep}, {Command: sleep}}})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	panels := waitFor(t, h, func(in []revier.Instance) bool { return len(in) == 1 && len(in[0].Panels) == 2 })[0].Panels

	if err := h.ClosePanel(c, ref, panels[1].ID); err != nil {
		t.Fatalf("ClosePanel: %v", err)
	}
	got, err := h.Instances(c)
	if err != nil || len(got) != 1 || len(got[0].Panels) != 1 || got[0].Panels[0].ID != panels[0].ID {
		t.Fatalf("after ClosePanel = %+v, %v; want the instance with %s alone", got, err, panels[0].ID)
	}
	if err := h.Close(c, ref); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got, err := h.Instances(c); err != nil || len(got) != 0 {
		t.Errorf("after Close = %+v, %v; want none", got, err)
	}
	var list struct {
		Terminals []any `json:"terminals"`
	}
	if err := h.Request(c, "terminal.list", map[string]bool{"all": true}, &list); err != nil || len(list.Terminals) != 0 {
		t.Errorf("records left = %+v, %v; want none", list.Terminals, err)
	}
}

// A panel's command that ends leaves its terminal finished, for the window
// that shows it to hold, and a read of it is what it left (decisions.md
// D187).
func TestACommandThatEndsLeavesItsTerminalFinished(t *testing.T) {
	h, c := world(t), ctx(t)
	done := filepath.Join(t.TempDir(), "done")
	ref, err := h.Open(c, revier.Realization{Name: "exits", Launch: []string{"sh", "-c", `echo left-behind; while [ ! -e "$0" ]; do sleep 0.05; done`, done}})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	panel := waitFor(t, h, func(in []revier.Instance) bool { return len(in) == 1 })[0].Panels[0].ID
	if err := os.WriteFile(done, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	waitForRecords(t, c, h, ref, "finished")

	if got, err := h.ReadPanel(c, ref, panel, false); err != nil || !strings.Contains(got, "left-behind") {
		t.Errorf("ReadPanel of %s = %q, %v; want what it left", panel, got, err)
	}
	if got, err := h.Instances(c); err != nil || len(got) != 0 {
		t.Errorf("Instances = %+v, %v; want none: a finished terminal is no instance", got, err)
	}
}

// A shell that exits leaves no record: hc removes it at the exit, as the
// kitty runtime holds no shell, and a read of it is a panel that is gone
// (decisions.md D172).
func TestAShellThatExitsLeavesNoRecord(t *testing.T) {
	h, c := world(t), ctx(t)
	ref, err := h.Open(c, revier.Realization{Name: "shell", Panels: []revier.PanelSpec{{Kind: revier.PanelShell}}})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	panel := waitFor(t, h, func(in []revier.Instance) bool { return len(in) == 1 })[0].Panels[0].ID
	if err := h.SendText(c, ref, panel, "exit\n"); err != nil {
		t.Fatalf("SendText: %v", err)
	}
	waitForRecords(t, c, h, ref, "")

	if _, err := h.ReadPanel(c, ref, panel, false); !errors.Is(err, revier.ErrPanelGone) {
		t.Errorf("ReadPanel of %s = %v, want ErrPanelGone", panel, err)
	}
}

// A close of an instance one of whose programs ended, as a shutdown closes
// each instance, ends the panel that runs and leaves no record, the
// finished one's included.
func TestCloseOfAnInstanceWithAnEndedPanelLeavesNoRecord(t *testing.T) {
	h, c := world(t), ctx(t)
	done := filepath.Join(t.TempDir(), "done")
	ref, err := h.Open(c, revier.Realization{Name: "half", Panels: []revier.PanelSpec{
		{Command: []string{"sh", "-c", "exec sleep 30"}}, {Command: untilFile(done)},
	}})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := os.WriteFile(done, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	waitForRecords(t, c, h, ref, "finished live")
	if err := h.Close(c, ref); err != nil {
		t.Fatalf("Close: %v", err)
	}
	var list struct {
		Terminals []any `json:"terminals"`
	}
	if err := h.Request(c, "terminal.list", map[string]bool{"all": true}, &list); err != nil || len(list.Terminals) != 0 {
		t.Errorf("records left = %+v, %v; want none", list.Terminals, err)
	}
}

// untilFile is a command that runs until the file exists.
func untilFile(path string) []string {
	return []string{"sh", "-c", `while [ ! -e "$0" ]; do sleep 0.05; done`, path}
}

// A program hc cannot start, after a panel it started, and a program that is
// not on the core's PATH, fail the Open and leave nothing running. A program
// that ends at once is caught only when it ended before Open's listing, a race
// the default layer pins with a canned list.
func TestAPanelThatDoesNotStartFailsTheOpenAndLeavesNothing(t *testing.T) {
	h, c := world(t), ctx(t)
	notExecutable := filepath.Join(t.TempDir(), "agent")
	if err := os.WriteFile(notExecutable, []byte("#!/bin/sh\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := h.Open(c, revier.Realization{Name: "denied", Panels: []revier.PanelSpec{
		{Kind: revier.PanelShell}, {Kind: revier.PanelAgent, Command: []string{notExecutable}},
	}})
	if err == nil || !strings.Contains(err.Error(), "spawn_failed") {
		t.Errorf("Open = %v, want hc's spawn failure", err)
	}
	_, err = h.Open(c, revier.Realization{Name: "missing", Panels: []revier.PanelSpec{
		{Kind: revier.PanelShell}, {Kind: revier.PanelAgent, Command: []string{"no-such-program-revier"}},
	}})
	if err == nil || !strings.Contains(err.Error(), "is not on PATH") {
		t.Errorf("Open = %v, want the program named as not on PATH", err)
	}
	var list struct {
		Terminals []any `json:"terminals"`
	}
	if err := h.Request(c, "terminal.list", map[string]bool{"all": true}, &list); err != nil || len(list.Terminals) != 0 {
		t.Errorf("records left = %+v, %v; want none", list.Terminals, err)
	}
}

// The attach names a terminal the core knows by that name.
func TestAttachCommandNamesTheFirstTerminal(t *testing.T) {
	h, c := world(t), ctx(t)
	ref, err := h.Open(c, revier.Realization{Name: "attach", Launch: []string{"sh", "-c", "exec sleep 30"}})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	argv, err := h.AttachCommand(ref)
	if err != nil {
		t.Fatalf("AttachCommand: %v", err)
	}
	var got struct {
		Labels map[string]string `json:"labels"`
	}
	if err := h.Request(c, "terminal.get", map[string]string{"terminal": argv[len(argv)-1]}, &got); err != nil || got.Labels["revier.panel"] != "0" {
		t.Errorf("terminal %s = %+v, %v; want the instance's first", argv[len(argv)-1], got, err)
	}
}

func waitForFile(t *testing.T, path string, ok func(string) bool) string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		got, _ := os.ReadFile(path)
		if ok(string(got)) {
			return string(got)
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s never settled: %q", path, got)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
