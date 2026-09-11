// Package config loads revier's configuration: the global settings and one
// TOML file per project.
//
// Validation happens here rather than at the keystroke. A match that
// constrains nothing, a project with no home target, or two targets sharing a
// key are all rejected at load, where the message can name the file. So is a
// template that does not render or a pattern that does not compile: projects
// leave this package prepared (core.Project), with that work done once.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/BurntSushi/toml"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/theme"
	"github.com/hk9890/revier/pkg/revier"
)

// Config is the global configuration: which adapters to prefer, the actions
// the TUI exposes, and the external agent probes.
type Config struct {
	Hosts   Hosts    `toml:"hosts"`
	UI      UI       `toml:"ui"`
	Actions []Action `toml:"action"`
	Probes  []Probe  `toml:"probe"`
}

// UI is how the TUI looks. Both names are resolved at load, so a typo is a
// startup error and not an unstyled surface.
type UI struct {
	Theme  string `toml:"theme"`
	Glyphs string `toml:"glyphs"`

	// TriggerKey is the desktop chord that opens revier. It belongs here and
	// not on a project, because it opens the surface itself rather than any
	// one project's target, so nothing in projects/ could declare it.
	TriggerKey string `toml:"trigger_key"`
}

// DefaultTriggerKey is the chord revier expects to be opened with. It has a
// default so `revier keys status` reports the key on a machine with no
// config.toml at all, which is the common case.
const DefaultTriggerKey = "alt-space"

// Probe declares an external agent probe: a binary that reads one panel as
// JSON and answers with one agent state. Name is the harness, and the
// foreground command the probe claims; Exec is the binary.
type Probe struct {
	Name string `toml:"name"`
	Exec string `toml:"exec"`
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
func Load(root string) (*Config, []core.Project, error) {
	cfg := &Config{}
	cfgPath := filepath.Join(root, "config.toml")
	if _, err := toml.DecodeFile(cfgPath, cfg); err != nil && !os.IsNotExist(err) {
		return nil, nil, fmt.Errorf("%s: %w", cfgPath, err)
	}
	for i, pr := range cfg.Probes {
		if pr.Name == "" || pr.Exec == "" {
			return nil, nil, fmt.Errorf("%s: probe %d needs both name and exec", cfgPath, i+1)
		}
		cfg.Probes[i].Exec = expandHome(pr.Exec)
	}

	if _, err := cfg.Theme(); err != nil {
		return nil, nil, fmt.Errorf("%s: %w", cfgPath, err)
	}
	if _, err := cfg.TriggerKey(); err != nil {
		return nil, nil, fmt.Errorf("%s: ui.trigger_key: %w", cfgPath, err)
	}
	if err := validateActions(cfg.Actions); err != nil {
		return nil, nil, fmt.Errorf("%s: %w", cfgPath, err)
	}

	projects, err := LoadProjects(filepath.Join(root, "projects"))
	if err != nil {
		return nil, nil, err
	}
	return cfg, projects, nil
}

// Theme resolves the configured palette and glyph set. An empty [ui] table
// gives the default.
func (c *Config) Theme() (theme.Theme, error) {
	return theme.Lookup(c.UI.Theme, c.UI.Glyphs)
}

// TriggerKey resolves the chord that opens revier. An empty [ui] table gives
// DefaultTriggerKey.
func (c *Config) TriggerKey() (core.Chord, error) {
	if c.UI.TriggerKey == "" {
		return core.ParseChord(DefaultTriggerKey)
	}
	return core.ParseChord(c.UI.TriggerKey)
}

// validateActions refuses an action the TUI can never run. An action's key is
// the TUI's alone - no desktop binding carries it - so a key the terminal
// does not deliver, or one the filter takes as typed text, does nothing, and
// the only place to say so is here.
func validateActions(actions []Action) error {
	var errs []error
	for _, act := range actions {
		if len(act.Run) == 0 {
			errs = append(errs, fmt.Errorf("action %q runs nothing", act.Name))
		}
		chord, err := core.ParseChord(act.Key)
		if err != nil {
			errs = append(errs, fmt.Errorf("action %q: %w", act.Name, err))
			continue
		}
		sent, ok := chord.Terminal()
		switch {
		case chord.Typed():
			errs = append(errs, fmt.Errorf("action %q: key %q is typed text, which the TUI filters on; give it ctrl or alt", act.Name, act.Key))
		case !ok:
			errs = append(errs, fmt.Errorf("action %q: key %q never reaches a terminal", act.Name, act.Key))
		case sent != chord:
			errs = append(errs, fmt.Errorf("action %q: key %q reaches a terminal as %s; bind that instead", act.Name, act.Key, sent))
		}
	}
	return errors.Join(errs...)
}

// LoadProjects reads every *.toml in dir, sorted by name so ordering is stable
// across machines.
func LoadProjects(dir string) ([]core.Project, error) {
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

	projects := make([]core.Project, 0, len(names))
	declared := map[revier.ProjectName]string{}
	var errs []error
	for _, name := range names {
		path := filepath.Join(dir, name)
		p, err := LoadProject(path)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		// A project is found by name everywhere - a lookup, a key in state, a
		// run log - so a second file under a taken name would act as the first.
		if first, taken := declared[p.Name]; taken {
			errs = append(errs, fmt.Errorf("%s: project %q is already declared in %s", path, p.Name, first))
			continue
		}
		declared[p.Name] = path
		projects = append(projects, p)
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return projects, nil
}

// LoadProject reads, validates, and prepares one project file. Every error
// names the file: a rendering or compile failure is reported here, at load,
// and never reaches a keystroke.
func LoadProject(path string) (core.Project, error) {
	var p revier.Project
	if _, err := toml.DecodeFile(path, &p); err != nil {
		return core.Project{}, fmt.Errorf("%s: %w", path, err)
	}
	if p.Name == "" {
		// Fall back to the file stem so a project file need not repeat its own
		// name, and so a renamed file cannot silently keep the old identity.
		p.Name = revier.ProjectName(strings.TrimSuffix(filepath.Base(path), ".toml"))
	}
	// A path is expanded once, here, so every consumer - templates, working
	// directories, the cwd lookup - sees an absolute path and none of them
	// hands a literal "~" to a program that does not expand it. A remote
	// project's path is its host's to expand: the same file is read there,
	// and the home directory here says nothing about the one there.
	if p.Host == "" {
		p.Path = expandHome(p.Path)
	}
	for _, t := range p.Targets {
		for _, r := range []*revier.Realization{t.Window, t.Runtime} {
			if r != nil {
				r.Dir = expandHome(r.Dir)
			}
		}
	}
	if err := Validate(p); err != nil {
		return core.Project{}, fmt.Errorf("%s: %w", path, err)
	}
	prepared, err := core.PrepareProject(p)
	if err != nil {
		return core.Project{}, fmt.Errorf("%s: %w", path, err)
	}
	prepared.File = path
	return prepared, nil
}

// Validate rejects a project whose structure would fail at the keystroke
// instead of at load. Every rule here names a failure that is invisible until
// the key is pressed. Whether a template renders and a pattern compiles is
// core.PrepareProject's to check; LoadProject runs both.
func Validate(p revier.Project) error {
	var errs []error

	if p.Path == "" {
		errs = append(errs, errors.New("project has no path"))
	}
	if strings.ContainsRune(string(p.Name), ':') {
		// `revier agent` addresses <project>:<target>, and the address is
		// split at its first colon.
		errs = append(errs, fmt.Errorf("project name %q contains \":\", which separates a project from its target in an agent address", p.Name))
	}
	if p.GitURL != "" {
		if err := ValidateGitURL(p.GitURL); err != nil {
			errs = append(errs, fmt.Errorf("git_url: %w", err))
		}
	}
	if p.Host != "" {
		if err := validateHost(p.Host); err != nil {
			errs = append(errs, fmt.Errorf("host: %w", err))
		}
	}

	homes := 0
	seenName := map[revier.TargetName]bool{}
	// Keyed by the canonical chord, not by the text. Two targets written
	// "ctrl-o" and "Ctrl+O" are the same key, and the raw strings do not say
	// so.
	seenKey := map[core.Chord]struct {
		target revier.TargetName
		raw    string
	}{}

	for _, t := range p.Targets {
		if t.Name == "" {
			errs = append(errs, errors.New("a target has no name"))
			continue
		}
		if err := core.ValidateTargetName(t.Name); err != nil {
			errs = append(errs, err)
		}
		if seenName[t.Name] {
			errs = append(errs, fmt.Errorf("target %q declared twice", t.Name))
		}
		seenName[t.Name] = true

		if t.Home {
			homes++
		}
		if t.Key != "" {
			if err := core.ValidateKeyTarget(t.Name); err != nil {
				errs = append(errs, err)
			}
			// A key that cannot be read is rejected here rather than dropped
			// later. Dropped, it costs the target its desktop chord and its
			// row in `revier keys status`, which is the one place a user
			// would look to find out what happened to it.
			chord, err := core.ParseChord(t.Key)
			if err != nil {
				errs = append(errs, fmt.Errorf("target %q: %w", t.Name, err))
			} else {
				// Both spellings are named: the two targets may be written
				// differently and still be the same key, and the file is
				// where the reader has to find them.
				if prev, ok := seenKey[chord]; ok {
					errs = append(errs, fmt.Errorf("targets %q and %q share key %q: %q and %q",
						prev.target, t.Name, chord, prev.raw, t.Key))
				}
				seenKey[chord] = struct {
					target revier.TargetName
					raw    string
				}{t.Name, t.Key}
			}
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
			if len(r.Launch) == 0 && len(r.Panels) == 0 {
				errs = append(errs, fmt.Errorf("target %q %s realization has no launch argv and no panels", t.Name, kind))
			}
			if len(r.Launch) > 0 && len(r.Panels) > 0 {
				// A host opens one or the other; a launch beside panels would
				// be dropped silently, and the agent it named never started.
				errs = append(errs, fmt.Errorf("target %q %s realization has both launch and panels; panels are what is launched, so drop launch", t.Name, kind))
			}
			if len(r.Panels) > 0 && kind == revier.HostWindow {
				errs = append(errs, fmt.Errorf("target %q window realization declares panels; only a runtime has them", t.Name))
			}
			if r.Name == "" && kind == revier.HostRuntime {
				// A runtime host gives the instance it opens this name, and
				// has no other identity to give it; both refuse to open
				// without one. A window host needs none: its launch argv
				// carries the identity match finds.
				errs = append(errs, fmt.Errorf("target %q runtime realization has no name; give it the name its match finds", t.Name))
			}
			if r.Place != "" && len(strings.Fields(r.Place)) != 4 {
				// A geometry short of its four tokens would reach the window
				// host and be refused there, after the window had opened.
				errs = append(errs, fmt.Errorf("target %q %s realization: place needs four tokens, x y width height, got %q", t.Name, kind, r.Place))
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

// ValidateGitURL refuses a clone URL that is unsafe to hand to git or to keep
// in a file shared between machines. The rules are the shell tool's
// (_session_is_safe_clone_url in ~/setup/scripts/sessions), so a URL one of
// them recorded the other accepts: no whitespace, no control characters, no
// shell metacharacters, and no credentials in an https URL.
//
// A URL with credentials is not echoed back: the message would print the
// token the rule exists to keep out of the file.
func ValidateGitURL(u string) error {
	switch {
	case u == "":
		return errors.New("empty")
	case httpsUserinfo.MatchString(u):
		return errors.New("an https URL with credentials in it is refused; record it without the user part")
	case strings.IndexFunc(u, unicode.IsSpace) >= 0:
		return fmt.Errorf("%q contains whitespace", u)
	case strings.IndexFunc(u, func(r rune) bool { return !unicode.IsPrint(r) }) >= 0:
		return fmt.Errorf("%q contains a control character", u)
	case strings.ContainsAny(u, "`\"'\\$;|&<>(){}"):
		return fmt.Errorf("%q contains a shell metacharacter", u)
	}
	return nil
}

// validateHost refuses a host ssh would read as something other than a
// destination. The name is handed to ssh as one argument after "--", so a
// shell character is harmless; a space or a control character is a name no
// ssh config holds, and a leading dash is a flag.
func validateHost(h string) error {
	switch {
	case strings.HasPrefix(h, "-"):
		return fmt.Errorf("%q starts with a dash, which ssh reads as a flag", h)
	case strings.IndexFunc(h, unicode.IsSpace) >= 0:
		return fmt.Errorf("%q contains whitespace", h)
	case strings.IndexFunc(h, func(r rune) bool { return !unicode.IsPrint(r) }) >= 0:
		return fmt.Errorf("%q contains a control character", h)
	}
	return nil
}

// httpsUserinfo is an https authority with a user part in it:
// https://user:token@host/...
var httpsUserinfo = regexp.MustCompile(`^https://[^/]+@`)

// expandHome resolves a leading "~" against the user's home directory.
func expandHome(p string) string {
	if p != "~" && !strings.HasPrefix(p, "~/") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	return filepath.Join(home, strings.TrimPrefix(p, "~"))
}
