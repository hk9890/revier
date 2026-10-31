package core

import (
	"fmt"
	"time"

	"github.com/hk9890/revier/internal/state"
	"github.com/hk9890/revier/pkg/revier"
)

// Settled is what a surface knows of what runs here beside a project's view:
// whether a survey has answered, and what the state it settled says landed,
// was attached by hand, or was launched and has no window yet.
type Settled struct {
	Surveyed bool
	Bound    map[revier.ProjectName]map[revier.TargetName]revier.TargetRef
	Attached map[revier.ProjectName][]revier.TargetRef
	Launch   *state.Launch
}

// RefuseChange refuses deleting or renaming a project with a target running
// or a window attached to it, since its windows are found by titles its name
// is part of (decisions.md D80). The last survey says what runs; a target that
// landed since, or a launch whose window is still to appear, is read from
// state, else the change would leave that window reachable by no name. An
// attachment or a binding on a host here is alive: every settle prunes the
// ones that host no longer lists. One on another host - a surface started
// where that host does not probe - cannot be reached from here, so it does
// not hold the change up. doing is the change, as in "renaming".
func (c *Core) RefuseChange(v revier.ProjectView, s Settled, doing string) error {
	name := v.Project.Name
	// Before the first survey the view is the files' alone, and says nothing
	// about what runs.
	if !s.Surveyed {
		return fmt.Errorf("no survey has answered yet; wait for it before %s %s", doing, name)
	}
	if c.RunsHere(v, s.Bound[name]) {
		return fmt.Errorf("%s is running; close its targets before %s it", name, doing)
	}
	if err := RefuseUnsettled(v, s.Launch, doing); err != nil {
		return err
	}
	if c.AttachedHere(s.Attached[name]) {
		return fmt.Errorf("%s has an attached window open; close it before %s the project", name, doing)
	}
	return nil
}

// RefuseUnsettled refuses the change while what of the project runs is not
// known: a target whose host could not list may be running (decisions.md
// D89), and launch, the launch on record, may be the project's with its
// window still to appear. The change waits for either, not for a guess.
func RefuseUnsettled(v revier.ProjectView, launch *state.Launch, doing string) error {
	for _, t := range v.Targets {
		if t.Unknown != "" {
			return fmt.Errorf("%s: %s; wait for the host before %s it", v.Project.Name, t.Unknown, doing)
		}
	}
	if launch.Pending(time.Now()) && launch.Project == v.Project.Name {
		return fmt.Errorf("%s is coming up; wait for its window before %s it", v.Project.Name, doing)
	}
	return nil
}

// RunsHere reports a project with a target running, by the survey or by where
// state says a target landed: bound is the project's entry in state.Bound.
func (c *Core) RunsHere(v revier.ProjectView, bound map[revier.TargetName]revier.TargetRef) bool {
	running := v.Held()
	for _, ref := range bound {
		running = running || c.HostHere(ref.Host)
	}
	return running
}

// AttachedHere reports one of a project's attached windows on the window host
// here: attached is the project's entry in state.Attached.
func (c *Core) AttachedHere(attached []revier.TargetRef) bool {
	for _, ref := range attached {
		if c.Window != nil && ref.Host == c.Window.Name() {
			return true
		}
	}
	return false
}

// HostHere reports a host this core lists, whose refs a settle prunes.
func (c *Core) HostHere(host string) bool {
	return (c.Runtime != nil && host == c.Runtime.Name()) || (c.Window != nil && host == c.Window.Name())
}
