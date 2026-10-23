//go:build live

// Layer L4: Open, Focus, Close and AttachCommand against a real herdr server,
// headless, of each test's own.
package herdr_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hk9890/revier/internal/adapter/herdr"
	"github.com/hk9890/revier/pkg/revier"
)

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
	if ref.Host != "herdr" || ref.ID == "" || ref.Title != "diff" {
		t.Fatalf("ref = %+v, want a herdr ref titled diff", ref)
	}
	m, err := real.Match.Compile()
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	got, err := h.Instances(c)
	if err != nil {
		t.Fatalf("Instances: %v", err)
	}
	if i := slices.IndexFunc(got, func(in revier.Instance) bool { return m.Matches(in) }); i < 0 || got[i].Ref != ref {
		t.Fatalf("no instance matched %q with ref %+v; got %+v", real.Match.Title, ref, got)
	}
}

// A user renames a workspace in herdr freely: the token Open left on it keeps
// the title its Match finds.
func TestARenamedWorkspaceKeepsItsTitle(t *testing.T) {
	h, c := server(t), ctx(t)
	ref, err := h.Open(c, revier.Realization{Name: "diff", Launch: []string{"sh", "-c", "sleep 30"}})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	_, workspace, _ := strings.Cut(ref.ID, "/")
	if err := h.Request(c, "workspace.rename", map[string]string{"workspace_id": workspace, "label": "renamed"}, nil); err != nil {
		t.Fatalf("workspace.rename: %v", err)
	}
	got, err := h.Instances(c)
	if err != nil || len(got) != 1 || got[0].Title != "diff" || got[0].Ref != ref {
		t.Errorf("instances = %+v, %v; want one titled diff with ref %+v", got, err, ref)
	}
}

// The most panels a tab holds open side by side: herdr refuses a layout more
// than 16 levels deep, which one split per panel would exceed.
func TestOpenLaysOutTheMostPanelsATabHolds(t *testing.T) {
	h, c := server(t), ctx(t)
	panels := make([]revier.PanelSpec, 24)
	for i := range panels {
		panels[i] = revier.PanelSpec{Kind: revier.PanelShell, Command: []string{"sh", "-c", "sleep 30"}}
	}
	if _, err := h.Open(c, revier.Realization{Name: "wide", Panels: panels}); err != nil {
		t.Fatalf("Open: %v", err)
	}
	got, err := h.Instances(c)
	if err != nil || len(got) != 1 || len(got[0].Panels) != len(panels) {
		t.Errorf("instances = %+v, %v; want one with %d panels", got, err, len(panels))
	}
}

// A name herdr cannot keep as a token - a newline is taken out of one - is
// still the workspace's label, which then stands as the title.
func TestANameHerdrCannotKeepAsATokenIsTheLabel(t *testing.T) {
	h, c := server(t), ctx(t)
	const name = "demo|pipe\nline"
	ref, err := h.Open(c, revier.Realization{Name: name, Launch: []string{"sh", "-c", "sleep 30"}})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	got, err := h.Instances(c)
	if err != nil {
		t.Fatalf("Instances: %v", err)
	}
	if len(got) != 1 || got[0].Title != name || got[0].Ref != ref {
		t.Errorf("instances = %+v, want one titled %q with ref %+v", got, name, ref)
	}
}

// The panels are one tab, side by side, each running its command in its
// directory with the realization's vars; the shell herdr starts a workspace
// with is gone.
func TestOpenBuildsThePanelsInOneTab(t *testing.T) {
	h, c := server(t), ctx(t)
	dir := t.TempDir()
	_, err := h.Open(c, revier.Realization{
		Name: "work",
		Panels: []revier.PanelSpec{
			{Kind: revier.PanelAgent, Title: "agent", Command: []string{"bash", "-c", "exec -a claude sleep 600"}, Dir: dir},
			{Kind: revier.PanelShell, Dir: dir},
		},
		Vars: map[string]string{"revier_home": "home", "revier_target": "home"},
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	var inst revier.Instance
	waitFor(t, h, func(got []revier.Instance) bool {
		if len(got) != 1 {
			return false
		}
		inst = got[0]
		return len(inst.Panels) == 2 && inst.Panels[0].Runs("claude") && inst.Panels[1].Kind == revier.PanelShell
	})
	a, b := inst.Panels[0], inst.Panels[1]
	if a.Tab != b.Tab {
		t.Errorf("panels in tabs %s and %s, want one tab", a.Tab, b.Tab)
	}
	for _, p := range inst.Panels {
		if p.Vars["revier_home"] != "home" || p.Vars["revier_target"] != "home" {
			t.Errorf("panel %s vars = %v, want the realization's", p.ID, p.Vars)
		}
	}
	if cwd, err := os.Readlink("/proc/" + strconv.Itoa(a.PID) + "/cwd"); err != nil || cwd != dir {
		t.Errorf("agent runs in %q (%v), want %s", cwd, err, dir)
	}
}

func TestOpenRefusesAPanelWhoseCommandIsNotOnPath(t *testing.T) {
	h, c := server(t), ctx(t)
	_, err := h.Open(c, revier.Realization{Name: "x", Panels: []revier.PanelSpec{
		{Kind: revier.PanelAgent, Command: []string{"revier-no-such-program"}},
	}})
	if err == nil || !strings.Contains(err.Error(), `the agent panel's command "revier-no-such-program" is not on PATH`) {
		t.Errorf("Open = %v, want the agent panel's command named as not on PATH", err)
	}
}

// A panel whose command ends at once is named, and nothing of the workspace
// is left to be matched.
func TestOpenSaysAPanelWhoseCommandExitsAtOnceExited(t *testing.T) {
	h, c := server(t), ctx(t)
	_, err := h.Open(c, revier.Realization{Name: "x", Panels: []revier.PanelSpec{
		{Kind: revier.PanelShell, Command: []string{"sh", "-c", "sleep 30"}},
		{Kind: revier.PanelAgent, Command: []string{"sh", "-c", "exit 3"}},
	}})
	if err == nil || !strings.Contains(err.Error(), `the agent panel's command "sh -c exit 3" exited as soon as it started`) {
		t.Fatalf("Open = %v, want the agent panel named as exited", err)
	}
	got, err := h.Instances(c)
	if err != nil || len(got) != 0 {
		t.Errorf("instances = %+v, %v; want the workspace closed", got, err)
	}
}

// Open leaves the focus where it was, and Focus moves it; Close ends the
// workspace and every pane in it.
func TestFocusFocusedAndCloseRoundTrip(t *testing.T) {
	h, c := server(t), ctx(t)
	launch := []string{"sh", "-c", "sleep 30"}
	first, err := h.Open(c, revier.Realization{Name: "first", Launch: launch})
	if err != nil {
		t.Fatalf("Open first: %v", err)
	}
	if err := h.Focus(c, first); err != nil {
		t.Fatalf("Focus first: %v", err)
	}
	second, err := h.Open(c, revier.Realization{Name: "second", Launch: launch})
	if err != nil {
		t.Fatalf("Open second: %v", err)
	}
	if got, err := h.Focused(c); err != nil || got != first {
		t.Errorf("after Open second, Focused = %+v, %v; want %+v", got, err, first)
	}
	if err := h.Focus(c, second); err != nil {
		t.Fatalf("Focus second: %v", err)
	}
	if got, err := h.Focused(c); err != nil || got != second {
		t.Errorf("Focused = %+v, %v; want %+v", got, err, second)
	}

	if err := h.Close(c, second); err != nil {
		t.Fatalf("Close: %v", err)
	}
	got, err := h.Instances(c)
	if err != nil || len(got) != 1 || got[0].Ref != first {
		t.Errorf("instances = %+v, %v; want first alone", got, err)
	}
}

// With no server running, Open starts one, which outlives the call.
func TestOpenStartsTheServerWhenNoneRuns(t *testing.T) {
	if _, err := exec.LookPath("herdr"); err != nil {
		t.Skip("herdr not installed; L4 needs it")
	}
	dir, err := os.MkdirTemp("", "rvh-"+shortName(t)+"-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	// The server Open starts takes this process's environment: it must not
	// read or write the user's herdr, nor run their shell.
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("XDG_STATE_HOME", dir)
	t.Setenv("SHELL", "/bin/sh")
	for _, name := range herdrEnv {
		t.Setenv(name, "")
		_ = os.Unsetenv(name)
	}
	h := &herdr.Host{Socket: filepath.Join(dir, "s.sock")}
	t.Cleanup(func() {
		stop, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = h.Request(stop, "server.stop", struct{}{}, nil)
		for h.Request(stop, "ping", struct{}{}, nil) == nil && stop.Err() == nil {
			time.Sleep(50 * time.Millisecond)
		}
	})

	c := ctx(t)
	ref, err := h.Open(c, revier.Realization{Name: "first", Launch: []string{"sh", "-c", "sleep 30"}})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	got, err := h.Instances(c)
	if err != nil || len(got) != 1 || got[0].Ref != ref {
		t.Errorf("instances = %+v, %v; want the workspace Open made", got, err)
	}
}
