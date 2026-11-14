// What Instances costs and how it reads hc's terminal list, against a core
// the test serves itself. No recorded output and no hc, so the default layer
// runs it.
package hc_test

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/adapter/hc"
	"github.com/hk9890/revier/pkg/revier"
)

// One hello and one terminal.list, whatever the project count.
func TestInstancesCostOneHelloAndOneListWhateverTheProjectCount(t *testing.T) {
	const projects = 90
	c, sock := newCore(t)
	for i := range projects {
		for p := range 2 {
			c.add(&fakeTerminal{Address: fmt.Sprintf("t%d%d", i, p), State: "live", Labels: map[string]string{
				"revier.instance": fmt.Sprintf("%016x", i), "revier.name": fmt.Sprintf("project-%d", i), "revier.panel": fmt.Sprint(p),
			}})
		}
	}

	got, err := (&hc.Host{Socket: sock}).Instances(t.Context())
	if err != nil {
		t.Fatalf("Instances: %v", err)
	}
	if len(got) != projects {
		t.Fatalf("got %d instances, want %d", len(got), projects)
	}
	if calls := c.called(); !slices.Equal(calls, []string{"hello", "terminal.list"}) {
		t.Errorf("hc asked %q, want one hello and one terminal.list", calls)
	}
}

// Terminals that share an instance label are one instance, its panels in
// their order; a terminal made by hand is one of its own; a finished one, and
// one on another machine, are left out.
func TestInstancesGroupTheTerminalsByTheirInstanceLabel(t *testing.T) {
	c, sock := newCore(t)
	label := func(panel string) map[string]string {
		return map[string]string{"revier.instance": "0a1b2c3d4e5f6a7b", "revier.name": "home", "revier.panel": panel, "revier_target": "agent"}
	}
	c.add(&fakeTerminal{Address: "t2", State: "live", PID: 20, Argv: []string{"/bin/sh"}, Cwd: "/p", Labels: label("1")})
	c.add(&fakeTerminal{Address: "t1", State: "live", PID: 10, Argv: []string{"sh", "-c", "claude"}, Cwd: "/p", Title: "✳ Claude",
		Labels: label("0"), Foreground: map[string]any{"pid": 11, "name": "claude", "argv": []string{"claude"}, "cwd": "/p/sub"}})
	c.add(&fakeTerminal{Address: "t3", Name: "scratch", State: "live", PID: 30, Argv: []string{"htop"}, Cwd: "/"})
	c.add(&fakeTerminal{Address: "t4", State: "live", PID: 40, Argv: []string{"vim"}, Cwd: "/"})
	c.add(&fakeTerminal{Address: "t5", State: "finished", Argv: []string{"true"}, Labels: label("2")})
	c.add(&fakeTerminal{Address: "box/t1", State: "live", PID: 50, Argv: []string{"sh"}})

	got, err := (&hc.Host{Socket: sock}).Instances(t.Context())
	if err != nil {
		t.Fatalf("Instances: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d instances, want home and the two by hand: %+v", len(got), got)
	}
	home := got[0]
	if want := (revier.TargetRef{Host: "hc", ID: "core1/0a1b2c3d4e5f6a7b", Title: "home"}); home.Ref != want || home.Title != "home" {
		t.Errorf("home = %+v titled %q, want %+v", home.Ref, home.Title, want)
	}
	if len(home.Panels) != 2 {
		t.Fatalf("home panels = %+v, want t1 and t2", home.Panels)
	}
	agent, shell := home.Panels[0], home.Panels[1]
	if agent.ID != "t1" || agent.Kind != revier.PanelTool || agent.PID != 11 || agent.Title != "✳ Claude" ||
		!slices.Equal(agent.Command, []string{"claude"}) || agent.Dir != "/p/sub" || agent.Tab != "" {
		t.Errorf("agent panel = %+v, want t1 running its foreground claude, pid 11, in /p/sub", agent)
	}
	if len(agent.Vars) != 1 || agent.Vars["revier_target"] != "agent" {
		t.Errorf("agent vars = %v, want revier_target alone: revier's own labels are not vars", agent.Vars)
	}
	if shell.ID != "t2" || shell.Kind != revier.PanelShell || shell.PID != 20 || shell.Dir != "/p" {
		t.Errorf("shell panel = %+v, want t2, a shell with no foreground read yet, pid 20, in /p", shell)
	}
	if named, bare := got[1], got[2]; named.Title != "scratch" || named.Ref.ID != "core1/t3" ||
		bare.Title != "t4" || bare.Ref.ID != "core1/t4" || len(bare.Panels) != 1 || bare.Panels[0].ID != "t4" {
		t.Errorf("by hand = %+v and %+v, want t3 titled by its name and t4 by its address", named, bare)
	}
}

// A revier.instance label that is no instance id, as one a user set by hand,
// makes no instance: each terminal that carries it is one of its own, by its
// address, and its ref reaches it.
func TestALabelThatIsNoInstanceIDIsATerminalOfItsOwn(t *testing.T) {
	c, sock := newCore(t)
	for _, address := range []string{"t1", "t2"} {
		c.add(&fakeTerminal{Address: address, State: "live", Labels: map[string]string{"revier.instance": "test", "revier.name": "mine"}})
	}
	h := &hc.Host{Socket: sock}
	got, err := h.Instances(t.Context())
	if err != nil {
		t.Fatalf("Instances: %v", err)
	}
	if len(got) != 2 || got[0].Ref.ID != "core1/t1" || got[1].Ref.ID != "core1/t2" || len(got[0].Panels) != 1 {
		t.Fatalf("instances = %+v, want t1 and t2, each of its own", got)
	}
	if _, err := h.ReadPanel(t.Context(), got[0].Ref, "t1", false); err != nil {
		t.Errorf("ReadPanel of t1 by its ref = %v", err)
	}
	if err := h.Close(t.Context(), got[0].Ref); err != nil {
		t.Fatalf("Close of t1 by its ref = %v", err)
	}
	if live := c.live(); !slices.Equal(live, []string{"t2"}) {
		t.Errorf("after Close live = %q, want t2", live)
	}
}

// The probe takes herdr-core, or an hc that links to it, and never runs
// either; an hc that is another program, such as Holochain's CLI, is not
// herdr-core.
func TestProbeTakesHerdrCoreAlone(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "none.sock")
	for name, dir := range map[string]string{"herdr-core": fakeBin(t, "herdr-core"), "an hc link": hcLink(t)} {
		t.Setenv("PATH", dir)
		if err := (&hc.Host{Socket: sock}).Probe(t.Context()); err != nil || ran(dir) {
			t.Errorf("Probe with %s = %v, ran it %v; want nil, not run", name, err, ran(dir))
		}
	}
	other := fakeBin(t, "hc")
	t.Setenv("PATH", other)
	if err := (&hc.Host{Socket: sock}).Probe(t.Context()); err == nil || !strings.Contains(err.Error(), "herdr-core not found (an hc on PATH is not herdr-core)") {
		t.Errorf("Probe with another hc = %v, want it refused", err)
	}
	if _, err := (&hc.Host{}).AttachCommand(home); err == nil {
		t.Error("AttachCommand with another hc passed")
	}
	if ran(other) {
		t.Error("the probe ran hc")
	}
}

// No core is no instances, not an error: Open starts one.
func TestInstancesWithNoCoreAreNone(t *testing.T) {
	got, err := (&hc.Host{Socket: filepath.Join(t.TempDir(), "none.sock")}).Instances(t.Context())
	if err != nil || got != nil {
		t.Errorf("Instances = %+v, %v; want none and no error", got, err)
	}
}

// A core that lacks a feature SendText types with, framed mode, the attach
// or automatic input, is refused by the probe, which names it; a socket
// nobody listens on is not.
func TestProbeRefusesACoreThatLacksWhatTheHostNeeds(t *testing.T) {
	t.Setenv("PATH", fakeBin(t, "herdr-core"))

	c, sock := newCore(t)
	if err := (&hc.Host{Socket: sock}).Probe(t.Context()); err != nil {
		t.Errorf("Probe of a core with all it needs = %v", err)
	}
	for _, missing := range []string{"framed", "attach", "automatic_input"} {
		c.mu.Lock()
		c.features = slices.DeleteFunc([]string{"framed", "attach", "automatic_input"}, func(f string) bool { return f == missing })
		c.mu.Unlock()
		if err := (&hc.Host{Socket: sock}).Probe(t.Context()); err == nil || !strings.Contains(err.Error(), missing) {
			t.Errorf("Probe of a core with no %s = %v, want it named", missing, err)
		}
	}
	if err := (&hc.Host{Socket: filepath.Join(t.TempDir(), "none.sock")}).Probe(t.Context()); err != nil {
		t.Errorf("Probe with no core running = %v, want nil", err)
	}
}

// The socket is the one hc's CLI would use: HC_SOCKET, else the session's in
// the runtime directory, else beside the session's state.
func TestTheSocketIsTheOneHcWouldUse(t *testing.T) {
	t.Setenv("HC_SOCKET", "/x/s.sock")
	t.Setenv("HC_SESSION", "")
	t.Setenv("XDG_RUNTIME_DIR", "/run/me")
	if got := hc.SocketPath(); got != "/x/s.sock" {
		t.Errorf("with HC_SOCKET: %s", got)
	}
	t.Setenv("HC_SOCKET", "")
	if got := hc.SocketPath(); got != "/run/me/herdr-core/default.sock" {
		t.Errorf("with a runtime dir: %s", got)
	}
	t.Setenv("HC_SESSION", " work ")
	if got := hc.SocketPath(); got != "/run/me/herdr-core/work.sock" {
		t.Errorf("with a session: %s", got)
	}
	if _, err := os.Stat(fmt.Sprintf("/run/user/%d", os.Getuid())); err == nil {
		return // the state directory is used only on a machine with no /run/user
	}
	t.Setenv("XDG_RUNTIME_DIR", "")
	t.Setenv("XDG_STATE_HOME", "/st")
	if got := hc.SocketPath(); got != "/st/herdr-core/work/work.sock" {
		t.Errorf("with no runtime dir: %s", got)
	}
}
