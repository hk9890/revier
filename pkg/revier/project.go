// Package revier holds the ports and shared types of revier: the interfaces an
// adapter implements and the values that cross between the core and a host.
//
// The design that this package implements is in docs/design/. Every operation
// revier performs is the same one: run-or-raise a named target, and remember
// where you came from.
package revier

import (
	"fmt"
	"regexp"
)

type (
	ProjectName string
	TargetName  string
)

// Project is a directory and the set of targets bound to it.
type Project struct {
	Name    ProjectName       `toml:"name" json:"name"`
	Path    string            `toml:"path" json:"path"`
	Targets []Target          `toml:"target" json:"targets"`
	Vars    map[string]string `toml:"vars" json:"vars,omitempty"`
}

// Home returns the project's workspace target, the one toggle-back returns to.
// A project without a home target can still be surveyed but cannot be opened.
func (p Project) Home() (Target, bool) {
	for _, t := range p.Targets {
		if t.Home {
			return t, true
		}
	}
	return Target{}, false
}

// Target returns the named target.
func (p Project) Target(name TargetName) (Target, bool) {
	for _, t := range p.Targets {
		if t.Name == name {
			return t, true
		}
	}
	return Target{}, false
}

// Target is a named thing reached with a key: the workspace, an editor, a diff
// viewer, a page. What a target *is* depends on the host that provides it; the
// name and the key do not.
type Target struct {
	Name TargetName `toml:"name" json:"name"`
	Key  string     `toml:"key" json:"key,omitempty"`
	Home bool       `toml:"home" json:"home,omitempty"`

	// Prefer names the host to use when both realizations are available:
	// HostWindow or HostRuntime. Empty applies the default rule, which is
	// HostWindow when a WindowController is configured.
	Prefer HostKind `toml:"prefer" json:"prefer,omitempty"`

	Window  *Realization `toml:"window" json:"window,omitempty"`
	Runtime *Realization `toml:"runtime" json:"runtime,omitempty"`
}

// HostKind names which side of the host split an adapter fills.
type HostKind string

const (
	HostWindow  HostKind = "window"
	HostRuntime HostKind = "runtime"
)

// Realization is how one host provides a target.
type Realization struct {
	// Name is the identity a host gives a new instance: a tmux window name, a
	// kitty OS window title, the --class a browser is launched with. Open must
	// produce an instance that this realization's Match then finds, and Name is
	// how a host that assigns its own identity satisfies that invariant.
	Name string `toml:"name" json:"name,omitempty"`

	// Launch is the argv used when Match finds nothing. It reaches a host
	// already rendered: an adapter never sees a template. A runtime
	// realization with Panels may leave it empty; the panels are then what is
	// launched.
	Launch []string `toml:"launch" json:"launch,omitempty"`

	// Dir is the working directory Launch and every panel start in. The core
	// fills it with the project path when the config leaves it empty, so a
	// workspace opens where the project lives without every file saying so.
	Dir string `toml:"dir" json:"dir,omitempty"`

	Match Match `toml:"match" json:"match"`

	// Panels is the layout for a Home target. Runtime hosts only: a window
	// host cannot see inside a terminal, and ignores it.
	Panels []PanelSpec `toml:"panels" json:"panels,omitempty"`
}

// Match recognises an instance. An empty field does not constrain; every
// non-empty field must match. Title and Class are regular expressions.
type Match struct {
	Class string `toml:"class" json:"class,omitempty"`
	Title string `toml:"title" json:"title,omitempty"`
	PID   int    `toml:"pid" json:"pid,omitempty"`
}

// IsZero reports whether the match constrains nothing. A zero match would
// select every instance, so callers reject it rather than acting on it.
func (m Match) IsZero() bool {
	return m.Class == "" && m.Title == "" && m.PID == 0
}

// Compile prepares the match for repeated use. The core compiles once per
// refresh and matches every instance against the result.
func (m Match) Compile() (CompiledMatch, error) {
	c := CompiledMatch{pid: m.PID}
	var err error
	if m.Class != "" {
		if c.class, err = regexp.Compile(m.Class); err != nil {
			return CompiledMatch{}, fmt.Errorf("match class %q: %w", m.Class, err)
		}
	}
	if m.Title != "" {
		if c.title, err = regexp.Compile(m.Title); err != nil {
			return CompiledMatch{}, fmt.Errorf("match title %q: %w", m.Title, err)
		}
	}
	return c, nil
}

// CompiledMatch is a Match with its patterns compiled.
type CompiledMatch struct {
	class *regexp.Regexp
	title *regexp.Regexp
	pid   int
}

// Matches reports whether the instance satisfies every constraint.
func (c CompiledMatch) Matches(i Instance) bool {
	if c.class != nil && !c.class.MatchString(i.Class) {
		return false
	}
	if c.title != nil && !c.title.MatchString(i.Title) {
		return false
	}
	if c.pid != 0 && c.pid != i.PID {
		return false
	}
	return true
}
