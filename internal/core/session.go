package core

import (
	"context"

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
// than empty: which agent panel, which probe named the conversation, and the
// probe's own word for it.
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
func (c *Core) Session(ctx context.Context, r Report, current revier.ProjectName) session.Session {
	byRef := make(map[string]revier.Instance, len(r.Instances))
	for _, inst := range r.Instances {
		byRef[key(inst.Ref)] = inst
	}

	s := session.Session{Current: current}
	for _, v := range r.Views {
		var targets []session.Target
		for _, tv := range v.Targets {
			// An attached instance has no name and no key, and so no way
			// back. A target with no live ref was not open.
			if tv.Attached || tv.Name == "" || tv.Ref.IsZero() {
				continue
			}
			t := session.Target{Name: tv.Name}
			if inst, ok := byRef[key(tv.Ref)]; ok {
				t.Panels = c.conversations(ctx, inst)
			}
			targets = append(targets, t)
		}
		if len(targets) > 0 {
			s.Projects = append(s.Projects, session.Project{Name: v.Project.Name, Targets: targets})
		}
	}
	return s
}

// conversations reads the agent panels of an instance and records the ones
// whose probe can name what they hold.
//
// The index recorded is the panel's position among the instance's agent
// panels, not among all its panels, because that is the identity restore can
// act on: it maps to the nth agent panel of the realization. A live panel's
// title is the agent's to rewrite - Claude Code replaces it with a summary of
// the turn - so a title is no identity at all here.
func (c *Core) conversations(ctx context.Context, inst revier.Instance) []session.Panel {
	var out []session.Panel
	nth := 0
	for _, panel := range inst.Panels {
		probe, ok := c.probeFor(panel)
		if !ok {
			continue
		}
		i := nth
		nth++
		res, ok := probe.(revier.Resumable)
		if !ok {
			continue
		}
		id, held, err := res.Session(ctx, panel)
		if err != nil || !held {
			// A probe that cannot say is not a failure: the panel restores
			// empty, which is what a probe without the capability does too.
			continue
		}
		out = append(out, session.Panel{Index: i, Harness: probe.Name(), Session: id})
	}
	return out
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
// this machine does not run, one without the capability, an agent panel the
// project no longer declares - is dropped, and that panel starts empty.
//
// The panel slice is copied before anything is written to it: the realization
// arrives sharing the prepared project's panels, and a restore must not edit
// the project every later keypress reads.
func (c *Core) resuming(real revier.Realization, resumes []Resume) revier.Realization {
	if len(resumes) == 0 {
		return real
	}
	var agents []int
	for i, spec := range real.Panels {
		if spec.Kind == revier.PanelAgent {
			agents = append(agents, i)
		}
	}

	var panels []revier.PanelSpec
	for _, r := range resumes {
		if r.Index >= len(agents) {
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
		i := agents[r.Index]
		panels[i].Command = res.ResumeCommand(panels[i], r.Session)
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
