package core

import (
	"context"
	"fmt"
	"time"

	"github.com/hk9890/revier/internal/logging"
	"github.com/hk9890/revier/pkg/revier"
)

// GoAgent brings the agent in a panel of a project to the front. On this
// machine its tab becomes current and the window holding it is raised. For a
// link the agent is the host's: the host makes its tab current in the
// workspace the pane onto it shows, and the pane is raised here, so Result
// names the home it landed on.
func (c *Core) GoAgent(ctx context.Context, p Project, panel revier.PanelID, bound Bindings) (res Result, err error) {
	start := time.Now()
	defer func() { logging.Op("go agent", start, err, "project", p.Name, "panel", panel) }()
	r, address, err := c.RemoteAt(p, panel.String())
	if err != nil {
		return Result{}, err
	}
	if r != nil {
		if err := r.FocusAgent(ctx, address); err != nil {
			return Result{}, fmt.Errorf("%s: focus agent %s: %w", r.Name(), address, err)
		}
		home, ok := p.Home()
		if !ok {
			return Result{}, nil
		}
		return c.Go(ctx, p, home.Name, bound)
	}
	a, err := c.Agent(ctx, p, panel.String(), bound)
	if err != nil {
		return Result{}, err
	}
	return Result{}, c.FocusAgent(ctx, a)
}

// FocusAgent makes an agent's tab current in the instance that holds it and
// raises that instance's window. A runtime that cannot focus a panel focuses
// the instance, which is as near the agent as it can bring the user.
func (c *Core) FocusAgent(ctx context.Context, a Agent) error {
	snap, err := c.snapshot(ctx)
	if err != nil {
		return err
	}
	inst, _ := byRef(snap, a.Ref)
	name := revier.TargetName("agent " + a.Panel.ID.String())
	osw, err := c.raisable(snap, inst, name)
	if err != nil {
		return err
	}
	if opener, ok := c.Runtime.(revier.PanelOpener); ok && a.Ref.Host == c.Runtime.Name() {
		if err := opener.FocusPanel(ctx, a.Ref, a.Panel.ID); err != nil {
			return fmt.Errorf("%s: focus %s: %w", c.Runtime.Name(), name, err)
		}
	} else if err := c.Focus(ctx, a.Ref); err != nil {
		return err
	}
	return c.raise(ctx, osw, name)
}
