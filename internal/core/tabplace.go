package core

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/hk9890/revier/internal/logging"
	"github.com/hk9890/revier/internal/panelstart"
	"github.com/hk9890/revier/pkg/revier"
)

// TabRequest is the tab `revier agent new` or `revier shell new` asks for.
type TabRequest struct {
	// Kind is revier.PanelAgent or revier.PanelShell.
	Kind revier.PanelKind
	// Project is the project -p names, and Target the target after its
	// colon. Without Target an agent tab opens in AgentTarget's, and a shell
	// tab in the home target.
	Project revier.ProjectName
	Target  revier.TargetName
	// Panel is the panel a terminal key was pressed in.
	Panel revier.PanelID
	// Dir is the directory the tab starts in, or "".
	Dir string
	// Cwd is the command's working directory, which the core does not read:
	// without Project, Panel and Dir the tab opens in the project it is in.
	Cwd string
	// Resume is how an agent tab's agent starts. PlaceTab sets its Dir.
	Resume Resume
	// NoFocus opens an agent tab and leaves the focus where it is: AddAgent,
	// not NewAgent.
	NoFocus bool
	// Outside is what a Panel no project's workspace holds gets.
	Outside OutsideKey
}

// PlacedTab is what PlaceTab did.
type PlacedTab struct {
	// Outside is a Panel that no project's open workspace holds: the tab
	// Outside asked for opened beside it, or nothing did.
	Outside bool
	// Outcome is what an agent tab's agent came to.
	Outcome AgentOutcome
	// Address is, with NoFocus, the address of the new agent, also when the
	// tab opened and then failed; "" while no tab opened.
	Address string
}

// PlaceTab opens the tab req asks for in the open workspace the core places
// it in. The place is the owner of Panel; or the project req names, else the
// one Dir is in, or without Dir the one Cwd is in, with req's target or the
// kind's. A Panel that no project's workspace holds gets what Outside says,
// with Outside.Agent the named agent an agent tab asks for. With a Panel and
// no Dir, a shell tab's directory is the panel's own (decisions.md D153), and
// an agent tab's is the project's (D158). Outside a workspace, with outside =
// "agent", an agent tab starts in the panel's directory (D164).
func (c *Core) PlaceTab(ctx context.Context, projects []Project, req TabRequest) (PlacedTab, error) {
	if req.Panel == "" {
		w, err := c.tabWorkspace(ctx, projects, req)
		if err != nil {
			return PlacedTab{}, err
		}
		return c.openPlaced(ctx, projects, w, req, req.Dir)
	}
	w, err := c.PanelOwner(ctx, projects, req.Panel)
	if errors.Is(err, ErrNoWorkspace) {
		return PlacedTab{Outside: true}, c.tabOutside(ctx, w, req)
	}
	if err != nil {
		return PlacedTab{}, err
	}
	return c.openPlaced(ctx, projects, w, req, panelTabDir(w, req, false))
}

// tabWorkspace is the open workspace a tab asked for with no Panel opens in.
// Its project is never the focused window's or the last one: a tab is asked
// for from a terminal or a script, which has a directory, so one in no
// project fails (decisions.md D160).
func (c *Core) tabWorkspace(ctx context.Context, projects []Project, req TabRequest) (Workspace, error) {
	p, err := c.ProjectFor(ctx, projects, Ask{Name: req.Project, Dir: cmp.Or(req.Dir, req.Cwd)})
	if err != nil {
		return Workspace{}, err
	}
	target := req.Target
	if target == "" {
		if req.Kind == revier.PanelAgent {
			target, err = c.AgentTarget(p)
		} else {
			target, err = p.homeTarget()
		}
		if err != nil {
			return Workspace{}, err
		}
	}
	return c.AgentWorkspace(ctx, p, target)
}

// homeTarget is the target `revier shell new -p <project>` opens its tab in.
// A project without one is loaded and listed all the same (decisions.md D85),
// and is refused here with its reason.
func (p Project) homeTarget() (revier.TargetName, error) {
	home, ok := p.Home()
	if !ok || home.Name == "" {
		if p.Invalid != nil {
			return "", p.Invalid
		}
		return "", fmt.Errorf("project %q has no home target", p.Name)
	}
	return home.Name, nil
}

// panelTabDir is the directory a tab asked for at req's panel in w starts
// in: Dir, else for a shell tab, and for every tab outside a workspace, the
// panel's own; each only while it is a directory. A key passes the directory
// its terminal names for the panel, which can be one removed since.
func panelTabDir(w Workspace, req TabRequest, outside bool) string {
	if dirExists(req.Dir) {
		return req.Dir
	}
	if outside || req.Kind != revier.PanelAgent {
		return panelDir(w, req.Panel)
	}
	return ""
}

// openPlaced opens the tab req asks for in the workspace w, started in dir,
// else where the project's tab starts. dir is a path on this machine, so a
// link, whose tab starts on its host, drops it. With a Panel, a directory
// outside the project that holds it is dropped too: a panel's shell can sit
// anywhere.
func (c *Core) openPlaced(ctx context.Context, projects []Project, w Workspace, req TabRequest, dir string) (PlacedTab, error) {
	op := "shell new"
	if req.Kind == revier.PanelAgent {
		op = "agent new"
	}
	if w.Project.Remote != nil {
		dir = ""
	}
	if req.Panel != "" && dir != "" {
		if p, ok := projectAt(projects, dir); !ok || p.Name != w.Project.Name {
			slog.Warn(op, "err", "the directory is outside the project of --panel; the tab opens in the project", "project", w.Project.Name, "dir", dir)
			dir = ""
		}
	}
	if dir != "" && !dirExists(dir) {
		return PlacedTab{}, fmt.Errorf("--dir %s: not a directory", dir)
	}
	start := time.Now()
	if req.Kind != revier.PanelAgent {
		err := c.NewShell(ctx, w, dir)
		logging.Op(op, start, err, "project", w.Project.Name, "target", w.Target, "ref", w.Ref, "dir", dir)
		return PlacedTab{}, err
	}
	r := req.Resume
	r.Dir = dir
	var placed PlacedTab
	var err error
	if req.NoFocus {
		var panel revier.PanelID
		panel, placed.Outcome, err = c.AddAgent(ctx, w, r)
		if panel != "" {
			placed.Address = c.AddressIn(w, panel)
		}
	} else {
		placed.Outcome, err = c.NewAgent(ctx, w, r)
	}
	logging.Op(op, start, err, "project", w.Project.Name, "target", w.Target, "ref", w.Ref, "agent", r.Agent, "session", r.Session, "dir", dir, "outcome", placed.Outcome.String())
	return placed, err
}

// tabOutside is the tab a terminal key asks for in a panel the runtime holds
// in the instance w, which no project's open workspace holds. The key is not
// refused for that: it does what req.Outside says, a plain tab of the
// runtime's own shell beside the panel, a tab of the agent an agent key asks
// for, or nothing. Every other failure is returned, so the key's own
// fallback runs (decisions.md D148, D164); so is an agent that cannot run
// there, as OutsideKey's agent says.
func (c *Core) tabOutside(ctx context.Context, w Workspace, req TabRequest) error {
	dir, outside := panelTabDir(w, req, true), req.Outside
	slog.Info("tab outside a workspace", "panel", req.Panel, "ref", w.Ref, "outside", string(outside.Outside), "kind", string(req.Kind), "agent", outside.Agent, "dir", dir)
	if outside.Outside == OutsideNone {
		return nil
	}
	opener, ok := c.Runtime.(revier.PanelOpener)
	if !ok {
		return fmt.Errorf("%s: %w", c.Runtime.Name(), ErrNoTabsToOpen)
	}
	spec, what := revier.PanelSpec{Kind: revier.PanelShell, Dir: dir}, "plain tab"
	if outside.Outside == OutsideAgent && req.Kind == revier.PanelAgent {
		agent, ok, err := outside.agent()
		if err != nil {
			return err
		}
		if ok {
			spec, what = revier.PanelSpec{Kind: revier.PanelAgent, Title: outside.Agent, Command: agent.Command, Dir: dir}, "agent tab"
		} else {
			slog.Warn("tab outside a workspace", "err", "config.toml declares no [[agent]] to run; the tab is a plain shell")
		}
	}
	tab := revier.Realization{Dir: dir, Panels: []revier.PanelSpec{spec}}
	opened, err := opener.OpenTab(ctx, w.Ref, tab, nil)
	if err != nil {
		return fmt.Errorf("%s: %s: %w", c.Runtime.Name(), what, err)
	}
	if err := opener.FocusPanel(ctx, w.Ref, opened); err != nil {
		if ended(ctx, c.Runtime, w.Ref, opened, tab.Panels) {
			return fmt.Errorf("%s: %s: %w", c.Runtime.Name(), what, panelstart.ExitedAtOnce(spec))
		}
		return fmt.Errorf("%s: focus the %s: %w", c.Runtime.Name(), what, err)
	}
	return nil
}
