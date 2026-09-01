package core

import (
	"errors"
	"fmt"

	"github.com/hk9890/revier/pkg/revier"
)

// Project is a revier.Project prepared for the hot path: every realization
// rendered and every match compiled, once, when the configuration is loaded.
// Survey and Go take this form and render or compile nothing of their own, so
// a refresh re-derives nothing from inputs that have not changed since load.
//
// The embedded Project is the rendered one: its realizations are what a host
// receives, and what --json prints. The zero value is unusable; construct one
// with PrepareProject.
type Project struct {
	revier.Project
	compiled []compiledTarget // parallel to Project.Targets
}

type compiledTarget struct {
	window, runtime revier.CompiledMatch
}

// index returns the position of the named target.
func (p Project) index(name revier.TargetName) (int, bool) {
	for i, t := range p.Targets {
		if t.Name == name {
			return i, true
		}
	}
	return 0, false
}

// Prepare prepares every project. One that cannot be prepared fails the whole
// set with an error naming it: this is load time, where a refusal can be read
// and the file fixed, not the keystroke, where it cannot.
func Prepare(projects []revier.Project) ([]Project, error) {
	out := make([]Project, 0, len(projects))
	var errs []error
	for _, p := range projects {
		prepared, err := PrepareProject(p)
		if err != nil {
			errs = append(errs, fmt.Errorf("project %q: %w", p.Name, err))
			continue
		}
		out = append(out, prepared)
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return out, nil
}

// PrepareProject renders one project's templates and compiles its matches.
//
// A template referring to a missing key, a pattern that does not parse, and a
// match that constrains nothing are all errors here. They used to surface at
// the keystroke, or be swallowed by the survey as a project with no available
// targets; a bad file is now refused at load like every other validation
// failure, naming the target.
func PrepareProject(p revier.Project) (Project, error) {
	rendered, err := Render(p)
	if err != nil {
		return Project{}, err
	}
	compiled := make([]compiledTarget, len(rendered.Targets))
	for i, t := range rendered.Targets {
		if t.Window != nil {
			if compiled[i].window, err = compileMatch(t.Name, revier.HostWindow, t.Window.Match); err != nil {
				return Project{}, err
			}
		}
		if t.Runtime != nil {
			if compiled[i].runtime, err = compileMatch(t.Name, revier.HostRuntime, t.Runtime.Match); err != nil {
				return Project{}, err
			}
		}
	}
	return Project{Project: rendered, compiled: compiled}, nil
}

func compileMatch(name revier.TargetName, kind revier.HostKind, m revier.Match) (revier.CompiledMatch, error) {
	if m.IsZero() {
		// An unconstrained match would select whichever instance the host
		// happens to list first, so run-or-raise would raise a random window.
		return revier.CompiledMatch{}, fmt.Errorf("target %q %s realization: %w", name, kind, ErrUnboundedMatch)
	}
	c, err := m.Compile()
	if err != nil {
		return revier.CompiledMatch{}, fmt.Errorf("target %q %s realization: %w", name, kind, err)
	}
	return c, nil
}
