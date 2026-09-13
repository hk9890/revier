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

// Resume is one recorded agent of a target: which probe claimed it, the
// probe's own word for its conversation, and the directory it worked in.
// Either of the last two may be empty.
type Resume struct {
	Harness string
	Session revier.SessionID
	Dir     string
}

// AgentOutcome is what restoring one recorded agent comes to.
type AgentOutcome uint8

const (
	// AgentResumed starts on the conversation it held, in its directory.
	AgentResumed AgentOutcome = iota
	// AgentEmpty starts with no conversation: none was recorded, or nothing
	// here can resume it.
	AgentEmpty
	// AgentDirGone starts empty in the project, because the directory it
	// worked in is gone - a worktree removed since the save. Resuming it
	// anywhere else would carry the conversation on in the wrong checkout.
	AgentDirGone
	// AgentDropped is not restored: the target declares no agent panel to
	// start it from, or it is past the declared ones and the runtime cannot
	// open a tab in an open instance.
	AgentDropped
	// AgentNotAdded is past the declared ones, and its tab failed to open
	// after the workspace itself opened. Result.AgentErr says why.
	AgentNotAdded
)

// RestoreStep is one recorded target and what restoring it means here.
type RestoreStep struct {
	Project revier.ProjectName
	Target  revier.TargetName
	Action  RestoreAction
	Resumes []Resume
}

// Session records what a survey found open, as the set that restoring opens
// again. It records names: which project, which target, and for each agent the
// conversation and directory its probe can name. Everything else - the host
// that won, the instance, the argv - is derived again at restore, because a
// project file edited in between must win over this file.
//
// Every agent a probe claims is recorded, in the order its runtime lists it,
// with or without a conversation: the order is what a restore lays the agents
// out by, and an agent left out would move every agent after it.
//
// Attached instances are left out and cannot be otherwise: an attachment is a
// live id with no launch argv anywhere in the model, so there is nothing to
// record that would bring one back. The caller reports how many were dropped.
//
// What the save could not record is returned beside it, so a save can say so
// while the agents are still running. A resume that cannot happen is
// otherwise found after the reboot, which is the worst moment. None of it
// fails the save.
func (c *Core) Session(ctx context.Context, r Report, current revier.ProjectName) (session.Session, SessionGaps) {
	byRef := make(map[string]revier.Instance, len(r.Instances))
	for _, inst := range r.Instances {
		byRef[key(inst.Ref)] = inst
	}

	s := session.Session{Current: current}
	var gaps SessionGaps
	var agents []agentPanel
	for _, v := range r.Views {
		var targets, tabs []session.Target
		var tabAgents []agentPanel
		for _, tv := range v.Targets {
			// An attached instance has no name and no key, and so no way
			// back. A target with no live ref was not open.
			if tv.Attached || tv.Name == "" || tv.Ref.IsZero() {
				continue
			}
			inst, listed := byRef[key(tv.Ref)]
			// A tab's ref is the instance that holds it. Its own panel is
			// recorded under the tab, and the tab after the other targets: a
			// restore that reached the tab first would open the instance for
			// it with none of its agents resumed.
			if t, ok := v.Project.Target(tv.Name); ok && tabTarget(t) {
				tabs = append(tabs, session.Target{Name: tv.Name})
				id, open := tabOf(inst, tv.Name)
				for _, panel := range inst.Panels {
					if !open || panel.ID != id {
						continue
					}
					if probe, ok := c.probeFor(panel); ok {
						tabAgents = append(tabAgents, agentPanel{project: len(s.Projects), target: len(tabs) - 1, panel: panel, probe: probe})
					}
				}
				continue
			}
			targets = append(targets, session.Target{Name: tv.Name})
			if !listed {
				continue
			}
			for _, panel := range inst.Panels {
				// A tab's panel is its tab target's, and a restore of the
				// instance would otherwise open it a second time.
				if recordedAsTab(v, inst, panel) {
					continue
				}
				if probe, ok := c.probeFor(panel); ok {
					agents = append(agents, agentPanel{
						project: len(s.Projects), target: len(targets) - 1,
						panel: panel, probe: probe,
					})
				}
			}
		}
		for _, a := range tabAgents {
			a.target += len(targets)
			agents = append(agents, a)
		}
		targets = append(targets, tabs...)
		if len(targets) > 0 {
			s.Projects = append(s.Projects, session.Project{Name: v.Project.Name, Targets: targets})
		}
	}

	named, failed := c.conversations(ctx, agents)
	gaps.Failed = failed
	for i, a := range agents {
		if named[i].ID == "" {
			gaps.Unnamed++
		}
		t := &s.Projects[a.project].Targets[a.target]
		t.Agents = append(t.Agents, session.Agent{Harness: a.probe.Name(), Session: named[i].ID, Dir: named[i].Dir})
	}
	return s, gaps
}

// recordedAsTab reports whether the panel is the one an open tab target of the
// view records as its own. A panel that names a target no longer a tab, or a
// tab open in another instance, stays with its instance, so its agent is not
// left out of the save.
func recordedAsTab(v revier.ProjectView, inst revier.Instance, panel revier.Panel) bool {
	name := revier.TargetName(panel.Vars[PanelTargetVar])
	if t, ok := v.Project.Target(name); !ok || !tabTarget(t) {
		return false
	}
	tv, _ := targetView(v, name)
	id, _ := tabOf(inst, name)
	return key(tv.Ref) == key(inst.Ref) && id == panel.ID
}

// SessionGaps is what a save found open and could not record. Unnamed is the
// agents recorded without a conversation. Failed holds why a probe could not
// answer at all - claude not on PATH - so those agents are not mistaken for
// the ones no listing could match.
type SessionGaps struct {
	Unnamed int
	Failed  []error
}

// agentPanel is one panel a probe claimed, and the target of the session being
// recorded that it belongs to.
type agentPanel struct {
	project, target int
	panel           revier.Panel
	probe           revier.AgentProbe
}

// conversations asks each resumable probe once, for every panel it claimed
// across the whole save, and returns a conversation per agent panel in order.
// A panel whose probe cannot resume, cannot say, or failed gets a zero one: it
// restores empty, which is not a failure of the save; a probe that failed is
// returned with its error, named.
func (c *Core) conversations(ctx context.Context, agents []agentPanel) ([]revier.Conversation, []error) {
	var failed []error
	out := make([]revier.Conversation, len(agents))
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
			out[i] = named[j]
		}
	}
	return out, failed
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
	for _, a := range t.Agents {
		out = append(out, Resume{Harness: a.Harness, Session: a.Session, Dir: a.Dir})
	}
	return out
}

// resuming returns the realization with the recorded agents laid over its
// declared agent panels, what became of each of those, and the recorded agents
// past them, which the launch adds once the instance is open. The panel slice
// is copied before anything is written to it: the realization arrives sharing
// the prepared project's panels, and a restore must not edit the project every
// later keypress reads.
func (c *Core) resuming(real revier.Realization, resumes []Resume) (revier.Realization, []AgentOutcome, []Resume) {
	if len(resumes) == 0 {
		return real, nil, nil
	}
	var outcomes []AgentOutcome
	var extra []Resume
	real.Panels, outcomes, extra = c.layAgents(real.Panels, resumes)
	return real, outcomes, extra
}

// Resumes is what a launch of the target would do with a step's recorded
// agents now, for a dry run that says what a restore would do. It lays them
// over the realization a launch resolves, and builds the agent tabs a launch
// would add, so the two cannot disagree.
func (c *Core) Resumes(p Project, name revier.TargetName, resumes []Resume) []AgentOutcome {
	i, ok := p.index(name)
	if !ok {
		return nil
	}
	if p.isTab(i) {
		_, outcomes := c.tabResuming(*p.Targets[i].Runtime, resumes)
		return outcomes
	}
	host, real, _, err := c.resolveAt(p, i)
	if err != nil {
		return nil
	}
	_, outcomes, extra := c.resuming(real, resumes)
	_, added := c.agentTabs(host, real, extra)
	return append(outcomes, added...)
}

// layAgents lays recorded agents over a layout, in order, and says what each
// comes to (decisions.md D62, D63).
//
// The first recorded agent goes to the first panel declared as an agent, the
// second to the second, and so on. The agents past the declared ones are
// returned: each is added to the open instance as an agent tab, which is what
// `revier agent new` adds, so an agent opened by hand beside a workspace comes
// back the way it was opened.
func (c *Core) layAgents(layout []revier.PanelSpec, resumes []Resume) ([]revier.PanelSpec, []AgentOutcome, []Resume) {
	panels := append([]revier.PanelSpec(nil), layout...)
	var outcomes []AgentOutcome
	n := 0
	for i := range panels {
		if panels[i].Kind != revier.PanelAgent || n == len(resumes) {
			continue
		}
		outcomes = append(outcomes, c.startAgent(&panels[i], resumes[n]))
		n++
	}
	return panels, outcomes, resumes[n:]
}

// startAgent points an agent panel at its recorded directory and conversation,
// and says what the agent comes to.
//
// An agent starts in the directory it worked in, and on its conversation when
// one was recorded and its probe here can resume it. A directory that is gone
// starts the agent empty where the realization starts: its conversation
// resumed there would carry on in the wrong checkout, and its edits would
// land there. Every other way a resume can fail starts the agent empty.
func (c *Core) startAgent(spec *revier.PanelSpec, r Resume) AgentOutcome {
	if r.Dir != "" && !dirExists(r.Dir) {
		return AgentDirGone
	}
	spec.Dir = r.Dir
	res, ok := c.resumer(r.Harness)
	if r.Session == "" || !ok {
		return AgentEmpty
	}
	spec.Command = res.ResumeCommand(*spec, r.Session)
	return AgentResumed
}

// resumer is the probe of that name here, when it can resume. With no name it
// is the first probe that can: `revier agent new --resume` names a
// conversation and not the harness that holds it.
func (c *Core) resumer(harness string) (revier.Resumable, bool) {
	for _, probe := range c.Probes {
		res, ok := probe.(revier.Resumable)
		if harness == "" && ok {
			return res, true
		}
		if probe.Name() == harness {
			return res, ok
		}
	}
	return nil, false
}
