package core

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/hk9890/revier/internal/logging"
	"github.com/hk9890/revier/internal/record"
	"github.com/hk9890/revier/pkg/revier"
)

// goAgent brings an agent the survey reported to the front: its tab becomes
// current and the window holding it is raised. A link's agent is reached the
// same way, in the panel here that shows it (decisions.md D84). One that no
// panel here shows - a link's started from another machine, or one this
// machine serves to a terminal elsewhere - is ErrAgentElsewhere.
//
// The agent is named by its instance and its panel, as the survey saw it, and
// not looked up again by panel id: a panel id is one process's, and two kitty
// processes of one project can each hold a panel 1 (D75).
func (c *Core) goAgent(ctx context.Context, p Project, a revier.AgentView) (res Result, err error) {
	start := time.Now()
	defer func() { logging.Op("go agent", start, err, "project", p.Name, "ref", a.Ref, "panel", a.Panel) }()
	if !c.here(a) {
		return Result{}, fmt.Errorf("agent %s: %w", a.Panel, ErrAgentElsewhere)
	}
	return Result{}, c.FocusAgent(ctx, p.Name, a)
}

// FocusAgent focuses the instance that holds the panel, makes the panel
// current in it and raises that instance's window. The instance comes first:
// a panel current in a tmux session is seen only by a terminal that shows the
// session, and focusing the session is what switches one to it. A runtime that
// cannot focus a panel stops at the instance, which is as near the agent as it
// can bring the user. An instance or a panel gone since it was reported is
// ErrAgentGone.
//
// An agent that was reached is one EventGoAgent of the project, recorded here
// and nowhere else, so every way to an agent records it; the state record has
// the focus failed or not.
func (c *Core) FocusAgent(ctx context.Context, project revier.ProjectName, a revier.AgentView) error {
	err := c.focusAgent(ctx, a.Ref, a.Panel)
	if err == nil {
		c.record(revier.Event{Kind: revier.EventGoAgent, Project: project, Agent: a.State.Harness, Session: a.State.Session})
	}
	c.note(record.Line{
		Kind: record.KindGoAgent, Project: project, Host: a.Ref.Host, Ref: a.Ref.ID, Panel: a.Panel,
		Harness: a.State.Harness, Session: a.State.Session,
	}.Failed(err))
	return err
}

func (c *Core) focusAgent(ctx context.Context, ref revier.TargetRef, panel revier.PanelID) error {
	snap, err := c.answered(ctx, ref.Host)
	if err != nil {
		return err
	}
	return c.focusPanel(ctx, snap, ref, panel, revier.TargetName("agent "+panel.String()))
}

// focusPanel focuses the instance, makes the panel current in it, and raises
// the instance's window, on a listing already taken. name is what a failure
// calls the panel.
func (c *Core) focusPanel(ctx context.Context, snap snapshot, ref revier.TargetRef, panel revier.PanelID, name revier.TargetName) error {
	inst, ok := byRef(snap, ref)
	if !ok || !slices.ContainsFunc(inst.Panels, func(p revier.Panel) bool { return p.ID == panel }) {
		return fmt.Errorf("%s: %w", name, ErrAgentGone)
	}
	osw, err := c.raisable(snap, inst, name)
	if err != nil {
		return err
	}
	if err := c.focus(ctx, ref); err != nil {
		return err
	}
	if opener, ok := c.Runtime.(revier.PanelOpener); ok && ref.Host == c.Runtime.Name() {
		if err := opener.FocusPanel(ctx, ref, panel); err != nil {
			return fmt.Errorf("%s: focus %s: %w", c.Runtime.Name(), name, err)
		}
	}
	return c.raise(ctx, osw, name)
}
