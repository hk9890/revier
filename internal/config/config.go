// Package config loads revier's configuration: the global settings and one
// TOML file per project.
//
// Validation happens here rather than at the keystroke. A match that
// constrains nothing, a project with no home target, or two targets sharing a
// key are all rejected at load, where the message can name the file.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/hk9890/revier/pkg/revier"
)

// Config is the global configuration: which adapters to prefer, and the
// actions the TUI exposes.
type Config struct {
	Hosts   Hosts    `toml:"hosts"`
	Actions []Action `toml:"action"`
}

// Hosts names adapter preference in order. An empty list means "the first
// adapter that probes successfully", which is the normal case.
type Hosts struct {
	Runtime []string `toml:"runtime"`
	Window  []string `toml:"window"`
}

// Action is a command bound to a key that produces no instance to return to:
// a script, a sync, a clipboard copy. A command that opens something you come
// back from is a target, not an action.
type Action struct {
	Key  string   `toml:"key"`
	Name string   `toml:"name"`
	Run  []string `toml:"run"`
}

// Root reports the configuration directory, honouring REVIER_CONFIG_HOME and
// then XDG_CONFIG_HOME. The override exists so a test, and scripts/drive, can
// run against a scratch tree instead of the user's own configuration.
func Root() (string, error) {
	if r := os.Getenv("REVIER_CONFIG_HOME"); r != "" {
		return r, nil
	}
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "revier"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".config", "revier"), nil
}

// Load reads the global config and every project under <root>/projects.
// A missing root or a missing config.toml is not an error: revier starts with
// no projects rather than refusing to run.
func Load(root string) (*Config, []revier.Project, error) {
	cfg := &Config{}
	cfgPath := filepath.Join(root, "config.toml")
	if _, err := toml.DecodeFile(cfgPath, cfg); err != nil && !os.IsNotExist(err) {
		return nil, nil, fmt.Errorf("%s: %w", cfgPath, err)
	}

	projects, err := LoadProjects(filepath.Join(root, "projects"))
	if err != nil {
		return nil, nil, err
	}
	return cfg, projects, nil
}

// LoadProjects reads every *.toml in dir, sorted by name so ordering is stable
// across machines.
func LoadProjects(dir string) ([]revier.Project, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("%s: %w", dir, err)
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".toml") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	projects := make([]revier.Project, 0, len(names))
	var errs []error
	for _, name := range names {
		path := filepath.Join(dir, name)
		p, err := LoadProject(path)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		projects = append(projects, p)
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return projects, nil
}

// LoadProject reads and validates one project file.
func LoadProject(path string) (revier.Project, error) {
	var p revier.Project
	if _, err := toml.DecodeFile(path, &p); err != nil {
		return revier.Project{}, fmt.Errorf("%s: %w", path, err)
	}
	if p.Name == "" {
		// Fall back to the file stem so a project file need not repeat its own
		// name, and so a renamed file cannot silently keep the old identity.
		p.Name = revier.ProjectName(strings.TrimSuffix(filepath.Base(path), ".toml"))
	}
	if err := Validate(p); err != nil {
		return revier.Project{}, fmt.Errorf("%s: %w", path, err)
	}
	return p, nil
}

// Validate rejects a project that would fail at the keystroke instead of at
// load. Every rule here names a failure that is invisible until the key is
// pressed.
func Validate(p revier.Project) error {
	var errs []error

	if p.Path == "" {
		errs = append(errs, errors.New("project has no path"))
	}

	homes := 0
	seenName := map[revier.TargetName]bool{}
	seenKey := map[string]revier.TargetName{}

	for _, t := range p.Targets {
		if t.Name == "" {
			errs = append(errs, errors.New("a target has no name"))
			continue
		}
		if seenName[t.Name] {
			errs = append(errs, fmt.Errorf("target %q declared twice", t.Name))
		}
		seenName[t.Name] = true

		if t.Home {
			homes++
		}
		if t.Key != "" {
			if prev, ok := seenKey[t.Key]; ok {
				errs = append(errs, fmt.Errorf("targets %q and %q share key %q", prev, t.Name, t.Key))
			}
			seenKey[t.Key] = t.Name
		}

		if t.Window == nil && t.Runtime == nil {
			errs = append(errs, fmt.Errorf("target %q declares no realization", t.Name))
		}
		for kind, r := range map[revier.HostKind]*revier.Realization{
			revier.HostWindow: t.Window, revier.HostRuntime: t.Runtime,
		} {
			if r == nil {
				continue
			}
			if r.Match.IsZero() {
				// An unconstrained match selects whichever instance the host
				// happens to list first, so run-or-raise would raise a random
				// window.
				errs = append(errs, fmt.Errorf("target %q %s realization has an empty match", t.Name, kind))
			}
			if _, err := r.Match.Compile(); err != nil {
				errs = append(errs, fmt.Errorf("target %q %s realization: %w", t.Name, kind, err))
			}
			if len(r.Launch) == 0 {
				errs = append(errs, fmt.Errorf("target %q %s realization has no launch argv", t.Name, kind))
			}
		}
		if t.Prefer != "" && t.Prefer != revier.HostWindow && t.Prefer != revier.HostRuntime {
			errs = append(errs, fmt.Errorf("target %q: prefer must be %q or %q, got %q",
				t.Name, revier.HostWindow, revier.HostRuntime, t.Prefer))
		}
	}

	switch homes {
	case 1:
	case 0:
		errs = append(errs, errors.New("no target is marked home; nothing to open or return to"))
	default:
		errs = append(errs, fmt.Errorf("%d targets are marked home; exactly one may be", homes))
	}

	return errors.Join(errs...)
}
