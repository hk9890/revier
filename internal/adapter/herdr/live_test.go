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
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/hk9890/revier/pkg/revier"
)

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
		RootPane struct {
			ID string `json:"pane_id"`
		} `json:"root_pane"`
	}
	_ = call(t, h.Socket, "workspace.create", map[string]any{"label": label, "cwd": t.TempDir(), "focus": false}, &created)
	var applied struct {
		Layout struct {
			TabID string `json:"tab_id"`
		} `json:"layout"`
	}
	_ = call(t, h.Socket, "layout.apply", map[string]any{
		"workspace_id": created.Workspace.ID, "focus": false,
		"root": map[string]any{"type": "pane", "command": []string{"bash", "-c", "exec -a claude sleep 600"}},
	}, &applied)
	var agentPane string
	var tab struct {
		Layout struct {
			FocusedPaneID string `json:"focused_pane_id"`
		} `json:"layout"`
	}
	_ = call(t, h.Socket, "layout.export", map[string]any{"tab_id": applied.Layout.TabID}, &tab)
	agentPane = tab.Layout.FocusedPaneID
	_ = call(t, h.Socket, "pane.report_metadata", map[string]any{
		"pane_id": agentPane, "source": "revier", "tokens": map[string]string{"revier_target": "agent"},
	}, &struct{}{})

	var inst revier.Instance
	var agent revier.Panel
	deadline := time.Now().Add(10 * time.Second)
	for {
		got, err := h.Instances(ctx(t))
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
		_ = call(t, h.Socket, "workspace.create", map[string]any{"label": fmt.Sprintf("w-%d", i), "focus": false}, &created)
		ids = append(ids, created.Workspace.ID)
	}
	_ = call(t, h.Socket, "workspace.focus", map[string]any{"workspace_id": ids[1]}, &struct{}{})

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
