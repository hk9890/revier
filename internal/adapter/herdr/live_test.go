//go:build live

// Layer L4: a real herdr server, headless.
//
// Every test here runs against a server of its own, on a socket in a scratch
// directory, started and stopped by the test. Nothing attaches a client,
// nothing reaches a display, and the user's own herdr is never visible to it.
// The host opens nothing yet, so a test arranges the server through herdr's
// API directly.
package herdr_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"
	"unicode"

	"github.com/hk9890/revier/internal/adapter/herdr"
	"github.com/hk9890/revier/pkg/revier"
)

// server starts a herdr server of this test's own, headless, with its config,
// state and socket in a scratch directory, and stops it when the test ends.
// Nothing attaches a client, and the user's own herdr is never reached: the
// environment that names it is left out of the server's. A pane's shell is
// sh, not the user's: their shell's startup files would run in every pane,
// and could attach their own tmux or hold a pane's foreground.
func server(t *testing.T) *herdr.Host {
	t.Helper()
	bin, err := exec.LookPath("herdr")
	if err != nil {
		t.Skip("herdr not installed; L4 needs it")
	}
	// Named after the test, cut short: a Unix socket path longer than 107
	// bytes cannot be bound, and t.TempDir carries the test's whole name.
	dir, err := os.MkdirTemp("", "rvh-"+shortName(t)+"-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sock := filepath.Join(dir, "s.sock")

	cmd := exec.Command(bin, "server")
	cmd.Env = append(scrubbed(os.Environ()),
		"XDG_CONFIG_HOME="+dir, "XDG_STATE_HOME="+dir, "HERDR_SOCKET_PATH="+sock, "SHELL=/bin/sh")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start herdr server: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		_, _ = cmd.Process.Wait()
	})

	h := &herdr.Host{Socket: sock}
	deadline := time.Now().Add(10 * time.Second)
	for {
		ping, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := h.Request(ping, "ping", struct{}{}, nil)
		cancel()
		if err == nil {
			return h
		}
		if time.Now().After(deadline) {
			t.Fatalf("herdr server on %s never answered ping: %v", sock, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// scrubbed is the environment without what points herdr at another server or
// tells it it runs inside one.
func scrubbed(env []string) []string {
	var out []string
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		switch name {
		case "HERDR_ENV", "HERDR_SESSION", "HERDR_SOCKET_PATH", "HERDR_CONFIG_PATH",
			"HERDR_PANE_ID", "HERDR_TAB_ID", "HERDR_WORKSPACE_ID", "XDG_CONFIG_HOME", "XDG_STATE_HOME":
			continue
		}
		out = append(out, kv)
	}
	return out
}

// shortName is the test's name with its letters and digits alone, at most 40
// of them: enough to tell which test left a socket behind.
func shortName(t *testing.T) string {
	name := strings.Map(func(r rune) rune {
		if r < 128 && (unicode.IsLetter(r) || unicode.IsDigit(r)) {
			return r
		}
		return -1
	}, strings.TrimPrefix(t.Name(), "Test"))
	return name[:min(len(name), 40)]
}

// call sends one request to the host's server and decodes its result into
// out, and fails the test when either goes wrong. It is how a test arranges
// the server: the host does not open yet.
func call(t *testing.T, h *herdr.Host, method string, params, out any) {
	t.Helper()
	if err := h.Request(ctx(t), method, params, out); err != nil {
		t.Fatal(err)
	}
}

func ctx(t *testing.T) context.Context {
	c, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	return c
}

func TestProbeFindsHerdr(t *testing.T) {
	if err := server(t).Probe(ctx(t)); err != nil {
		t.Fatalf("Probe: %v", err)
	}
}

// A server with nothing on it is an empty list, not an error: the survey has
// to render on a machine where nothing has been opened yet.
func TestInstancesOnAnEmptyServer(t *testing.T) {
	h := server(t)
	got, err := h.Instances(ctx(t))
	if err != nil || len(got) != 0 {
		t.Fatalf("Instances = %+v, %v; want none", got, err)
	}
	focused, err := h.Focused(ctx(t))
	if err != nil || !focused.IsZero() {
		t.Errorf("Focused = %+v, %v; want nothing", focused, err)
	}
}

// A workspace is an instance titled by its label as written, and each pane is
// a panel that names its tab, its tokens and the process in its foreground.
func TestInstancesListAWorkspaceWithItsPanes(t *testing.T) {
	h := server(t)
	const label = "demo|pipe\nline"
	var created struct {
		Workspace struct {
			ID string `json:"workspace_id"`
		} `json:"workspace"`
	}
	call(t, h, "workspace.create", map[string]any{"label": label, "cwd": t.TempDir(), "focus": false}, &created)
	var applied struct {
		Layout struct {
			TabID string `json:"tab_id"`
		} `json:"layout"`
	}
	call(t, h, "layout.apply", map[string]any{
		"workspace_id": created.Workspace.ID, "focus": false,
		"root": map[string]any{"type": "pane", "command": []string{"bash", "-c", "exec -a claude sleep 600"}},
	}, &applied)
	var tab struct {
		Layout struct {
			FocusedPaneID string `json:"focused_pane_id"`
		} `json:"layout"`
	}
	call(t, h, "layout.export", map[string]any{"tab_id": applied.Layout.TabID}, &tab)
	agentPane := tab.Layout.FocusedPaneID
	call(t, h, "pane.report_metadata", map[string]any{
		"pane_id": agentPane, "source": "revier", "tokens": map[string]string{"revier_target": "agent"},
	}, &struct{}{})

	var inst revier.Instance
	var agent revier.Panel
	c := ctx(t)
	deadline := time.Now().Add(10 * time.Second)
	for {
		got, err := h.Instances(c)
		if err != nil {
			t.Fatalf("Instances: %v", err)
		}
		if len(got) == 1 {
			inst = got[0]
			if i := slices.IndexFunc(inst.Panels, func(p revier.Panel) bool { return string(p.ID) == agentPane }); i >= 0 {
				agent = inst.Panels[i]
			}
		}
		if agent.PID != 0 && slices.ContainsFunc(inst.Panels, func(p revier.Panel) bool { return p.PID != 0 && p.Kind == revier.PanelShell }) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("instances = %+v; want one workspace with a shell pane and %s running claude", got, agentPane)
		}
		time.Sleep(50 * time.Millisecond)
	}

	if inst.Title != label || inst.Ref.Title != label || inst.Ref.Host != "herdr" {
		t.Errorf("instance = %+v, want host herdr titled %q", inst.Ref, label)
	}
	if !agent.Runs("claude") || agent.Kind != revier.PanelTool || agent.Tab != applied.Layout.TabID || agent.Vars["revier_target"] != "agent" {
		t.Errorf("agent panel = %+v, want claude as a tool in %s with its token", agent, applied.Layout.TabID)
	}
	if len(inst.Panels) != 2 {
		t.Errorf("panels = %+v, want the root shell and the agent", inst.Panels)
	}
}

// Focused is the workspace the server last moved to.
func TestFocusedIsTheWorkspaceTheServerLastMovedTo(t *testing.T) {
	h := server(t)
	var ids []string
	for i := range 2 {
		var created struct {
			Workspace struct {
				ID string `json:"workspace_id"`
			} `json:"workspace"`
		}
		call(t, h, "workspace.create", map[string]any{"label": fmt.Sprintf("w-%d", i), "focus": false}, &created)
		ids = append(ids, created.Workspace.ID)
	}
	call(t, h, "workspace.focus", map[string]any{"workspace_id": ids[1]}, &struct{}{})

	focused, err := h.Focused(ctx(t))
	if err != nil {
		t.Fatalf("Focused: %v", err)
	}
	got, err := h.Instances(ctx(t))
	if err != nil {
		t.Fatalf("Instances: %v", err)
	}
	if i := slices.IndexFunc(got, func(in revier.Instance) bool { return in.Title == "w-1" }); i < 0 || focused != got[i].Ref {
		t.Errorf("Focused = %+v, want w-1 among %+v", focused, got)
	}
}

// A pane moved to another workspace gets a new id while its process keeps the
// old one in its environment; the panel still names the process it runs.
func TestAMovedPaneKeepsItsProcess(t *testing.T) {
	h := server(t)
	var from, to struct {
		Workspace struct {
			ID string `json:"workspace_id"`
		} `json:"workspace"`
		Tab struct {
			ID string `json:"tab_id"`
		} `json:"tab"`
	}
	call(t, h, "workspace.create", map[string]any{"label": "from", "focus": false}, &from)
	call(t, h, "workspace.create", map[string]any{"label": "to", "focus": false}, &to)
	var applied struct {
		Layout struct {
			FocusedPaneID string `json:"focused_pane_id"`
		} `json:"layout"`
	}
	call(t, h, "layout.apply", map[string]any{
		"workspace_id": from.Workspace.ID, "focus": false,
		"root": map[string]any{"type": "pane", "command": []string{"bash", "-c", "exec -a claude sleep 600"}},
	}, &applied)
	call(t, h, "pane.move", map[string]any{
		"pane_id": applied.Layout.FocusedPaneID, "focus": false,
		"destination": map[string]any{"type": "tab", "tab_id": to.Tab.ID, "split": "right"},
	}, &struct{}{})

	c := ctx(t)
	deadline := time.Now().Add(10 * time.Second)
	for {
		got, err := h.Instances(c)
		if err != nil {
			t.Fatalf("Instances: %v", err)
		}
		i := slices.IndexFunc(got, func(in revier.Instance) bool { return in.Title == "to" })
		if i >= 0 && slices.ContainsFunc(got[i].Panels, func(p revier.Panel) bool {
			return p.PID != 0 && p.Runs("claude") && string(p.ID) != applied.Layout.FocusedPaneID
		}) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("instances = %+v; want the moved claude pane in workspace to, under a new id, with its process", got)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
