package config

import (
	"errors"
	"fmt"
	"regexp"
	"slices"

	"github.com/BurntSushi/toml"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/pkg/revier"
)

// A named agent is an [[agent]] table: a name, the command an agent panel
// that names it runs, and in config.toml the terminal key that opens it.
// config.toml's tables reach every local project, merged by name with the
// project file's own key by key, as a shared target is (shared.go); a name
// the file writes that config.toml does not is the project's alone. A
// project cannot remove an agent it inherits. A link inherits none: its
// agents are its host's.

// Inherited is what config.toml gives every project file, without the
// tables its rules refuse: the shared targets, which reach a link through
// their remote part, and the named agents, which reach no link.
type Inherited struct {
	Targets []map[string]any
	Agents  []map[string]any
}

// Inherited is what config.toml gives every project file.
func (c *Config) Inherited() Inherited {
	return Inherited{Targets: Usable(c.Targets), Agents: UsableAgents(c.Agents)}
}

// UsableAgents is the [[agent]] tables of config.toml a project gets: the
// ones validateAgents does not refuse.
func UsableAgents(agents []map[string]any) []map[string]any {
	refused, _ := validateAgents(agents, false)
	out := make([]map[string]any, 0, len(agents))
	for i, err := range refused {
		if err == nil {
			out = append(out, agents[i])
		}
	}
	return out
}

// agentTable is one [[agent]] table, typed.
type agentTable struct {
	Name    string   `toml:"name"`
	Command []string `toml:"command"`
	Key     string   `toml:"key"`
}

// agentName is a name a terminal key passes as one word of a command line,
// `revier agent new <name>`, that does not read as a flag.
var agentName = regexp.MustCompile(`^[A-Za-z0-9_.][A-Za-z0-9_.-]*$`)

// validateAgents checks [[agent]] tables, one error per table, parallel to
// tables and nil where the table is sound: a table with no name, a name
// that is not one word of a command line, a name given twice, or a value of the wrong type is refused whole, and so is one
// of config.toml's with no command, since no project could run it.
// inProject is a project file's tables, which may leave the command to
// config.toml's table of their name, and hold no key: the terminal's key
// file is one per machine. A key there costs only itself (decisions.md
// D85): keyErrs is one error per such key, and the agent keeps its command.
// config.toml's keys are validateTerminal's.
func validateAgents(tables []map[string]any, inProject bool) (refused, keyErrs []error) {
	refused = make([]error, len(tables))
	seen := map[string]bool{}
	for i, raw := range tables {
		name, _ := raw["name"].(string)
		if name == "" {
			refused[i] = fmt.Errorf("agent %d: has no name", i+1)
			continue
		}
		if !agentName.MatchString(name) {
			refused[i] = fmt.Errorf("agent %q: a name is letters, digits, '-', '_' and '.', and does not start with '-'", name)
			seen[name] = true
			continue
		}
		var a agentTable
		switch err := recode(raw, &a); {
		case err != nil:
			refused[i] = fmt.Errorf("agent %q: %w", name, err)
		case seen[name]:
			refused[i] = fmt.Errorf("agent %q: declared twice", name)
		case !inProject && len(a.Command) == 0:
			refused[i] = fmt.Errorf("agent %q: has no command", name)
		}
		seen[name] = true
		if inProject && refused[i] == nil && a.Key != "" {
			keyErrs = append(keyErrs, fmt.Errorf("agent %q: key is read from config.toml alone, because the terminal's keys are one file per machine; move it there", name))
		}
	}
	return refused, keyErrs
}

// agentsOf is the named agents of a project file, as written: config.toml's
// usable tables merged with the file's own, in that order, without the ones a
// rule refuses, whose reasons are the third answer. declared is every name
// the merged tables write, refused ones included. A link has none of its own
// and inherits none, and each table its file writes is refused.
func agentsOf(data []byte, inherited []map[string]any, isLink bool) (agents []agentTable, declared map[string]bool, errs []error) {
	var raw map[string]any
	if _, err := toml.Decode(string(data), &raw); err != nil {
		// decodeProject reported it, and the project is refused for it.
		return nil, nil, nil
	}
	own := tablesOf(raw["agent"])
	if isLink {
		for i, t := range own {
			errs = append(errs, fmt.Errorf("agent %s: a link runs the agents of its host; declare it in the host's config.toml", agentLabel(t, i)))
		}
		return nil, nil, errs
	}
	refused, errs := validateAgents(own, true)
	declared = map[string]bool{}
	var sound []map[string]any
	for i, err := range refused {
		if name, _ := own[i]["name"].(string); name != "" {
			declared[name] = true
		}
		if err != nil {
			errs = append(errs, err)
			continue
		}
		sound = append(sound, own[i])
	}
	for _, t := range mergeByName(inherited, sound) {
		var a agentTable
		if err := recode(t, &a); err != nil {
			errs = append(errs, fmt.Errorf("agent %q: %w", a.Name, err))
			continue
		}
		declared[a.Name] = true
		if len(a.Command) == 0 {
			errs = append(errs, fmt.Errorf("agent %q: has no command", a.Name))
			continue
		}
		agents = append(agents, a)
	}
	return agents, declared, errs
}

// agentLabel is how a refusal names the i-th table: by its name, or by its
// place when it has none.
func agentLabel(t map[string]any, i int) string {
	if name, _ := t["name"].(string); name != "" {
		return fmt.Sprintf("%q", name)
	}
	return fmt.Sprintf("%d", i+1)
}

// renderAgents is agents with each command rendered against p, as a panel's
// is, and why each that does not render is left out.
func renderAgents(p revier.Project, agents []agentTable) ([]core.NamedAgent, []error) {
	var out []core.NamedAgent
	var errs []error
	for _, a := range agents {
		cmd, err := core.RenderArgv(p, a.Command)
		if err != nil {
			errs = append(errs, fmt.Errorf("agent %q: command: %w", a.Name, err))
			continue
		}
		out = append(out, core.NamedAgent{Name: a.Name, Command: cmd})
	}
	return out, errs
}

// nameAgents gives each runtime panel of p that names an agent that agent's
// command, as the [[agent]] table writes it, so the project's rendering
// renders it as it renders a panel's own. A panel with no title takes the
// agent's name. The answer refuses, by target, a panel whose name is no agent
// of the project, names a refused one, also writes a command, or is no agent
// panel. A link resolves no name: its host does.
func nameAgents(p *revier.Project, agents []agentTable, declared map[string]bool) map[revier.TargetName]error {
	if p.Remote != nil {
		return nil
	}
	out := map[revier.TargetName]error{}
	for _, t := range p.Targets {
		if t.Runtime == nil {
			continue
		}
		var errs []error
		for i := range t.Runtime.Panels {
			panel := &t.Runtime.Panels[i]
			if panel.Agent == "" {
				continue
			}
			at := slices.IndexFunc(agents, func(a agentTable) bool { return a.Name == panel.Agent })
			switch {
			case len(panel.Command) > 0:
				errs = append(errs, fmt.Errorf("target %q: panel %d names agent %q and writes a command; drop one of the two", t.Name, i+1, panel.Agent))
			case panel.Kind != revier.PanelAgent:
				errs = append(errs, fmt.Errorf("target %q: panel %d names agent %q and has kind %q; give it kind = %q", t.Name, i+1, panel.Agent, panel.Kind, revier.PanelAgent))
			case at < 0 && declared[panel.Agent]:
				errs = append(errs, fmt.Errorf("target %q: panel %d names agent %q, which is refused", t.Name, i+1, panel.Agent))
			case at < 0:
				errs = append(errs, fmt.Errorf("target %q: panel %d names agent %q, which no [[agent]] table declares", t.Name, i+1, panel.Agent))
			default:
				panel.Command = slices.Clone(agents[at].Command)
				if panel.Title == "" {
					panel.Title = panel.Agent
				}
			}
		}
		if len(errs) > 0 {
			out[t.Name] = errors.Join(errs...)
		}
	}
	return out
}
