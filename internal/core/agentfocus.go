package core

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/hk9890/revier/internal/logging"
	"github.com/hk9890/revier/pkg/revier"
)

// GoAgent brings an agent the survey reported to the front. On this machine
// its tab becomes current and the window holding it is raised. For a link the
// agent is the host's: the host makes its tab current in the workspace the
// pane onto it shows, and the pane is raised here, so Result names the home it
// landed on.
//
// The agent is named by its instance and its panel, as the survey saw it, and
// not looked up again by panel id: a panel id is one process's, and two kitty
// processes of one project can each hold a panel 1 (decisions.md D75).
func (c *Core) GoAgent(ctx context.Context, p Project, a revier.AgentView, bound Bindings) (res Result, err error) {
	start := time.Now()
	defer func() { logging.Op("go agent", start, err, "project", p.Name, "ref", a.Ref, "panel", a.Panel) }()
	r, address, err := c.RemoteAt(p, a.Panel.String())
	if err != nil {
		return Result{}, err
	}
	if r != nil {
		if err := r.FocusAgent(ctx, address, a.Ref); err != nil {
			return Result{}, fmt.Errorf("%s: focus agent %s: %w", r.Name(), address, err)
		}
		home, ok := p.Home()
		if !ok {
			return Result{}, nil
		}
		return c.Go(ctx, p, home.Name, bound)
	}
	return Result{}, c.FocusAgent(ctx, a.Ref, a.Panel)
}

// FocusAgent makes the panel current in the instance that holds it and raises
// that instance's window. A runtime that cannot focus a panel focuses the
// instance, which is as near the agent as it can bring the user. An instance
// or a panel gone since it was reported is ErrAgentGone.
func (c *Core) FocusAgent(ctx context.Context, ref revier.TargetRef, panel revier.PanelID) error {
	snap, err := c.snapshot(ctx)
	if err != nil {
		return err
	}
	name := revier.TargetName("agent " + panel.String())
	inst, ok := byRef(snap, ref)
	if !ok || !slices.ContainsFunc(inst.Panels, func(p revier.Panel) bool { return p.ID == panel }) {
		return fmt.Errorf("%s: %w", name, ErrAgentGone)
	}
	osw, err := c.raisable(snap, inst, name)
	if err != nil {
		return err
	}
	if opener, ok := c.Runtime.(revier.PanelOpener); ok && ref.Host == c.Runtime.Name() {
		if err := opener.FocusPanel(ctx, ref, panel); err != nil {
			return fmt.Errorf("%s: focus %s: %w", c.Runtime.Name(), name, err)
		}
	} else if err := c.Focus(ctx, ref); err != nil {
		return err
	}
	return c.raise(ctx, osw, name)
}
