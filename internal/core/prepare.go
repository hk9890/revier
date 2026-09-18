package core

import (
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

	// File is the project file this was loaded from, for the surface that
	// edits or removes it. Empty for a project built in memory.
	File string

	// Invalid is why nothing in this project can run: its file did not
	// parse, or a rule about the project as a whole refused it. The project
	// is kept and listed anyway - a file that vanishes from the surface
	// takes with it the one place its reason could be read (decisions.md
	// D85).
	Invalid error

	compiled []compiledTarget // parallel to Project.Targets
}

type compiledTarget struct {
	window, runtime revier.CompiledMatch

	// windowClass is the window rule's class alone, for binding a window
	// that has just appeared and whose title has not settled. hasClass is
	// false when the rule constrains no class, and then any new window may
	// be the one.
	windowClass revier.CompiledMatch
	hasClass    bool

	// err is why this target cannot be used: a template that did not
	// render, a match that did not compile, or a rule that refused it. The
	// target keeps its place in the project so the surface can name it and
	// say why, and every lookup fails closed on it through resolveAt.
	err error
}

// refuse records why a target cannot be used and leaves it matching nothing.
// Every match is blanked, not only the one that failed: the zero
// CompiledMatch is the opposite of matchesNothing - it constrains nothing and
// takes every instance - and the lookups that read a match without resolving
// first (Core.served, Core.declared) would then hand this target every
// instance the host lists.
func (c *compiledTarget) refuse(err error) {
	c.err = err
	c.runtime, c.window, c.windowClass = matchesNothing, matchesNothing, matchesNothing
	c.hasClass = true
}

// TargetErr is why the i-th target cannot be used, or nil when it can.
func (p Project) TargetErr(i int) error { return p.compiled[i].err }

// Refuse records that a rule refused the i-th target. config calls it for the
// rules it checks before a project is prepared, so that a refusal and a
// rendering failure reach the surface by the same route - and match nothing
// by the same route too, which is what keeps a refused target out of every
// lookup and not only out of the ones that resolve.
func (p *Project) Refuse(i int, err error) { p.compiled[i].refuse(err) }

// index returns the position of the named target.
func (p Project) index(name revier.TargetName) (int, bool) {
	for i, t := range p.Targets {
		if t.Name == name {
			return i, true
		}
	}
	return 0, false
}

// Prepare prepares every project. Preparing cannot fail: a target that does
// not render or compile is kept and marked, and the project keeps the rest.
func Prepare(projects []revier.Project) []Project {
	out := make([]Project, 0, len(projects))
	for _, p := range projects {
		out = append(out, PrepareProject(p))
	}
	return out
}

// PrepareProject renders one project's templates and compiles its matches.
//
// A template referring to a missing key, a pattern that does not parse, and a
// match that constrains nothing are all failures of one target. Each is
// recorded against that target, which then reports itself unavailable with
// the reason; the project's other targets are prepared and work. Refusing the
// whole project - or, as it once did, the whole configuration - costs the user
// every target that was written correctly (decisions.md D85).
func PrepareProject(p revier.Project) Project {
	rendered, errs := Render(p)
	compiled := make([]compiledTarget, len(rendered.Targets))
	for i, t := range rendered.Targets {
		if errs[i] != nil {
			// The realizations are as written, not as rendered: a match
			// compiled from an unrendered pattern would match by accident.
			compiled[i].refuse(errs[i])
			continue
		}
		var err error
		if t.Window != nil {
			if compiled[i].window, err = compileMatch(t.Name, revier.HostWindow, t.Window.Match); err != nil {
				compiled[i].refuse(err)
				continue
			}
			if class := t.Window.Match.Class; class != "" {
				// Already known to compile: the full match did.
				compiled[i].windowClass, _ = revier.Match{Class: class}.Compile()
				compiled[i].hasClass = true
			}
		}
		// A tab inside another target is found by its name, not by a match.
		// Its compiled match matches nothing, so no loop over compiled
		// matches can take a tab for an instance.
		if tabTarget(t) {
			compiled[i].runtime = matchesNothing
			continue
		}
		if t.Runtime != nil {
			if compiled[i].runtime, err = compileMatch(t.Name, revier.HostRuntime, t.Runtime.Match); err != nil {
				compiled[i].refuse(err)
				continue
			}
		}
	}
	return Project{Project: rendered, compiled: compiled}
}

// matchesNothing is a compiled match no instance satisfies. The zero
// CompiledMatch is the opposite: it constrains nothing and matches every one.
var matchesNothing, _ = revier.Match{Title: `[^\s\S]`}.Compile()

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
