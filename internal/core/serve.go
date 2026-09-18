package core

import (
	"errors"
	"fmt"
	"os"

	"github.com/hk9890/revier/pkg/revier"
)

// Serving a panel to a terminal on another machine: a link's panel there is an
// ssh that runs `revier agent exec` or `revier shell exec` here, and that
// process becomes the panel's program (decisions.md D84). What the program is
// and where it starts is this machine's project file's to say, as it is for a
// panel a runtime here opens.

// Served is what such a process becomes.
type Served struct {
	// Workspace is the name of the realization the panel belongs to: what
	// the served-processes host titles the instance, so that realization's
	// Match finds it.
	Workspace string
	Dir       string
	Argv      []string
	// Outcome is what a conversation asked for came to.
	Outcome AgentOutcome
}

// workspace is the first runtime realization of the project that declares a
// panel of that kind, or its home's when none does. It is read from the
// project and not resolved: a machine reached over ssh alone has no runtime,
// and serves its panels all the same. A refused target is passed over, as
// resolveAt passes over it: its realization is as written, not as rendered,
// and nothing may run from an argv that did not render (decisions.md D85).
func workspace(p Project, kind revier.PanelKind) (revier.Realization, revier.PanelSpec, error) {
	var refused []error
	for i, t := range p.Targets {
		if err := p.TargetErr(i); err != nil {
			refused = append(refused, err)
			continue
		}
		if t.Runtime == nil || tabTarget(t) {
			continue
		}
		if spec, ok := declared(t.Runtime.Panels, kind); ok {
			return *t.Runtime, spec, nil
		}
	}
	if home, ok := p.Home(); ok && home.Runtime != nil && kind == revier.PanelShell {
		if i, ok := p.index(home.Name); ok && p.TargetErr(i) == nil {
			return *home.Runtime, revier.PanelSpec{Kind: revier.PanelShell, Dir: home.Runtime.Dir}, nil
		}
	}
	err := fmt.Errorf("%s: no target declares %s panel", p.Name, article(kind))
	if len(refused) > 0 {
		err = fmt.Errorf("%w: %w", err, errors.Join(refused...))
	}
	return revier.Realization{}, revier.PanelSpec{}, err
}

func article(kind revier.PanelKind) string {
	if kind == revier.PanelAgent {
		return "an agent"
	}
	return "a " + string(kind)
}

// ServeAgent is the project's agent panel, started as a restore starts it: in
// the directory and on the conversation r names, when both can be had. The
// conversation arrives with no harness named - the panel's ssh carries the id
// alone - so the harness is the panel's, as an agent tab takes it: a
// conversation of one harness is never resumed into a panel that runs another.
func (c *Core) ServeAgent(p Project, r Resume) (Served, error) {
	real, spec, err := workspace(p, revier.PanelAgent)
	if err != nil {
		return Served{}, err
	}
	if r.Harness == "" {
		r.Harness = c.harnessOf(spec)
	}
	outcome := c.startAgent(&spec, r, false)
	if len(spec.Command) == 0 {
		return Served{}, fmt.Errorf("%s: the agent panel declares no command to run", p.Name)
	}
	return Served{Workspace: real.Name, Dir: spec.Dir, Argv: spec.Command, Outcome: outcome}, nil
}

// ServeShell is the project's shell panel, or the user's login shell where it
// declares none, in dir or else where the panel starts.
func (c *Core) ServeShell(p Project, dir string) (Served, error) {
	real, spec, err := workspace(p, revier.PanelShell)
	if err != nil {
		return Served{}, err
	}
	if dir != "" && dirExists(dir) {
		spec.Dir = dir
	}
	if len(spec.Command) == 0 {
		shell := os.Getenv("SHELL")
		if shell == "" {
			shell = "sh"
		}
		spec.Command = []string{shell, "-l"}
	}
	return Served{Workspace: real.Name, Dir: spec.Dir, Argv: spec.Command}, nil
}
