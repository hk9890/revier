package config

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/pkg/revier"
)

// One project file, written from the project screen. As config.toml is, the
// file is changed line by line, so its comments and the values the screen
// does not show stay as they were, and nothing is written that does not load.
//
// A target the project shares with config.toml is written as the values that
// differ from the shared one (decisions.md D59): the file never repeats a
// shared value, and config.toml is never written from here.

// Source is where a project's target comes from.
type Source int

const (
	FromProject Source = iota // the project file alone
	FromShared                // config.toml alone
	Overridden                // config.toml, with values the project file changes
	Derived                   // a link's pane onto its host, which the file leaves out
)

// ProjectTarget is a target as the project has it, templates unrendered.
type ProjectTarget struct {
	Target revier.Target
	Source Source
	// Shared is the target of config.toml it is or overrides.
	Shared *revier.Target
}

// ProjectText is a project file as written: what the project screen shows.
type ProjectText struct {
	Path    string
	GitURL  string
	Remote  *revier.Link // a link's host and the name there, the derived one included
	Vars    map[string]string
	Targets []ProjectTarget
}

// ReadProject is the project in file, with the shared targets it has.
func ReadProject(file string, shared []map[string]any) (ProjectText, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return ProjectText{}, err
	}
	own, _, err := decodeProject(data, nil)
	if err != nil {
		return ProjectText{}, fmt.Errorf("%s: %w", file, err)
	}
	out := ProjectText{Path: own.Path, GitURL: own.GitURL, Vars: own.Vars}
	merged, _, err := decodeProject(data, shared)
	if err != nil {
		return ProjectText{}, fmt.Errorf("%s: %w", file, err)
	}
	sharedTargets, err := targetsFor(shared, own.Remote != nil)
	if err != nil {
		return ProjectText{}, err
	}
	if own.Remote != nil {
		p := merged
		p.Name = nameOf(file)
		remote := *own.Remote
		p.Remote = &remote
		link(&p)
		out.Remote = p.Remote
		for _, t := range p.Targets {
			pt := ProjectTarget{Target: t, Source: FromProject}
			if s := findTarget(sharedTargets, t.Name); s >= 0 {
				pt.Shared = &sharedTargets[s]
				pt.Source = FromShared
				if findTarget(own.Targets, t.Name) >= 0 {
					pt.Source = Overridden
				}
			} else if findTarget(own.Targets, t.Name) < 0 {
				// The pane onto the host, which neither the file nor
				// config.toml declares.
				pt.Source = Derived
			}
			out.Targets = append(out.Targets, pt)
		}
		return out, nil
	}
	for _, t := range merged.Targets {
		pt := ProjectTarget{Target: t, Source: FromProject}
		if s := findTarget(sharedTargets, t.Name); s >= 0 {
			pt.Shared = &sharedTargets[s]
			pt.Source = FromShared
			if findTarget(own.Targets, t.Name) >= 0 {
				pt.Source = Overridden
			}
		}
		out.Targets = append(out.Targets, pt)
	}
	return out, nil
}

// SetProjectValue writes one top-level value of a project file: path or
// git_url. An empty value removes the key.
func SetProjectValue(file string, shared []map[string]any, key, value string) (core.Project, error) {
	return editProject(file, shared, func(lines []string, _ projectFile) ([]string, error) {
		top := tables(lines)[0]
		if value == "" {
			return deleteKey(lines, top, key), nil
		}
		return putKey(lines, top, key, quote(value)), nil
	}, nil)
}

// SaveProjectTarget writes a target of the project as e says. was is the
// name the target had, empty for a new one. A target config.toml has is
// written as the values that differ from it, and an override left with
// none is removed.
func SaveProjectTarget(file string, shared []map[string]any, was revier.TargetName, e TargetEdit) (core.Project, error) {
	t := e.Target
	return editProject(file, shared, func(lines []string, f projectFile) ([]string, error) {
		sharedTargets, err := targetsFor(shared, f.link)
		if err != nil {
			return nil, err
		}
		if was != t.Name {
			if findTarget(sharedTargets, was) >= 0 {
				return nil, fmt.Errorf("target %q is config.toml's, and keeps its name here", was)
			}
			if findTarget(f.targets, t.Name) >= 0 {
				return nil, fmt.Errorf("the project has a target named %q already", t.Name)
			}
			if findTarget(sharedTargets, t.Name) >= 0 {
				return nil, fmt.Errorf("config.toml declares a target named %q; change that one to override it here", t.Name)
			}
		}
		own, from := t, e.PanelFrom
		i := findTarget(f.targets, was)
		if s := findTarget(sharedTargets, t.Name); s >= 0 {
			if own, err = overrideOf(t, sharedTargets[s]); err != nil {
				return nil, err
			}
			if i < 0 || panelCount(f.targets[i]) == 0 {
				from = newPanels(panelCount(own))
			}
			if panelCount(own) == 0 {
				from = nil
			}
		}
		bare := own == revier.Target{Name: t.Name}
		if i < 0 {
			if bare {
				return lines, nil
			}
			i = len(f.targets)
			lines = appendTarget(lines, t.Name)
			return applyTarget(lines, i, revier.Target{Name: t.Name}, map[string]any{}, f.base(), TargetEdit{Target: own, PanelFrom: newPanels(panelCount(own))})
		}
		if bare {
			e := targetEntries(lines)[i]
			return dropLines(lines, e.start, e.end), nil
		}
		return applyTarget(lines, i, f.targets[i], f.rawOf(i), f.base(), TargetEdit{Target: own, PanelFrom: from})
	}, func(decoded revier.Project, prepared core.Project) error {
		if i := findTarget(decoded.Targets, t.Name); i < 0 || !sameTargets(decoded.Targets[i:i+1], []revier.Target{t}) {
			return errors.New("the target did not come out as written; change it by hand")
		}
		// The target written is the one being fixed: a change that leaves it
		// refused has not fixed it, whatever it was refused for before.
		if i := findTarget(prepared.Targets, t.Name); i >= 0 {
			if err := prepared.TargetErr(i); err != nil {
				return fmt.Errorf("not written: %w", err)
			}
		}
		return nil
	})
}

// RemoveProjectTarget deletes the project file's own entry for a target: a
// target of its own goes, and an override leaves the shared target as it is
// in config.toml.
func RemoveProjectTarget(file string, shared []map[string]any, name revier.TargetName) (core.Project, error) {
	return editProject(file, shared, func(lines []string, f projectFile) ([]string, error) {
		i := findTarget(f.targets, name)
		if i < 0 {
			return nil, fmt.Errorf("target %q is config.toml's; the project file does not declare it", name)
		}
		e := targetEntries(lines)[i]
		return dropLines(lines, e.start, e.end), nil
	}, nil)
}

// projectFile is a project file's own targets, as typed and as TOML decoded
// them, and whether it is a link.
type projectFile struct {
	targets []revier.Target
	raw     []map[string]any
	link    bool
}

// base is the table a target's realizations sit under in this file: a link
// writes them under [target.remote] (decisions.md D82).
func (f projectFile) base() string {
	if f.link {
		return "target.remote"
	}
	return "target"
}

// rawOf is the i-th entry's realizations as TOML decoded them, under the
// table of this file's kind.
func (f projectFile) rawOf(i int) map[string]any {
	raw := f.raw[i]
	if !f.link {
		return raw
	}
	remote, _ := raw["remote"].(map[string]any)
	return remote
}

// editProject runs one change to a project file, and writes it only if the
// result loads no worse than the file did, and check, when given, accepts the
// project it decodes to and the one it prepares to.
func editProject(file string, shared []map[string]any, change func([]string, projectFile) ([]string, error), check func(revier.Project, core.Project) error) (core.Project, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return core.Project{}, err
	}
	own, _, err := decodeProject(data, nil)
	if err != nil {
		return core.Project{}, fmt.Errorf("%s: %w", file, err)
	}
	var raw map[string]any
	if _, err := toml.Decode(string(data), &raw); err != nil {
		return core.Project{}, fmt.Errorf("%s: %w", file, err)
	}
	lines := strings.Split(string(data), "\n")
	if len(targetEntries(lines)) != len(own.Targets) {
		return core.Project{}, fmt.Errorf("%s: the targets are not written as [[target]] tables; change them by hand", file)
	}
	lines, err = change(lines, projectFile{targets: own.Targets, raw: tablesOf(raw["target"]), link: own.Remote != nil})
	if err != nil {
		return core.Project{}, fmt.Errorf("%s: %w", file, err)
	}
	text := []byte(strings.Join(lines, "\n"))
	// Loading no longer refuses a project, so the edit is what refuses: an
	// edit the surface makes is refused for what it breaks, where a file the
	// user wrote by hand is loaded as it is and reports what is wrong with
	// it. What was wrong before the edit is not this edit's to refuse: a file
	// with two broken targets is repaired one at a time.
	p := loadProject(file, text, shared)
	if probs := newProblems(loadProject(file, data, shared), p); len(probs) > 0 {
		return core.Project{}, fmt.Errorf("not written: %w", errors.Join(probs...))
	}
	if check != nil {
		decoded, _, err := decodeProject(text, shared)
		if err == nil {
			err = check(decoded, p)
		}
		if err != nil {
			return core.Project{}, fmt.Errorf("%s: %w", file, err)
		}
	}
	return p, replaceFile(file, text)
}

// overrideOf is what a project file writes for t, a target config.toml has
// as shared: the values that differ from it. A value the project empties
// cannot be written, because an absent key is the shared value.
func overrideOf(t, shared revier.Target) (revier.Target, error) {
	o := revier.Target{Name: t.Name}
	var err error
	if o.Key, err = differs("key", t.Key, shared.Key); err != nil {
		return o, err
	}
	if o.Prefer, err = differs("prefer", t.Prefer, shared.Prefer); err != nil {
		return o, err
	}
	if o.Home, err = differs("home", t.Home, shared.Home); err != nil {
		return o, err
	}
	if o.Runtime, err = realizationOverride("runtime", t.Runtime, shared.Runtime); err != nil {
		return o, err
	}
	if o.Window, err = realizationOverride("window", t.Window, shared.Window); err != nil {
		return o, err
	}
	return o, nil
}

func realizationOverride(kind string, r, shared *revier.Realization) (*revier.Realization, error) {
	switch {
	case r == nil && shared == nil:
		return nil, nil
	case r == nil:
		return nil, cannotClear(kind)
	case shared == nil:
		return r, nil
	}
	var o revier.Realization
	var err error
	for _, f := range []struct {
		what        string
		into        *string
		value, from string
	}{
		{"name", &o.Name, r.Name, shared.Name},
		{"dir", &o.Dir, r.Dir, shared.Dir},
		{"place", &o.Place, r.Place, shared.Place},
		{"match title", &o.Match.Title, r.Match.Title, shared.Match.Title},
		{"match class", &o.Match.Class, r.Match.Class, shared.Match.Class},
	} {
		if *f.into, err = differs(kind+" "+f.what, f.value, f.from); err != nil {
			return nil, err
		}
	}
	if o.Match.PID, err = differs(kind+" match pid", r.Match.PID, shared.Match.PID); err != nil {
		return nil, err
	}
	if o.Inside, err = differs(kind+" inside", r.Inside, shared.Inside); err != nil {
		return nil, err
	}
	if o.Launch, err = listOverride(kind+" command", r.Launch, shared.Launch); err != nil {
		return nil, err
	}
	if o.Panels, err = listOverride(kind+" panels", r.Panels, shared.Panels); err != nil {
		return nil, err
	}
	if reflect.DeepEqual(o, revier.Realization{}) {
		return nil, nil
	}
	return &o, nil
}

func differs[T comparable](what string, value, shared T) (T, error) {
	var zero T
	switch value {
	case shared:
		return zero, nil
	case zero:
		return zero, cannotClear(what)
	}
	return value, nil
}

// listOverride is differs for a list, which a project replaces whole.
func listOverride[T any](what string, value, shared []T) ([]T, error) {
	switch {
	case len(value) == 0 && len(shared) == 0, reflect.DeepEqual(value, shared):
		return nil, nil
	case len(value) == 0:
		return nil, cannotClear(what)
	}
	return value, nil
}

func cannotClear(what string) error {
	return fmt.Errorf("%s: config.toml sets it for every project, and a project cannot empty it", what)
}

func findTarget(targets []revier.Target, name revier.TargetName) int {
	return slices.IndexFunc(targets, func(t revier.Target) bool { return t.Name == name })
}

// appendTarget adds an entry with only a name at the end of the file.
func appendTarget(lines []string, name revier.TargetName) []string {
	out := strings.TrimRight(strings.Join(lines, "\n"), "\n")
	if out != "" {
		out += "\n\n"
	}
	return strings.Split(out+"[[target]]\nname = "+quote(string(name))+"\n", "\n")
}

// newPanels is the PanelFrom of n panels none of which the file has.
func newPanels(n int) []int {
	from := make([]int, n)
	for j := range from {
		from[j] = -1
	}
	return from
}
