//go:build live

// Layer L4: a real herdr server, headless.
//
// Every test here runs against a server of its own, on a socket in a scratch
// directory, started and stopped by the test. Nothing attaches a client,
// nothing reaches a display, and the user's own herdr is never visible to it.
// A test arranges what the host does not make, such as a moved pane,
// through herdr's API directly.
package herdr_test

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/hk9890/revier/internal/adapter/herdr"
	"github.com/hk9890/revier/pkg/revier"
)

// call sends one request to the host's server and decodes its result into
// out, and fails the test when either goes wrong. It is how a test arranges
// the server, for what the host does not make.
func call(t *testing.T, h *herdr.Host, method string, params, out any) {
	t.Helper()
	if err := h.Request(ctx(t), method, params, out); err != nil {
		t.Fatal(err)
	}
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
		// The agent pane runs bash until bash execs claude, and a listing
		// taken in between must not end the wait.
		if agent.Runs("claude") && slices.ContainsFunc(inst.Panels, func(p revier.Panel) bool { return p.PID != 0 && p.Kind == revier.PanelShell }) {
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
