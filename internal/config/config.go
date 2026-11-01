// Package config loads revier's configuration: the global settings and one
// TOML file per project.
//
// Validation happens here rather than at the keystroke. A match that
// constrains nothing, a project with no home target, or two targets sharing a
// key are all found at load, where the message can name the file. So is a
// template that does not render or a pattern that does not compile: projects
// leave this package prepared (core.Project), with that work done once.
//
// What a failure costs is the smallest thing that is wrong (decisions.md
// D85). validateTargets refuses one target, validateProject refuses one
// project, validateStatuses refuses one [[status]] table, and loading refuses
// nothing: every file becomes a project, marked with what could not be read,
// except a link to this machine, which Load leaves out and lists in SelfLinks
// (D131). Nothing here returns an error for
// a mistake in a project file, because there is no caller that mistake should
// stop - revier is how a user reaches the project they have to fix. The write
// paths are the exception, and ask Problems before they replace a file.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"unicode"

	"github.com/BurntSushi/toml"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/theme"
	"github.com/hk9890/revier/pkg/revier"
)

// Config is the global configuration: which adapters to prefer, the actions
// the TUI exposes, the external agent probes, the statuses it draws, and the
// targets every project shares.
type Config struct {
	Hosts   Hosts         `toml:"hosts"`
	UI      UI            `toml:"ui"`
	Actions []Action      `toml:"action"`
	Probes  []Probe       `toml:"probe"`
	Status  []StatusTable `toml:"status"`
	// Targets are the shared targets, as TOML decoded them, so a project
	// can override any one field of one (shared.go). Every [[target]] the
	// file writes is here, so the config screen edits the file as it is;
	// Shared is the ones a project gets.
	Targets []map[string]any `toml:"target"`
	// Agents are the named agents, as TOML decoded them, so a project can
	// override any one value of one (agents.go).
	Agents []map[string]any `toml:"agent"`
	// Terminal is the terminal's keys and what they do outside a workspace
	// (terminal.go).
	Terminal Terminal `toml:"terminal"`
	// Machines are the machines the remote screen lists (machines.go).
	Machines []RemoteMachine `toml:"machine"`

	// Problems is what config.toml declares that nothing can use, each with
	// its reason: an action the TUI could never run, a shared target no
	// project could take, a probe half declared, a [ui] value that does not
	// resolve. Each costs itself alone (decisions.md D85): the action stays
	// unbound, the target reaches no project, the probe is not run, an
	// unknown theme or glyph set is the default, a trigger key that does not
	// parse refuses only the keys commands, and the file is otherwise read
	// whole. `revier doctor` and the TUI's config screen are where these
	// are read.
	Problems []error `toml:"-"`

	// SelfLinks is the files of the links whose host is this machine, which
	// Load leaves out (decisions.md D131). `revier doctor` says how many.
	SelfLinks []string `toml:"-"`

	statuses []core.Status  // Status without the tables a rule refused
	termKeys []core.TermKey // the terminal keys no rule refused
	machines []string       // the hosts of the machines no rule refused
}

// UI is how the TUI looks. Both names are resolved at load: a typo is one of
// Problems, and the default is drawn in its place.
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

	// Refused is why the action is bound to nothing: a key the TUI cannot
	// run, or nothing to run. The action is still listed, so the config
	// screen can repair it, and a press of its name says this reason.
	Refused error `toml:"-"`
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

// Load reads the global config and every project under <root>/projects,
// without the links to this machine, whose files it lists in SelfLinks
// (decisions.md D131). A missing root or a missing config.toml is not an
// error: revier starts with no projects rather than refusing to run.
func Load(root string) (*Config, []core.Project, error) {
	cfg, err := Read(root)
	if err != nil {
		return nil, nil, err
	}

	projects, err := LoadProjects(filepath.Join(root, "projects"), cfg.Inherited())
	if err != nil {
		return nil, nil, err
	}
	projects, cfg.SelfLinks = elsewhere(projects)
	// A table that changes a shared status refuses only once merged with
	// config.toml's, so the merge's refusals are laid on the project here,
	// where every reader of Problems sees them.
	for i := range projects {
		_, unrun := projects[i].StatusOf(cfg.statuses)
		projects[i].StatusRefused = append(projects[i].StatusRefused, unrun...)
	}
	return cfg, projects, nil
}

// Read is config.toml under root as Load reads it, without the projects: a
// missing file is the empty configuration.
func Read(root string) (*Config, error) {
	path := File(root)
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	cfg, err := parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

// elsewhere is projects without the links to this machine, and the files of
// the ones it left out. The project files are shared between machines, so a
// link that is right on one machine names the other one itself. Leaving one
// out is not a refusal: nothing in the file is wrong, and on the other
// machine the same file is a working link. The machine is read at the first
// link, so a load with no link, on every command, reads no ssh configuration.
func elsewhere(projects []core.Project) ([]core.Project, []string) {
	here := sync.OnceValue(ThisMachine)
	var self []string
	out := projects[:0:0]
	for _, p := range projects {
		if p.Remote != nil && here().Is(p.Remote.Host) {
			self = append(self, p.File)
			continue
		}
		out = append(out, p)
	}
	return out, self
}

// Usable is the shared targets a project gets out of every [[target]]
// config.toml writes: the ones validateShared does not refuse. A surface that
// holds the whole list, to edit the file as it is, passes this to the project
// files.
func Usable(targets []map[string]any) []map[string]any {
	out := make([]map[string]any, 0, len(targets))
	for i, err := range validateShared(targets) {
		if err == nil {
			out = append(out, targets[i])
		}
	}
	return out
}

// problemsAdded is what after holds against config.toml's rules that before
// did not: the reasons a write is refused for. A write is refused for what
// it breaks and not for what was broken before it, else one bad action would
// lock the config screen against every other edit.
func problemsAdded(before, after *Config) error {
	had := map[string]bool{}
	for _, err := range before.Problems {
		had[err.Error()] = true
	}
	var out []error
	for _, err := range after.Problems {
		if !had[err.Error()] {
			out = append(out, err)
		}
	}
	return errors.Join(out...)
}

// File is config.toml under a configuration root.
func File(root string) string {
	return filepath.Join(root, "config.toml")
}

// parse decodes and validates the text of config.toml. Only text that does
// not decode is an error; every rule it breaks is one of Problems. Load and
// the writers all run it, and a writer refuses what adds to Problems, so a
// value written is one Load uses at the next start.
func parse(data []byte) (*Config, error) {
	cfg := &Config{}
	if _, err := toml.Decode(string(data), cfg); err != nil {
		return nil, err
	}
	probes := cfg.Probes[:0:0]
	for i, pr := range cfg.Probes {
		switch {
		case pr.Name == "":
			cfg.Problems = append(cfg.Problems, fmt.Errorf("probe %d: needs a name", i+1))
			continue
		case pr.Exec == "":
			cfg.Problems = append(cfg.Problems, fmt.Errorf("probe %q: needs an exec", pr.Name))
			continue
		}
		pr.Exec = probeExec(pr.Exec)
		probes = append(probes, pr)
	}
	cfg.Probes = probes

	// A name that does not resolve is drawn with the default, so the
	// surface still opens and says what it did not use.
	if _, err := theme.Lookup(cfg.UI.Theme, ""); err != nil {
		cfg.Problems = append(cfg.Problems, fmt.Errorf("ui.theme: %w", err))
		cfg.UI.Theme = ""
	}
	if _, err := theme.Lookup("", cfg.UI.Glyphs); err != nil {
		cfg.Problems = append(cfg.Problems, fmt.Errorf("ui.glyphs: %w", err))
		cfg.UI.Glyphs = ""
	}
	// A key that does not parse stays as written: the keys commands refuse
	// it, rather than bind the default in place of the key the user chose.
	if _, err := cfg.TriggerKey(); err != nil {
		cfg.Problems = append(cfg.Problems, fmt.Errorf("ui.trigger_key: %w", err))
	}
	for i, err := range validateActions(cfg.Actions) {
		if err != nil {
			cfg.Actions[i].Refused = err
			cfg.Problems = append(cfg.Problems, err)
		}
	}
	for _, err := range validateShared(cfg.Targets) {
		if err != nil {
			cfg.Problems = append(cfg.Problems, err)
		}
	}
	refused, _ := validateAgents(cfg.Agents, false)
	for _, err := range refused {
		if err != nil {
			cfg.Problems = append(cfg.Problems, err)
		}
	}
	cfg.Problems = append(cfg.Problems, cfg.validateTerminal(refused)...)
	statuses, errs := validateStatuses(cfg.Status, false)
	cfg.statuses = statuses
	cfg.Problems = append(cfg.Problems, errs...)
	for i, err := range validateMachines(cfg.Machines) {
		if err != nil {
			cfg.Problems = append(cfg.Problems, err)
			continue
		}
		cfg.machines = append(cfg.machines, cfg.Machines[i].Host)
	}
	return cfg, nil
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

// SurfaceKeys are the chords the TUI answers to itself, ahead of any action:
// moving, opening, stepping through sections, going to the targets, switching
// between the projects and the agents, going back, quitting, editing, closing
// and deleting, opening the log, and the action bar's buttons. Typed text is not here: an
// action is refused that key already.
var SurfaceKeys = []core.Chord{
	"up", "down", "ctrl+p", "ctrl+n", "home", "end", "pgup", "pgdown",
	"enter", "tab", "shift+tab", "alt+t", "alt+space", "esc", "ctrl+c",
	"alt+e", "delete", "alt+delete", "alt+d", "alt+l",
	"alt+n", "alt+r", "alt+s", "alt+q", "alt+c", "alt+a", "alt+h",
}

// validateActions refuses an action the TUI can never run, one error per
// action, parallel to actions and nil where the action is sound. An action's
// key is the TUI's alone - no desktop binding carries it - so a key the
// terminal does not deliver, one the filter takes as typed text, or one the
// TUI takes for itself does nothing, and the only place to say so is here.
func validateActions(actions []Action) []error {
	out := make([]error, len(actions))
	for i, act := range actions {
		var errs []error
		if len(act.Run) == 0 {
			errs = append(errs, fmt.Errorf("action %q runs nothing", act.Name))
		}
		chord, err := core.ParseChord(act.Key)
		if err != nil {
			out[i] = errors.Join(append(errs, fmt.Errorf("action %q: %w", act.Name, err))...)
			continue
		}
		// An action is matched on the press the TUI folds, which is the
		// same in every terminal.
		sent, ok := chord.Terminal(false)
		switch {
		case chord.Typed():
			errs = append(errs, fmt.Errorf("action %q: key %q is typed text, which the TUI filters on; give it ctrl or alt", act.Name, act.Key))
		case !ok:
			errs = append(errs, fmt.Errorf("action %q: key %q never reaches a terminal", act.Name, act.Key))
		case sent != chord:
			errs = append(errs, fmt.Errorf("action %q: key %q reaches a terminal as %s; bind that instead", act.Name, act.Key, sent))
		case slices.Contains(SurfaceKeys, chord):
			errs = append(errs, fmt.Errorf("action %q: key %q is one of revier's own keys; give it another", act.Name, act.Key))
		}
		out[i] = errors.Join(errs...)
	}
	return out
}

// LoadProjects reads every *.toml in dir, sorted by name so ordering is stable
// across machines. shared are config.toml's shared targets.
func LoadProjects(dir string, inh Inherited) ([]core.Project, error) {
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
	for _, name := range names {
		projects = append(projects, LoadProject(filepath.Join(dir, name), inh))
	}
	return projects, nil
}

// LoadProject reads, validates, and prepares one project file. It always
// returns a project: one whose file could not be read or parsed, or which a
// project-wide rule refused, comes back carrying the reason in Invalid, and
// one whose targets were refused carries a reason on each. shared are
// config.toml's shared targets, merged in before anything is checked.
//
// Nothing here returns an error, because there is no caller a project file's
// own mistake should stop. The surface lists what loaded and says what did
// not; `revier doctor` is where the reasons are read in full.
func LoadProject(path string, inh Inherited) core.Project {
	data, err := os.ReadFile(path)
	if err != nil {
		// The error names the file already.
		return invalid(path, err)
	}
	return loadProject(path, data, inh)
}

// invalid is the project a file that could not be read or parsed still
// produces: its name, its file, and why. It declares no target, so every
// operation on it fails closed with the reason.
func invalid(path string, err error) core.Project {
	p := core.PrepareProject(revier.Project{Name: nameOf(path)})
	p.File, p.Invalid = path, err
	return p
}

// loadProject is LoadProject on the text the file at path has, or is about
// to have.
func loadProject(path string, data []byte, inh Inherited) core.Project {
	p, parts, err := decodeProject(data, inh.Targets)
	if err != nil {
		return invalid(path, fmt.Errorf("%s: %w", path, err))
	}
	p.Name = nameOf(path)
	// A path is expanded once, here, so every consumer - templates, working
	// directories, the cwd lookup - sees an absolute path and none of them
	// hands a literal "~" to a program that does not expand it. A link's
	// path is the host's, kept as written: the home directory here says
	// nothing about the one there.
	if p.Remote == nil {
		p.Path = ExpandHome(p.Path)
	} else {
		link(&p)
	}
	for _, t := range p.Targets {
		for _, r := range []*revier.Realization{t.Window, t.Runtime} {
			if r != nil {
				r.Dir = ExpandHome(r.Dir)
			}
		}
	}
	agents, declared, agentRefused := agentsOf(data, inh.Agents, p.Remote != nil)
	named := nameAgents(&p, agents, declared)
	prepared := core.PrepareProject(p)
	prepared.File = path
	prepared.Status, prepared.StatusRefused = projectStatuses(data)
	var unrendered []error
	prepared.Agents, unrendered = renderAgents(p, agents)
	prepared.AgentRefused = append(agentRefused, unrendered...)
	if err := validateProject(p); err != nil {
		prepared.Invalid = fmt.Errorf("%s: %w", path, err)
	}
	// A rule's refusal wins over a rendering failure of the same target: both
	// name the same mistake, and the rule says which key to look for in the
	// file, where the template only says that it rendered to nothing. A part
	// of the other kind is a rule about the target too, and so is a panel
	// that names no agent the project can run.
	for i, err := range validateTargets(p, prepared) {
		name := p.Targets[i].Name
		if part := parts[name]; part != nil {
			err = errors.Join(part, err)
		}
		if bad := named[name]; bad != nil {
			err = errors.Join(err, bad)
		}
		if err != nil {
			prepared.Refuse(i, err)
		}
	}
	return prepared
}

// Problems is every reason a loaded project is not whole: the project-wide
// refusal first, then one per refused target, then one per refused
// [[status]] table, then one per refused [[agent]] table or key. It is
// empty for a project that loaded entire, which is what `revier new` asks
// before it keeps a file.
func Problems(p core.Project) []error {
	var out []error
	if p.Invalid != nil {
		out = append(out, p.Invalid)
	}
	for i := range p.Targets {
		if err := p.TargetErr(i); err != nil {
			out = append(out, err)
		}
	}
	out = append(out, p.StatusRefused...)
	return append(out, p.AgentRefused...)
}

// newProblems is what after is not whole for that before was: the reasons a
// write is refused for. A write is refused for what it breaks and not for
// what was broken before it, else one unfixed target would lock every other
// edit of the file, and the file could only be repaired by hand. A problem
// is the project's, one target's by name, or one status place's or named
// agent's: the opening of its refusal, since a TOML error in the tables
// names a line, and an edit above them moves it.
func newProblems(before, after core.Project) []error {
	had := map[string]bool{}
	if before.Invalid != nil {
		had["project"] = true
	}
	for i, t := range before.Targets {
		if before.TargetErr(i) != nil {
			had["target "+string(t.Name)] = true
		}
	}
	var out []error
	if after.Invalid != nil && !had["project"] {
		out = append(out, after.Invalid)
	}
	for i, t := range after.Targets {
		if err := after.TargetErr(i); err != nil && !had["target "+string(t.Name)] {
			out = append(out, err)
		}
	}
	place := func(err error) string {
		what, _, _ := strings.Cut(err.Error(), ":")
		return what
	}
	for _, err := range slices.Concat(before.StatusRefused, before.AgentRefused) {
		had[place(err)] = true
	}
	for _, err := range slices.Concat(after.StatusRefused, after.AgentRefused) {
		if !had[place(err)] {
			out = append(out, err)
		}
	}
	return out
}

// nameOf is the name of the project a file holds: the file's name without
// its extension (decisions.md D80).
func nameOf(path string) revier.ProjectName {
	return revier.ProjectName(strings.TrimSuffix(filepath.Base(path), ".toml"))
}

// Validate is every structural rule at once: the project-wide ones and every
// target's, joined. It answers "is this project whole", of a project that is
// not yet prepared.
//
// Nothing in revier asks that question any more. Loading asks each rule the
// narrower one - does this refuse the project or one target - through
// validateProject and validateTargets, and a write path asks Problems of the
// prepared project, which also covers the templates and patterns Validate
// cannot see. This is kept as the one entry point the rules are pinned
// through.
func Validate(p revier.Project) error {
	errs := []error{validateProject(p)}
	return errors.Join(append(errs, validateTargets(p, core.PrepareProject(p))...)...)
}

// validateProject holds the rules that refuse the project as a whole: there is
// nowhere to run, no way to address it, or nothing to open. A project they
// refuse is still loaded and listed, carrying the reason (decisions.md D85).
func validateProject(p revier.Project) error {
	var errs []error

	if p.Path == "" && p.Remote == nil {
		errs = append(errs, errors.New("project has no path"))
	}
	// A relative path names another directory for each working directory
	// revier runs in. A link's path is the host's, kept as written.
	if p.Path != "" && p.Remote == nil && !filepath.IsAbs(p.Path) {
		errs = append(errs, fmt.Errorf("path %q is not absolute; write it in full or from ~", p.Path))
	}
	if err := ValidateName(p.Name); err != nil {
		errs = append(errs, err)
	}
	if err := CanAddress(p.Name); err != nil {
		errs = append(errs, err)
	}
	if p.GitURL != "" {
		if err := ValidateGitURL(p.GitURL); err != nil {
			errs = append(errs, fmt.Errorf("git_url: %w", err))
		}
	}
	if p.Remote != nil {
		if err := validateHost(p.Remote.Host); err != nil {
			errs = append(errs, fmt.Errorf("remote.host: %w", err))
		}
	}
	homes := 0
	for _, t := range p.Targets {
		// A nameless target is refused and cannot be opened, so it is no
		// home: counting it would leave the project looking whole while
		// nothing in it can be opened or returned to. A command target is
		// refused as a home, and takes no home's place either.
		if t.Home && t.Name != "" && t.Command == nil {
			homes++
		}
	}
	if homes == 0 {
		errs = append(errs, errors.New("no target is marked home; nothing to open or return to"))
	}
	return errors.Join(errs...)
}

// validateTargets checks each target on its own, returning one error per
// target, parallel to p.Targets and nil where the target is sound. A target
// these rules refuse loses only itself: the project keeps the others, and the
// refused one reports why instead of disappearing (decisions.md D85).
// prepared is p as core.PrepareProject made it, for the rules that read a
// rendered value: the load has rendered every template once already.
func validateTargets(p revier.Project, prepared core.Project) []error {
	out := make([]error, len(p.Targets))
	seenName := map[revier.TargetName]bool{}
	// Keyed by the canonical chord, not by the text. Two targets written
	// "ctrl-o" and "Ctrl+O" are the same key, and the raw strings do not say
	// so.
	seenKey := map[core.Chord]struct {
		target revier.TargetName
		raw    string
	}{}

	// The first target marked home is the project's; a later one is refused
	// rather than making the count ambiguous for every target at once. A
	// command target marked home is refused by validateCommand and takes no
	// home's place, so the real home after it stays.
	home := false

	for i, t := range p.Targets {
		var errs []error
		if t.Name == "" {
			out[i] = errors.New("a target has no name")
			continue
		}
		if err := core.ValidateTargetName(t.Name); err != nil {
			errs = append(errs, err)
		}
		if seenName[t.Name] {
			errs = append(errs, fmt.Errorf("target %q declared twice", t.Name))
		}
		seenName[t.Name] = true

		if t.Home && t.Command == nil {
			if home {
				errs = append(errs, fmt.Errorf("target %q is marked home, and an earlier target already is; exactly one may be", t.Name))
			}
			home = true
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

		if t.Command != nil {
			errs = append(errs, validateCommand(t)...)
		}
		switch {
		case t.Window != nil || t.Runtime != nil || t.Command != nil:
		case p.Remote != nil:
			// A link's part is under [target.remote]; a local part was
			// dropped and refused by validateParts, and is not the fix.
			errs = append(errs, fmt.Errorf("target %q declares no realization under [target.remote.window] or [target.remote.runtime]", t.Name))
		default:
			errs = append(errs, fmt.Errorf("target %q declares no realization", t.Name))
		}
		tab := t.Runtime != nil && t.Runtime.Inside != ""
		if tab {
			errs = append(errs, validateTab(p, t)...)
		}
		if t.Window != nil && t.Window.Inside != "" {
			// Only a runtime opens tabs. Ignored, it would open a window of its
			// own, which is the choice inside was written to refuse.
			errs = append(errs, fmt.Errorf("target %q window realization is inside %q; only a runtime realization can be a tab", t.Name, t.Window.Inside))
		}
		for kind, r := range map[revier.HostKind]*revier.Realization{
			revier.HostWindow: t.Window, revier.HostRuntime: t.Runtime,
		} {
			if r == nil {
				continue
			}
			errs = append(errs, validateTabs(p, t, kind, r)...)
			if tab && kind == revier.HostRuntime {
				// A tab is found by its target's name, so it needs no match
				// and no name; validateTab has checked the rest.
				continue
			}
			if r.Match.IsZero() {
				// An unconstrained match selects whichever instance the host
				// happens to list first, so run-or-raise would raise a random
				// window.
				errs = append(errs, fmt.Errorf("target %q %s realization has an empty match", t.Name, kind))
			}
			if len(r.Launch) == 0 && len(r.Panels) == 0 && len(r.Tabs) == 0 {
				errs = append(errs, nothingToStart(p, t, kind))
			}
			if len(r.Panels) > 0 && kind == revier.HostWindow {
				errs = append(errs, fmt.Errorf("target %q window realization declares panels; only a runtime has them", t.Name))
			}
			if len(r.Panels) > 0 && kind == revier.HostRuntime {
				// Panels are what a tab starts, and a realization that is no
				// tab has none (decisions.md D128).
				errs = append(errs, panelsOffATab(p, t))
			}
			if r.Name == "" && kind == revier.HostRuntime {
				// A runtime host gives the instance it opens this name, and
				// has no other identity to give it; both refuse to open
				// without one. A window host needs none: its launch argv
				// carries the identity match finds.
				errs = append(errs, fmt.Errorf("target %q runtime realization has no name; give it the name its match finds", t.Name))
			}
			// A geometry short of its four tokens would reach the window host
			// and be refused there, after the window had opened. place takes
			// templates, so the tokens counted are the rendered ones; a target
			// that did not prepare is refused by that instead, and its place
			// may not have rendered.
			if place := renderedPlace(prepared.Targets[i], kind); prepared.TargetErr(i) == nil && place != "" && len(strings.Fields(place)) != 4 {
				errs = append(errs, fmt.Errorf("target %q %s realization: place needs four tokens, x y width height, got %q", t.Name, kind, place))
			}
		}
		if t.Prefer != "" && t.Prefer != revier.HostWindow && t.Prefer != revier.HostRuntime {
			errs = append(errs, fmt.Errorf("target %q: prefer must be %q or %q, got %q",
				t.Name, revier.HostWindow, revier.HostRuntime, t.Prefer))
		}
		out[i] = errors.Join(errs...)
	}
	return out
}

// renderedPlace is the place of t's realization of kind, as rendered.
func renderedPlace(t revier.Target, kind revier.HostKind) string {
	r := t.Runtime
	if kind == revier.HostWindow {
		r = t.Window
	}
	if r == nil {
		return ""
	}
	return r.Place
}

// validateCommand checks a target that runs a command (decisions.md D166).
// The command does the run-or-raise in place of a host, so the target has no
// realization beside it, no host to prefer, and is no home: a home is an
// instance the others open in and a second press returns to.
func validateCommand(t revier.Target) []error {
	var errs []error
	if len(t.Command.Run) == 0 {
		errs = append(errs, fmt.Errorf("target %q command has no run argv", t.Name))
	}
	if t.Window != nil || t.Runtime != nil {
		errs = append(errs, fmt.Errorf("target %q has a command and a realization; a command target has neither a window nor a runtime realization", t.Name))
	}
	if t.Home {
		errs = append(errs, fmt.Errorf("target %q runs a command and is home; a home is an instance the others open in", t.Name))
	}
	if t.Prefer != "" {
		errs = append(errs, fmt.Errorf("target %q runs a command and has prefer; a command needs no host", t.Name))
	}
	if _, err := core.RunTimeout(*t.Command); err != nil {
		errs = append(errs, fmt.Errorf("target %q: %w", t.Name, err))
	}
	return errs
}

// nothingToStart refuses a realization of t with no launch and no tabs. On a
// link the reason is the link's: the home alone has a list of tabs, the
// derived one (decisions.md D128). A home with the name of the derived tab
// gets none; one that also has a window gets none under any name.
func nothingToStart(p revier.Project, t revier.Target, kind revier.HostKind) error {
	if p.Remote != nil && kind == revier.HostRuntime {
		switch {
		case !t.Home:
			return fmt.Errorf("target %q runtime realization has no launch argv, and on a link only the home has tabs", t.Name)
		case t.Name == agentTab && t.Window == nil:
			return fmt.Errorf("target %q is the home of a link, and %q is the name of the tab a link opens with; give the home another name", t.Name, agentTab)
		}
	}
	return fmt.Errorf("target %q %s realization has no launch argv and no tabs", t.Name, kind)
}

// panelsOffATab refuses panels on a runtime realization of t that is no tab,
// with the move that repairs it. On a link the move is another one: the home
// lists the tab named agent alone, and no other target lists tabs. A home
// that also has a window opens no tab, and a home named agent cannot hold the
// tab of that name, so neither is told to move panels into one.
func panelsOffATab(p revier.Project, t revier.Target) error {
	const what = "target %q runtime realization declares panels; only a tab has them, "
	if p.Remote == nil {
		return fmt.Errorf(what+"so move them into a target with inside = %q and list it in tabs", t.Name, t.Name)
	}
	home, ok := p.Home()
	switch {
	case !ok || home.Runtime == nil:
		return fmt.Errorf(what+"and the home of this link has no runtime realization to open a tab in", t.Name)
	case home.Window != nil:
		return fmt.Errorf(what+"and the home of this link also has a window realization, so no tab opens in it", t.Name)
	case t.Name != home.Name:
		return fmt.Errorf(what+"and on a link only the home has tabs, so move them into a target with inside = %q", t.Name, home.Name)
	case t.Name == agentTab:
		return fmt.Errorf(what+"and a link opens with the tab %q, so give the home another name, and move them into a target named %q inside it", t.Name, agentTab, agentTab)
	}
	return fmt.Errorf(what+"and a link opens with the tab %q, so move them into a target named %q with inside = %q", t.Name, agentTab, agentTab, t.Name)
}

// validateTab checks a target declared inside another. Each rule is a tab
// that would fail at the keypress: one with nothing to run, one inside a
// target that is not an instance of the runtime, or one that is home itself.
func validateTab(p revier.Project, t revier.Target) []error {
	var errs []error
	r := t.Runtime
	if t.Home {
		errs = append(errs, fmt.Errorf("target %q is home and inside %q; home is what the others open in", t.Name, r.Inside))
	}
	if t.Window != nil {
		errs = append(errs, fmt.Errorf("target %q is inside %q and has a window realization; a tab has only the runtime", t.Name, r.Inside))
	}
	if len(r.Launch) == 0 && len(r.Panels) == 0 {
		errs = append(errs, fmt.Errorf("target %q is inside %q and has no launch argv and no panels to run in the tab", t.Name, r.Inside))
	}
	if len(r.Launch) > 0 && len(r.Panels) > 0 {
		errs = append(errs, fmt.Errorf("target %q is inside %q and has both launch and panels; panels are what is launched, so drop launch", t.Name, r.Inside))
	}
	for i, panel := range r.Panels {
		// A kind left out is a tool. Any other unknown kind is a typo that
		// would cost an agent panel its role: no resume, no remote command,
		// no copy by `revier agent new`.
		switch panel.Kind {
		case "", revier.PanelAgent, revier.PanelShell, revier.PanelTool:
		default:
			errs = append(errs, fmt.Errorf("target %q: panel %d has kind %q; use agent, shell or tool", t.Name, i+1, panel.Kind))
		}
	}
	if r.Inside == t.Name {
		return append(errs, fmt.Errorf("target %q is inside itself", t.Name))
	}
	in, ok := p.Target(r.Inside)
	switch {
	case !ok:
		errs = append(errs, fmt.Errorf("target %q is inside %q, which the project does not declare", t.Name, r.Inside))
	case in.Command != nil:
		errs = append(errs, fmt.Errorf("target %q is inside %q, which runs a command and opens no tab", t.Name, r.Inside))
	case in.Runtime == nil:
		errs = append(errs, fmt.Errorf("target %q is inside %q, which has no runtime realization to open a tab in", t.Name, r.Inside))
	case in.Runtime.Inside != "":
		errs = append(errs, fmt.Errorf("target %q is inside %q, which is itself a tab", t.Name, r.Inside))
	case in.Window != nil:
		errs = append(errs, fmt.Errorf("target %q is inside %q, which also has a window realization; the tab needs it on the runtime", t.Name, r.Inside))
	}
	return errs
}

// validateTabs checks the tabs and the active tab a realization of t
// declares (decisions.md D126). Each rule is a list the open path could not
// follow: an entry that is no tab of this target, or a list on a realization
// that opens something else.
func validateTabs(p revier.Project, t revier.Target, kind revier.HostKind, r *revier.Realization) []error {
	if len(r.Tabs) == 0 {
		if r.Active != "" {
			return []error{fmt.Errorf("target %q %s realization has active %q and no tabs; active names an entry of tabs", t.Name, kind, r.Active)}
		}
		return nil
	}
	if kind == revier.HostWindow {
		return []error{fmt.Errorf("target %q window realization declares tabs; only a runtime has them", t.Name)}
	}
	if r.Inside != "" {
		return []error{fmt.Errorf("target %q is inside %q and declares tabs; a tab holds no tabs", t.Name, r.Inside)}
	}
	var errs []error
	if len(r.Launch) > 0 {
		// The first tab is what the instance opens with, so a launch beside
		// the list would have no tab to run in.
		errs = append(errs, fmt.Errorf("target %q runtime realization has tabs beside launch; the tabs are what is launched, so move it into a tab target", t.Name))
	}
	seen := map[revier.TargetName]bool{}
	for _, name := range r.Tabs {
		if seen[name] {
			errs = append(errs, fmt.Errorf("target %q lists tab %q twice", t.Name, name))
			continue
		}
		seen[name] = true
		entry, ok := p.Target(name)
		switch {
		case !ok:
			errs = append(errs, fmt.Errorf("target %q lists tab %q, which the project does not declare", t.Name, name))
		case p.Remote != nil && name == agentTab && (entry.Runtime == nil || entry.Runtime.Inside != t.Name):
			// The list is the derived one, so the fix is on the other target.
			errs = append(errs, fmt.Errorf("target %q opens a link with the tab %q, and target %q is not a tab of it; give that target another name, or inside = %q", t.Name, name, name, t.Name))
		case entry.Runtime == nil || entry.Runtime.Inside != t.Name:
			errs = append(errs, fmt.Errorf("target %q lists tab %q, which is not inside it; give that target inside = %q", t.Name, name, t.Name))
		}
	}
	if r.Active != "" && !seen[r.Active] {
		errs = append(errs, fmt.Errorf("target %q has active %q, which is not an entry of its tabs", t.Name, r.Active))
	}
	return errs
}

// ValidateGitURL refuses a clone URL that is unsafe to hand to git or to keep
// in a file shared between machines: no whitespace, no control characters,
// no shell metacharacters, and no credentials in an http or https URL.
//
// A URL with credentials is not echoed back: the message would print the
// token the rule exists to keep out of the file.
func ValidateGitURL(u string) error {
	if u == "" {
		return errors.New("empty")
	}
	if httpsUserinfo.MatchString(u) {
		return errors.New("an http or https URL with credentials in it is refused; record it without the user part")
	}
	if err := oneWord(u); err != nil {
		return err
	}
	if strings.ContainsAny(u, "`\"'\\$;|&<>(){}") {
		return fmt.Errorf("%q contains a shell metacharacter", u)
	}
	return nil
}

// validateHost refuses a host ssh would read as something other than a
// destination. The name is handed to ssh as one argument after "--", so a
// shell character is harmless; a space or a control character is a name no
// ssh config holds, and a leading dash is a flag.
func validateHost(h string) error {
	if strings.HasPrefix(h, "-") {
		return fmt.Errorf("%q starts with a dash, which ssh reads as a flag", h)
	}
	return oneWord(h)
}

// oneWord refuses whitespace and control characters: a value handed to a
// program as one argument, and shown back in a message, holds neither.
func oneWord(s string) error {
	switch {
	case strings.IndexFunc(s, unicode.IsSpace) >= 0:
		return fmt.Errorf("%q contains whitespace", s)
	case strings.IndexFunc(s, func(r rune) bool { return !unicode.IsPrint(r) }) >= 0:
		return fmt.Errorf("%q contains a control character", s)
	}
	return nil
}

// httpsUserinfo is an http or https authority with a user part in it:
// https://user:token@host/...
var httpsUserinfo = regexp.MustCompile(`^https?://[^/]+@`)

// probeExec is the binary a probe runs. A bare name is looked up on PATH when
// the probe runs. Any other relative path is read against the home directory
// rather than against the directory a command runs in, which is the home
// directory in the TUI and wherever the user is in the CLI.
func probeExec(exec string) string {
	exec = ExpandHome(exec)
	if filepath.IsAbs(exec) || !strings.ContainsRune(exec, filepath.Separator) {
		return exec
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return exec
	}
	return filepath.Join(home, exec)
}

// ExpandHome resolves a leading "~" against the user's home directory, so a
// path typed the way it is spoken reaches the file system.
func ExpandHome(p string) string {
	if p != "~" && !strings.HasPrefix(p, "~/") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	return filepath.Join(home, strings.TrimPrefix(p, "~"))
}
