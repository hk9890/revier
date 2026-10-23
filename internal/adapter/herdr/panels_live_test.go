//go:build live

// Layer L4: the panel operations against a real herdr server, headless, of
// each test's own.
package herdr_test

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hk9890/revier/internal/adapter/herdr"
	"github.com/hk9890/revier/pkg/revier"
)

// home opens a workspace of one sh panel and focuses it, and returns its ref
// and that panel.
func home(t *testing.T, h *herdr.Host) (revier.TargetRef, revier.PanelID) {
	t.Helper()
	c := ctx(t)
	ref, err := h.Open(c, revier.Realization{Name: "home", Launch: []string{"sh", "-c", "sleep 600"}})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := h.Focus(c, ref); err != nil {
		t.Fatalf("Focus: %v", err)
	}
	panel, err := h.FocusedPanel(c, ref)
	if err != nil || panel == "" {
		t.Fatalf("FocusedPanel = %q, %v", panel, err)
	}
	return ref, panel
}

// A tab opens after the workspace's tabs, with the vars on each of its panes,
// and the focus stays where it was (decisions.md D118). FocusPanel then goes
// to it.
func TestOpenTabAppendsATabWithoutTheFocus(t *testing.T) {
	h, c := server(t), ctx(t)
	ref, first := home(t, h)

	vars := map[string]string{"revier_target": "agent"}
	panel, err := h.OpenTab(c, ref, revier.Realization{Panels: []revier.PanelSpec{
		{Kind: revier.PanelAgent, Command: []string{"sh", "-c", "sleep 600"}},
		{Kind: revier.PanelShell},
	}}, vars)
	if err != nil {
		t.Fatalf("OpenTab: %v", err)
	}
	if got, err := h.FocusedPanel(c, ref); err != nil || got != first {
		t.Errorf("after OpenTab, FocusedPanel = %q, %v; want %q still", got, err, first)
	}

	got, err := h.Instances(c)
	if err != nil || len(got) != 1 {
		t.Fatalf("Instances = %+v, %v", got, err)
	}
	panels := got[0].Panels
	i := slices.IndexFunc(panels, func(p revier.Panel) bool { return p.ID == panel })
	if i < 1 || len(panels) != 3 {
		t.Fatalf("panels = %+v, want the home panel, then %s and one more", panels, panel)
	}
	for _, p := range panels[1:] {
		if p.Tab != panels[i].Tab || p.Tab == panels[0].Tab || p.Vars["revier_target"] != "agent" {
			t.Errorf("panel %+v, want it in the new tab with its var", p)
		}
	}

	if err := h.FocusPanel(c, ref, panel); err != nil {
		t.Fatalf("FocusPanel: %v", err)
	}
	if got, err := h.FocusedPanel(c, ref); err != nil || got != panel {
		t.Errorf("after FocusPanel, FocusedPanel = %q, %v; want %q", got, err, panel)
	}
}

// A workspace that is not shown keeps its own focused pane.
func TestFocusedPanelOfAWorkspaceNotShown(t *testing.T) {
	h, c := server(t), ctx(t)
	ref, first := home(t, h)
	other, err := h.Open(c, revier.Realization{Name: "other", Launch: []string{"sh", "-c", "sleep 600"}})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := h.Focus(c, other); err != nil {
		t.Fatalf("Focus: %v", err)
	}
	if got, err := h.FocusedPanel(c, ref); err != nil || got != first {
		t.Errorf("FocusedPanel = %q, %v; want %q", got, err, first)
	}
}

// A tab whose panel exits at once is named and closed, and the workspace keeps
// the tabs it had.
func TestOpenTabThatFailsIsClosed(t *testing.T) {
	h, c := server(t), ctx(t)
	ref, first := home(t, h)
	_, err := h.OpenTab(c, ref, revier.Realization{Panels: []revier.PanelSpec{
		{Kind: revier.PanelShell, Command: []string{"sh", "-c", "sleep 600"}},
		{Kind: revier.PanelAgent, Command: []string{"sh", "-c", "exit 3"}},
	}}, map[string]string{"revier_target": "agent"})
	if err == nil || !strings.Contains(err.Error(), `the agent panel's command "sh -c exit 3" exited as soon as it started`) {
		t.Fatalf("OpenTab = %v, want the agent panel named as exited", err)
	}
	got, err := h.Instances(c)
	if err != nil || len(got) != 1 || len(got[0].Panels) != 1 || got[0].Panels[0].ID != first {
		t.Errorf("instances = %+v, %v; want the home panel alone", got, err)
	}
}

// The screen comes with its colours as SGR and lines ending in "\n"; the
// scrollback holds what scrolled off it.
func TestReadPanelReadsTheScreenWithItsColours(t *testing.T) {
	h, c := server(t), ctx(t)
	ref, _ := home(t, h)
	script := `printf '\033[1;31mred\033[0m plain\n'; i=0; while [ $i -lt 60 ]; do i=$((i+1)); echo line$i; done; sleep 600`
	panel, err := h.OpenTab(c, ref, revier.Realization{Launch: []string{"sh", "-c", script}}, map[string]string{"revier_target": "log"})
	if err != nil {
		t.Fatalf("OpenTab: %v", err)
	}

	screen := waitRead(t, h, ref, panel, false, "line60")
	if strings.Contains(screen, "\r") || strings.Contains(screen, "red") {
		t.Errorf("screen = %q, want line60 at the bottom, red scrolled off, no \\r", screen)
	}
	all := waitRead(t, h, ref, panel, true, "line60")
	if !strings.Contains(all, "\x1b[") || !strings.Contains(all, "red") || !strings.Contains(all, "line1\n") {
		t.Errorf("scrollback = %q, want red in its colour and line1", all)
	}
}

// Text arrives as typed: nothing submits it until a "\r" of its own.
func TestSendTextTypesIntoThePane(t *testing.T) {
	h, c := server(t), ctx(t)
	ref, _ := home(t, h)
	panel, err := h.OpenTab(c, ref, revier.Realization{Launch: []string{"sh", "-c", "read l; echo got:$l; sleep 600"}},
		map[string]string{"revier_target": "agent"})
	if err != nil {
		t.Fatalf("OpenTab: %v", err)
	}
	if err := h.SendText(c, ref, panel, "hello"); err != nil {
		t.Fatalf("SendText: %v", err)
	}
	waitRead(t, h, ref, panel, false, "hello")
	if screen, _ := h.ReadPanel(c, ref, panel, false); strings.Contains(screen, "got:") {
		t.Fatalf("screen = %q, submitted with no \\r", screen)
	}
	if err := h.SendText(c, ref, panel, "\r"); err != nil {
		t.Fatalf("SendText: %v", err)
	}
	waitRead(t, h, ref, panel, false, "got:hello")
}

// A pane closes alone; the tab goes with its last pane, and the workspace
// keeps its other tab.
func TestClosePanelEndsAPaneThenTheTab(t *testing.T) {
	h, c := server(t), ctx(t)
	ref, first := home(t, h)
	panel, err := h.OpenTab(c, ref, revier.Realization{Panels: []revier.PanelSpec{
		{Kind: revier.PanelAgent, Command: []string{"sh", "-c", "sleep 600"}},
		{Kind: revier.PanelShell},
	}}, map[string]string{"revier_target": "agent"})
	if err != nil {
		t.Fatalf("OpenTab: %v", err)
	}
	ids := func() []revier.PanelID {
		got, err := h.Instances(c)
		if err != nil || len(got) != 1 {
			t.Fatalf("Instances = %+v, %v", got, err)
		}
		var out []revier.PanelID
		for _, p := range got[0].Panels {
			out = append(out, p.ID)
		}
		return out
	}
	before := ids()
	if err := h.ClosePanel(c, ref, panel); err != nil {
		t.Fatalf("ClosePanel: %v", err)
	}
	after := ids()
	if len(after) != 2 || slices.Contains(after, panel) {
		t.Fatalf("after one close, panels = %q, want two of %q without %s", after, before, panel)
	}
	if err := h.ClosePanel(c, ref, after[1]); err != nil {
		t.Fatalf("ClosePanel: %v", err)
	}
	if got := ids(); !slices.Equal(got, []revier.PanelID{first}) {
		t.Errorf("panels = %q, want the home panel alone", got)
	}
}

// waitRead reads the panel until it holds want: a pane draws after the
// request that started it returns.
func waitRead(t *testing.T, h *herdr.Host, ref revier.TargetRef, panel revier.PanelID, scrollback bool, want string) string {
	t.Helper()
	c, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for {
		got, err := h.ReadPanel(c, ref, panel, scrollback)
		if err != nil {
			t.Fatalf("ReadPanel: %v", err)
		}
		if strings.Contains(got, want) {
			return got
		}
		if c.Err() != nil {
			t.Fatalf("panel %s never showed %q: %q", panel, want, got)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
