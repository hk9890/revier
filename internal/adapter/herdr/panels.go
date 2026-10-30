package herdr

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/hk9890/revier/internal/panelstart"
	"github.com/hk9890/revier/pkg/revier"
)

// maxTokens is the most tokens one pane.report_metadata sets.
const maxTokens = 16

// OpenTab opens a tab after the ones the workspace holds: r.Launch alone, or
// r.Panels side by side. vars go on every pane of it as tokens, which
// Instances reports back as the panel's Vars. The tab opens without the
// focus; the core focuses the panel it wants.
//
// A tab's vars are its whole identity (decisions.md D64): a tab that carries
// none, or carries one herdr changed, is found by nothing, and the next press
// opens another. So a var herdr would not keep as written refuses the tab
// before it opens, and a tab that fails is closed whole: the core names its
// agent as not added, and it must not be running without its shell.
func (h *Host) OpenTab(ctx context.Context, ref revier.TargetRef, r revier.Realization, vars map[string]string) (revier.PanelID, error) {
	panels := r.PanelSpecs()
	if err := keeps(vars); err != nil {
		return "", err
	}
	if err := buildable(panels); err != nil {
		return "", err
	}
	workspace, err := h.current(ctx, ref)
	if err != nil {
		return "", err
	}
	before, err := h.tabs(ctx, workspace)
	if err != nil {
		return "", err
	}

	tab, panes, err := h.apply(ctx, "workspace_id", workspace, panels)
	if err == nil {
		for i, id := range panes {
			if err = h.mark(ctx, id, vars); err != nil {
				// A command that ends just after the check apply makes is
				// gone by its mark.
				if isCode(err, "pane_not_found") {
					err = panelstart.ExitedAtOnce(panels[i])
				}
				break
			}
		}
	}
	if err != nil {
		// The close has a bound of its own: the end of ctx may be what failed
		// the tab.
		closing, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if tab == "" {
			// herdr may have made the tab and its answer been lost, the end
			// of ctx while it was read. A tab of the workspace that was not
			// there before is then that one: unmarked, nothing would find it.
			if now, err := h.tabs(closing, workspace); err == nil {
				for id := range now {
					if !before[id] {
						tab = id
					}
				}
			}
		}
		if tab != "" {
			_, _ = h.request(closing, "tab.close", map[string]string{"tab_id": tab}, nil)
		}
		return "", err
	}
	return revier.PanelID(panes[0]), nil
}

// tabs is the set of the workspace's tabs.
func (h *Host) tabs(ctx context.Context, workspace string) (map[string]bool, error) {
	_, snap, err := h.snapshot(ctx)
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, l := range snap.Layouts {
		if l.WorkspaceID == workspace {
			out[l.TabID] = true
		}
	}
	return out, nil
}

// keeps refuses vars herdr would not keep as written as tokens.
func keeps(vars map[string]string) error {
	if len(vars) > maxTokens {
		return fmt.Errorf("herdr: a tab carries at most %d vars, and this one has %d", maxTokens, len(vars))
	}
	for k, v := range vars {
		if !validKey(k) {
			return fmt.Errorf("herdr: var %q is not a token name herdr takes: up to 32 ASCII letters, digits, '_' and '-'", k)
		}
		if !survives(v) {
			return fmt.Errorf("herdr: var %s=%q is not kept as written: herdr trims a value, drops its control characters and cuts it at 80 characters", k, v)
		}
	}
	return nil
}

// apply builds the panels side by side, without moving the focus, in a new
// tab after the tabs of the workspace a "workspace_id" key names, or in the
// tab a "tab_id" key names, which they replace. It returns the tab and its
// panes, left to right, and fails when a panel's command exited as soon as it
// started. The tab is returned with a failure too, once herdr made it, for
// the caller to close.
func (h *Host) apply(ctx context.Context, key, id string, panels []revier.PanelSpec) (string, []string, error) {
	params := map[string]any{key: id, "focus": false, "root": layout(panels)}
	var applied struct {
		Layout struct {
			TabID string     `json:"tab_id"`
			Root  layoutNode `json:"root"`
		} `json:"layout"`
	}
	if _, err := h.request(ctx, "layout.apply", params, &applied); err != nil {
		return "", nil, err
	}
	tab, panes := applied.Layout.TabID, applied.Layout.Root.panes()
	if len(panes) != len(panels) {
		return tab, nil, fmt.Errorf("herdr layout.apply: answered with %d panes for %d panels", len(panes), len(panels))
	}

	_, snap, err := h.snapshot(ctx)
	if err != nil {
		return tab, nil, err
	}
	for i, id := range panes {
		if !slices.ContainsFunc(snap.Panes, func(p pane) bool { return p.ID == id }) {
			return tab, nil, panelstart.ExitedAtOnce(panels[i])
		}
	}
	return tab, panes, nil
}

// FocusPanel makes the pane current in its tab and its tab current in the
// workspace. Like every herdr focus, it moves each attached terminal.
func (h *Host) FocusPanel(ctx context.Context, ref revier.TargetRef, panel revier.PanelID) error {
	if _, err := h.current(ctx, ref); err != nil {
		return err
	}
	_, err := h.request(ctx, "pane.focus", map[string]string{"pane_id": panel.String()}, nil)
	return err
}

// FocusedPanel reports the focused pane of the workspace's active tab, which
// herdr keeps for a workspace that is not shown too.
func (h *Host) FocusedPanel(ctx context.Context, ref revier.TargetRef) (revier.PanelID, error) {
	if err := h.own(ref); err != nil {
		return "", err
	}
	pid, snap, err := h.snapshot(ctx)
	if err != nil {
		return "", err
	}
	id, err := workspaceAt(ref, pid)
	if err != nil {
		return "", err
	}
	i := slices.IndexFunc(snap.Workspaces, func(w workspace) bool { return w.ID == id })
	if i < 0 {
		return "", fmt.Errorf("herdr: workspace %s is gone", ref.ID)
	}
	tab := snap.Workspaces[i].ActiveTabID
	for _, l := range snap.Layouts {
		if l.TabID == tab {
			return revier.PanelID(l.FocusedPaneID), nil
		}
	}
	return "", fmt.Errorf("herdr: tab %s of workspace %s has no layout", tab, ref.ID)
}

// SendText types text into the pane as given: herdr adds no key, so a submit
// is a "\r" of its own.
func (h *Host) SendText(ctx context.Context, ref revier.TargetRef, panel revier.PanelID, text string) error {
	if _, err := h.current(ctx, ref); err != nil {
		return err
	}
	_, err := h.request(ctx, "pane.send_text", map[string]string{"pane_id": panel.String(), "text": text}, nil)
	return err
}

// maxReadLines is the most lines herdr reads from a pane at once.
const maxReadLines = 1000

// ReadPanel is what the pane shows, with the colour of each run as SGR
// sequences, which is all of the terminal herdr's ansi read carries; with
// scrollback, the last 1000 lines, the most herdr reads at once. herdr puts
// "\r\n" between two lines and nothing after the last; here a "\n" goes
// between them, the line end tmux uses. herdr's screen follows the pane's
// scroll: while the user has scrolled a pane up in herdr, the screen read is
// the lines shown there, not the newest, and herdr reads no other.
func (h *Host) ReadPanel(ctx context.Context, ref revier.TargetRef, panel revier.PanelID, scrollback bool) (string, error) {
	if _, err := h.current(ctx, ref); err != nil {
		return "", err
	}
	params := map[string]any{"pane_id": panel.String(), "source": "visible", "format": "ansi"}
	if scrollback {
		params["source"], params["lines"] = "recent", maxReadLines
	}
	var res struct {
		Read struct {
			Text string `json:"text"`
		} `json:"read"`
	}
	if _, err := h.request(ctx, "pane.read", params, &res); err != nil {
		return "", err
	}
	return strings.ReplaceAll(res.Read.Text, "\r\n", "\n"), nil
}

// ClosePanel closes one pane; a tab left with no pane goes with it, and a
// workspace with its last tab.
func (h *Host) ClosePanel(ctx context.Context, ref revier.TargetRef, panel revier.PanelID) error {
	if _, err := h.current(ctx, ref); err != nil {
		return err
	}
	_, err := h.request(ctx, "pane.close", map[string]string{"pane_id": panel.String()}, nil)
	return err
}
