// What OpenTab asks of hc and what Instances makes of the tabs, against a
// core the test serves itself.
package hc_test

import (
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/adapter/hc"
	"github.com/hk9890/revier/pkg/revier"
)

// tabsOf is the tab of each panel of the instance, in the listing's order.
func tabsOf(t *testing.T, h *hc.Host, ref revier.TargetRef) []revier.Panel {
	t.Helper()
	instances, err := h.Instances(t.Context())
	if err != nil {
		t.Fatalf("Instances: %v", err)
	}
	for _, in := range instances {
		if in.Ref == ref {
			return in.Panels
		}
	}
	t.Fatalf("no instance %s in %+v", ref.ID, instances)
	return nil
}

// A tab is the terminals that share the instance's labels and a token of
// their own, after every tab the instance holds. Two tabs of the same vars,
// as two agent tabs, are two tabs, and the listing keeps their order and the
// panels' order within each (decisions.md D179).
func TestOpenTabStartsALastTabOfItsOwnToken(t *testing.T) {
	c, sock := newCore(t)
	h := &hc.Host{Socket: sock}
	dir := t.TempDir()
	ref, err := h.Open(t.Context(), revier.Realization{Name: "home", Vars: map[string]string{"revier_target": "tickets"}, Panels: []revier.PanelSpec{
		{Kind: revier.PanelShell, Command: []string{"sh", "-c", "sleep 30"}, Dir: dir},
	}})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	agent := revier.Realization{Panels: []revier.PanelSpec{
		{Kind: revier.PanelAgent, Command: []string{"sh", "-c", "sleep 30"}, Dir: dir},
		{Kind: revier.PanelShell, Dir: dir},
	}}
	first, err := h.OpenTab(t.Context(), ref, agent, map[string]string{"revier_target": "agent"})
	if err != nil {
		t.Fatalf("OpenTab: %v", err)
	}
	second, err := h.OpenTab(t.Context(), ref, agent, map[string]string{"revier_target": "agent"})
	if err != nil {
		t.Fatalf("second OpenTab: %v", err)
	}
	if first != "t2" || second != "t4" {
		t.Errorf("OpenTab = %s, %s; want each tab's first terminal, t2 and t4", first, second)
	}

	id := strings.TrimPrefix(ref.ID, "core1/")
	runs := c.paramsOf("terminal.run")
	for i, run := range runs[1:] {
		labels := asMap(run["labels"])
		if labels["revier.instance"] != id || labels["revier.name"] != "home" || labels["revier.tabplace"] != []string{"1", "1", "2", "2"}[i] ||
			labels["revier.panel"] != []string{"0", "1", "0", "1"}[i] || labels["revier_target"] != "agent" {
			t.Errorf("tab run %d labels = %v, want the instance, its name, the tab's place, the panel's place and the var", i, labels)
		}
		token, _ := labels["revier.tab"].(string)
		if want := regexp.MustCompile(`^rv-` + id + `-` + token[:8] + `-[01]$`); !want.MatchString(run["name"].(string)) {
			t.Errorf("tab run %d named %v, want rv-<instance>-<token>-<place>", i, run["name"])
		}
	}
	if argv := asList(runs[1]["argv"]); len(argv) != 3 || !filepath.IsAbs(argv[0].(string)) {
		t.Errorf("tab argv = %v, want the agent's command with sh by its path", argv)
	}

	panels := tabsOf(t, h, ref)
	var ids, tabs []string
	for _, p := range panels {
		ids, tabs = append(ids, p.ID.String()), append(tabs, p.Tab)
	}
	if !slices.Equal(ids, []string{"t1", "t2", "t3", "t4", "t5"}) {
		t.Errorf("panels = %v, want the first tab, then each tab in its order", ids)
	}
	if tabs[0] == "" || tabs[1] != tabs[2] || tabs[3] != tabs[4] || tabs[0] == tabs[1] || tabs[1] == tabs[3] {
		t.Errorf("tabs = %q, want three tabs: t1, t2 and t3, t4 and t5", tabs)
	}
	for _, p := range panels {
		if _, ok := p.Vars["revier.tab"]; ok || len(p.Vars) != 1 {
			t.Errorf("panel %s vars = %v, want the var alone: the tab is Panel.Tab", p.ID, p.Vars)
		}
	}
}

// The listing orders panels by the tab's place, then the panel's, whatever
// order the core lists them in: a view that opens again draws the same tabs
// in the same order.
func TestInstancesOrderThePanelsByTabThenPlace(t *testing.T) {
	c, sock := newCore(t)
	labels := func(tab, place, panel string) map[string]string {
		return map[string]string{"revier.instance": "0a1b2c3d4e5f6a7b", "revier.name": "home", "revier.tab": tab, "revier.tabplace": place, "revier.panel": panel}
	}
	c.add(&fakeTerminal{Address: "t9", State: "live", Labels: labels("b", "2", "1")})
	c.add(&fakeTerminal{Address: "t8", State: "live", Labels: labels("b", "2", "0")})
	c.add(&fakeTerminal{Address: "t3", State: "live", Labels: labels("c", "10", "0")})
	c.add(&fakeTerminal{Address: "t5", State: "live", Labels: labels("a", "0", "0")})
	panels := tabsOf(t, &hc.Host{Socket: sock}, home)
	var got []string
	for _, p := range panels {
		got = append(got, p.ID.String()+":"+p.Tab)
	}
	if want := []string{"t5:a", "t8:b", "t9:b", "t3:c"}; !slices.Equal(got, want) {
		t.Errorf("panels = %v, want %v", got, want)
	}
}

// A tab whose terminal fails is closed whole, and the tabs before it stay:
// the core names the agent of a failed tab as not added, and it must not run
// without its shell.
func TestAFailedTabClosesItsOwnTerminalsAlone(t *testing.T) {
	c, sock := newCore(t)
	h := &hc.Host{Socket: sock}
	ref, err := h.Open(t.Context(), revier.Realization{Name: "home", Launch: []string{"sh"}})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	c.failRun = 3
	c.runErr = map[string]any{"code": -32000, "message": "cannot start \"sh\": Permission denied (os error 13)", "data": map[string]string{"kind": "spawn_failed"}}
	_, err = h.OpenTab(t.Context(), ref, revier.Realization{Panels: []revier.PanelSpec{
		{Kind: revier.PanelAgent, Command: []string{"sh"}}, {Kind: revier.PanelShell, Command: []string{"sh"}},
	}}, nil)
	if err == nil || !strings.Contains(err.Error(), "Permission denied") {
		t.Fatalf("OpenTab = %v, want hc's spawn failure", err)
	}
	if live := c.live(); !slices.Equal(live, []string{"t1"}) {
		t.Errorf("live terminals = %q, want the first tab's t1 alone", live)
	}
}

// A tab opens in an instance of revier's on the core that made it: not in a
// terminal started by hand, not in an instance whose terminals are all gone,
// and not on a core started since.
func TestOpenTabRefusesWhatIsNoLiveInstance(t *testing.T) {
	c, sock := newCore(t)
	h := &hc.Host{Socket: sock}
	tab := revier.Realization{Launch: []string{"sh"}}
	c.add(&fakeTerminal{Address: "t4", State: "live"})
	for _, ref := range []revier.TargetRef{
		{Host: "hc", ID: "core1/t4"},
		home,
		{Host: "hc", ID: "core0/0a1b2c3d4e5f6a7b"},
	} {
		if _, err := h.OpenTab(t.Context(), ref, tab, nil); err == nil {
			t.Errorf("OpenTab in %s passed", ref.ID)
		}
	}
	if runs := c.paramsOf("terminal.run"); len(runs) != 0 {
		t.Errorf("ran %d terminals, want none", len(runs))
	}
	if _, err := h.OpenTab(t.Context(), home, tab, map[string]string{"revier.tab": "x"}); err == nil {
		t.Error("OpenTab took revier's own label as a var")
	}
}

// A view's window attaches one terminal by its address, with hc by its path
// and the socket named: the window starts in the view's environment, which
// need not name hc's socket at all.
func TestAttachPanelNamesTheSocketAndHcByItsPath(t *testing.T) {
	dir := hcLink(t)
	t.Setenv("PATH", dir)
	t.Setenv("HC_SOCKET", "/run/s.sock")
	got, err := (&hc.Host{}).AttachPanel("t7")
	if err != nil || !slices.Equal(got, []string{"env", "HC_SOCKET=/run/s.sock", filepath.Join(dir, "hc"), "attach", "t7"}) {
		t.Errorf("AttachPanel = %q, %v", got, err)
	}
	got, err = (&hc.Host{Socket: "/s.sock"}).AttachPanel("t7")
	if err != nil || got[1] != "HC_SOCKET=/s.sock" {
		t.Errorf("AttachPanel with a socket of its own = %q, %v", got, err)
	}
}
