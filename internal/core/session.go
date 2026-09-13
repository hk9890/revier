package core

import (
	"context"
	"fmt"

	"github.com/hk9890/revier/internal/session"
	"github.com/hk9890/revier/pkg/revier"
)

// RestoreAction is what restoring one recorded target comes to. Every value
// but RestoreLaunch is a normal outcome that the command reports and steps
// over: a machine two weeks after the save is not the machine that saved.
type RestoreAction uint8

const (
	// RestoreLaunch is the only action that does anything: run-or-raise.
	RestoreLaunch RestoreAction = iota
	// RestoreRunning is a target that is already up. Restore leaves it alone
	// rather than raising it, so a re-run does not walk focus across the
	// desktop for no reason.
	RestoreRunning
	// RestoreNoProject is a project file that is gone since the save.
	RestoreNoProject
	// RestoreNoTarget is a project that no longer declares the target.
	RestoreNoTarget
	// RestoreNoHost is a target no host on this machine can realize - a
	// window target on a headless box - which is ErrNoHost's outcome seen
	// from here.
	RestoreNoHost
)

var restoreNames = map[RestoreAction]string{
	RestoreLaunch:    "open",
	RestoreRunning:   "running",
	RestoreNoProject: "no such project",
	RestoreNoTarget:  "no such target",
	RestoreNoHost:    "no host for it here",
}

func (a RestoreAction) String() string { return restoreNames[a] }

// Resume is one agent panel of a target to start on a conversation rather
// than empty: the panel's position in the realization, which probe named the
// conversation, and the probe's own word for it.
type Resume struct {
	Index   int
	Harness string
	Session revier.SessionID
}

// RestoreStep is one recorded target and what restoring it means here.
type RestoreStep struct {
	Project revier.ProjectName
	Target  revier.TargetName
	Action  RestoreAction
	Resumes []Resume
}

// Session records what a survey found open, as the set that restoring opens
// again. It records names: which project, which target, and for an agent panel
// the conversation its probe can name. Everything else - the host that won,
// the instance, the argv - is derived again at restore, because a project file
// edited in between must win over this file.
//
// Attached instances are left out and cannot be otherwise: an attachment is a
// live id with no launch argv anywhere in the model, so there is nothing to
// record that would bring one back. The caller reports how many were dropped.
//
// Unnamed is the number of agent panels recorded without a conversation, so a
// save can say so while the agents are still running. A resume that cannot
// happen is otherwise found after the reboot, which is the worst moment.
// Failed holds why a probe could not answer at all - claude not on PATH - so
// those agents are not mistaken for the ones no listing could match. Neither
// fails the save.
func (c *Core) Session(ctx context.Context, r Report, current revier.ProjectName) (s session.Session, unnamed int, failed []error) {
	byRef := make(map[string]revier.Instance, len(r.Instances))
	for _, inst := range r.Instances {
		byRef[key(inst.Ref)] = inst
	}

	s = session.Session{Current: current}
	var agents []agentPanel
	for _, v := range r.Views {
		var targets []session.Target
		for _, tv := range v.Targets {
			// An attached instance has no name and no key, and so no way
			// back. A target with no live ref was not open.
			if tv.Attached || tv.Name == "" || tv.Ref.IsZero() {
				continue
			}
			targets = append(targets, session.Target{Name: tv.Name})
			inst, ok := byRef[key(tv.Ref)]
			if !ok {
				continue
			}
			for i, panel := range inst.Panels {
				if probe, ok := c.probeFor(panel); ok {
					agents = append(agents, agentPanel{
						project: len(s.Projects), target: len(targets) - 1,
						index: i, panel: panel, probe: probe,
					})
				}
			}
		}
		if len(targets) > 0 {
			s.Projects = append(s.Projects, session.Project{Name: v.Project.Name, Targets: targets})
		}
	}

	ids, failed := c.conversations(ctx, agents)
	for i, a := range agents {
		if ids[i] == "" {
			unnamed++
			continue
		}
		t := &s.Projects[a.project].Targets[a.target]
		t.Panels = append(t.Panels, session.Panel{Index: a.index, Harness: a.probe.Name(), Session: ids[i]})
	}
	return s, unnamed, failed
}

// agentPanel is one panel a probe claimed, and where its conversation goes in
// the session being recorded.
//
// index is the panel's position among all the instance's panels, which is the
// position of its spec in the realization: a runtime lays panels out in the
// order they are declared. Counting only some panels would need save and
// restore to agree on which ones count, and they cannot: save sees which panels
// a probe claims, restore sees which specs say kind = "agent", and a declared
// agent no probe claims makes the two counts disagree. A live panel's title is
// the agent's to rewrite - Claude Code replaces it with a summary of the turn -
// so a title is no identity at all here.
type agentPanel struct {
	project, target, index int
	panel                  revier.Panel
	probe                  revier.AgentProbe
}

// conversations asks each resumable probe once, for every panel it claimed
// across the whole save, and returns an id per agent panel in order. A panel
// whose probe cannot resume, cannot say, or failed gets an empty id: it
// restores empty, which is not a failure of the save; a probe that failed is
// returned with its error, named.
func (c *Core) conversations(ctx context.Context, agents []agentPanel) ([]revier.SessionID, []error) {
	var failed []error
	ids := make([]revier.SessionID, len(agents))
	// Grouped by name, the identity a restore finds a probe by. A probe is an
	// interface value, and one whose dynamic type is not comparable panics as
	// a map key.
	byProbe := map[string][]int{}
	var order []string
	for i, a := range agents {
		if _, ok := a.probe.(revier.Resumable); !ok {
			continue
		}
		name := a.probe.Name()
		if _, seen := byProbe[name]; !seen {
			order = append(order, name)
		}
		byProbe[name] = append(byProbe[name], i)
	}
	for _, name := range order {
		at := byProbe[name]
		panels := make([]revier.Panel, len(at))
		for j, i := range at {
			panels[j] = agents[i].panel
		}
		named, err := agents[at[0]].probe.(revier.Resumable).Sessions(ctx, panels)
		if err == nil && len(named) != len(panels) {
			err = fmt.Errorf("answered %d panels of %d", len(named), len(panels))
		}
		if err != nil {
			failed = append(failed, fmt.Errorf("%s: %w", name, err))
			continue
		}
		for j, i := range at {
			ids[i] = named[j]
		}
	}
	return ids, failed
}

// RestorePlan decides what each recorded target means on this machine now. It
// is the whole of restore's policy; the caller walks the plan and calls Go for
// every RestoreLaunch, in order.
//
// Order is the file's, and the caller keeps it: a launch is bound to the
// window that appears after it (decisions.md D21), so two launches at once are
// two windows neither can be attributed to.
func (c *Core) RestorePlan(s session.Session, r Report) []RestoreStep {
	views := make(map[revier.ProjectName]revier.ProjectView, len(r.Views))
	for _, v := range r.Views {
		views[v.Project.Name] = v
	}

	var plan []RestoreStep
	for _, p := range s.Projects {
		v, known := views[p.Name]
		for _, t := range p.Targets {
			step := RestoreStep{Project: p.Name, Target: t.Name}
			switch {
			case !known:
				step.Action = RestoreNoProject
			default:
				tv, declared := targetView(v, t.Name)
				switch {
				case !declared:
					step.Action = RestoreNoTarget
				case !tv.Available:
					step.Action = RestoreNoHost
				case !tv.Ref.IsZero():
					step.Action = RestoreRunning
				default:
					step.Action = RestoreLaunch
					step.Resumes = resumesOf(t)
				}
			}
			plan = append(plan, step)
		}
	}
	return plan
}

func targetView(v revier.ProjectView, name revier.TargetName) (revier.TargetView, bool) {
	for _, tv := range v.Targets {
		if !tv.Attached && tv.Name == name {
			return tv, true
		}
	}
	return revier.TargetView{}, false
}

func resumesOf(t session.Target) []Resume {
	var out []Resume
	for _, p := range t.Panels {
		out = append(out, Resume{Index: p.Index, Harness: p.Harness, Session: p.Session})
	}
	return out
}

// resuming returns the realization with its agent panels started on the
// conversations they held. A resume that nothing here can honour - a probe
// this machine does not run, one without the capability, a position the
// project no longer declares as an agent - is dropped, and that panel starts
// empty. The agent check is what keeps a layout edited since the save from
// typing a resume flag into a shell.
//
// The panel slice is copied before anything is written to it: the realization
// arrives sharing the prepared project's panels, and a restore must not edit
// the project every later keypress reads.
func (c *Core) resuming(real revier.Realization, resumes []Resume) revier.Realization {
	var panels []revier.PanelSpec
	for _, r := range resumes {
		if r.Index < 0 || r.Index >= len(real.Panels) || real.Panels[r.Index].Kind != revier.PanelAgent {
			continue
		}
		probe, ok := c.probeNamed(r.Harness)
		if !ok {
			continue
		}
		res, ok := probe.(revier.Resumable)
		if !ok {
			continue
		}
		if panels == nil {
			panels = append([]revier.PanelSpec(nil), real.Panels...)
		}
		panels[r.Index].Command = res.ResumeCommand(panels[r.Index], r.Session)
	}
	if panels != nil {
		real.Panels = panels
	}
	return real
}

func (c *Core) probeNamed(name string) (revier.AgentProbe, bool) {
	for _, probe := range c.Probes {
		if probe.Name() == name {
			return probe, true
		}
	}
	return nil, false
}
