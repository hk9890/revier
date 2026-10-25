package config_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/pkg/revier"
)

// agentsConfig names two agents and a shared workspace whose tab runs the
// first by name.
const agentsConfig = `
[[agent]]
name = "claude"
command = ["claude", "--name", "{{.Name}}"]
key = "ctrl+shift+c"

[[agent]]
name = "dtop"
command = ["dtop"]

[[target]]
name = "home"
home = true
  [target.runtime]
  name = "session:{{.Name}}"
  match = { title = "^session:{{.Name}}$" }
  tabs = ["agent"]

[[target]]
name = "agent"
  [target.runtime]
  inside = "home"
    [[target.runtime.panels]]
    kind = "agent"
    agent = "claude"
    [[target.runtime.panels]]
    kind = "shell"
`

// loadOne loads config.toml and one project file, demo.toml.
func loadOne(t *testing.T, cfgText, body string) (*config.Config, core.Project) {
	t.Helper()
	root := projectsRoot(t, cfgText, map[string]string{"demo.toml": body})
	cfg, projects, err := config.Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(projects) != 1 {
		t.Fatalf("Load: %d projects, want 1", len(projects))
	}
	return cfg, projects[0]
}

func agentNames(p core.Project) []string {
	var out []string
	for _, a := range p.Agents {
		out = append(out, a.Name)
	}
	return out
}

// Every project has config.toml's agents, each command rendered for it, and a
// panel that names one runs that agent's command under its name.
func TestAProjectInheritsTheNamedAgents(t *testing.T) {
	cfg, p := loadOne(t, agentsConfig, "path = \"/tmp/demo\"\n")
	if len(cfg.Problems) > 0 {
		t.Fatalf("Problems = %v", cfg.Problems)
	}
	if problems := config.Problems(p); len(problems) > 0 {
		t.Fatalf("project problems = %v", problems)
	}
	want := []core.NamedAgent{
		{Name: "claude", Command: []string{"claude", "--name", "demo"}},
		{Name: "dtop", Command: []string{"dtop"}},
	}
	if !reflect.DeepEqual(p.Agents, want) {
		t.Errorf("Agents = %+v, want %+v", p.Agents, want)
	}
	panel := target(t, p, "agent").Runtime.Panels[0]
	if !reflect.DeepEqual(panel.Command, []string{"claude", "--name", "demo"}) || panel.Title != "claude" {
		t.Errorf("panel = %+v, want the claude agent's command under its name", panel)
	}
}

// A project file's table of an inherited name changes the values it writes
// and keeps the others; a new name is the project's alone, after the
// inherited ones. A panel's own title stays.
func TestAProjectOverridesAnAgentByName(t *testing.T) {
	_, p := loadOne(t, agentsConfig, `path = "/tmp/demo"

[[agent]]
name = "dtop"
command = ["dtop", "--fast"]

[[agent]]
name = "reviewer"
command = ["claude", "--agent", "review"]

[[target]]
name = "agent"
  [target.runtime]
  inside = "home"
    [[target.runtime.panels]]
    kind = "agent"
    agent = "reviewer"
    title = "review"
`)
	if problems := config.Problems(p); len(problems) > 0 {
		t.Fatalf("project problems = %v", problems)
	}
	if got := agentNames(p); !reflect.DeepEqual(got, []string{"claude", "dtop", "reviewer"}) {
		t.Errorf("agents = %v, want claude, dtop, reviewer", got)
	}
	if got := p.Agents[1].Command; !reflect.DeepEqual(got, []string{"dtop", "--fast"}) {
		t.Errorf("dtop = %v, want the project's command", got)
	}
	panel := target(t, p, "agent").Runtime.Panels[0]
	if panel.Title != "review" || !reflect.DeepEqual(panel.Command, []string{"claude", "--agent", "review"}) {
		t.Errorf("panel = %+v, want reviewer's command under its own title", panel)
	}
}

// A key in a project file is refused, and costs the agent nothing else.
func TestAKeyInAProjectFileIsRefusedAlone(t *testing.T) {
	_, p := loadOne(t, agentsConfig, `path = "/tmp/demo"

[[agent]]
name = "dtop"
key = "ctrl+shift+d"
`)
	if len(p.AgentRefused) != 1 || !strings.Contains(p.AgentRefused[0].Error(), "config.toml alone") {
		t.Errorf("AgentRefused = %v, want the key refused", p.AgentRefused)
	}
	if got := agentNames(p); !reflect.DeepEqual(got, []string{"claude", "dtop"}) {
		t.Errorf("agents = %v, want both kept", got)
	}
}

// A panel that names no agent the project can run refuses its target, and
// says which panel and why.
func TestAPanelNamingNoUsableAgentRefusesItsTarget(t *testing.T) {
	for _, tc := range []struct{ panel, want string }{
		{`kind = "agent"
    agent = "nobody"`, `names agent "nobody", which no [[agent]] table declares`},
		{`kind = "agent"
    agent = "dtop"
    command = ["dtop"]`, `names agent "dtop" and writes a command; drop one`},
		{`kind = "shell"
    agent = "dtop"`, `names agent "dtop" and has kind "shell"; give it kind = "agent"`},
		{`kind = "agent"
    agent = "empty"`, `names agent "empty", which is refused`},
	} {
		_, p := loadOne(t, agentsConfig, `path = "/tmp/demo"

[[agent]]
name = "empty"

[[target]]
name = "agent"
  [target.runtime]
  inside = "home"
    [[target.runtime.panels]]
    `+tc.panel+`
`)
		err := refusalOf(p, "agent")
		if err == nil || !strings.Contains(err.Error(), "panel 1 "+tc.want) {
			t.Errorf("panel %q: refusal = %v, want %q", tc.panel, err, tc.want)
		}
		if refusalOf(p, "home") != nil {
			t.Errorf("panel %q: home refused too: %v", tc.panel, refusalOf(p, "home"))
		}
	}
}

// config.toml refuses a table no project could use, and that table reaches
// no project; the others still do.
func TestARefusedSharedAgentReachesNoProject(t *testing.T) {
	cfg, p := loadOne(t, `
[[agent]]
command = ["nameless"]

[[agent]]
name = "dtop"
command = ["dtop"]

[[agent]]
name = "dtop"
command = ["dtop", "again"]

[[agent]]
name = "idle"
`, "path = \"/tmp/demo\"\n")
	var got []string
	for _, err := range cfg.Problems {
		got = append(got, err.Error())
	}
	want := []string{`agent 1: has no name`, `agent "dtop": declared twice`, `agent "idle": has no command`}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Problems = %q, want %q", got, want)
	}
	if !reflect.DeepEqual(p.Agents, []core.NamedAgent{{Name: "dtop", Command: []string{"dtop"}}}) {
		t.Errorf("Agents = %+v, want the first dtop alone", p.Agents)
	}
}

// A name that is not one word of a command line, or that reads as a flag,
// is refused: a terminal key passes it to `revier agent new` in a sh -c
// string.
func TestAnAgentNameMustBeOneWordThatIsNoFlag(t *testing.T) {
	cfg, p := loadOne(t, `
[[agent]]
name = "-p"
command = ["x"]

[[agent]]
name = "two words"
command = ["x"]

[[agent]]
name = "it's"
command = ["x"]

[[agent]]
name = "dtop-2_b.v"
command = ["dtop"]
`, "path = \"/tmp/demo\"\n")
	var got []string
	for _, err := range cfg.Problems {
		got = append(got, err.Error())
	}
	rule := ": a name is letters, digits, '-', '_' and '.', and does not start with '-'"
	want := []string{`agent "-p"` + rule, `agent "two words"` + rule, `agent "it's"` + rule}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Problems = %q, want %q", got, want)
	}
	if len(p.Agents) != 1 || p.Agents[0].Name != "dtop-2_b.v" {
		t.Errorf("Agents = %+v, want dtop-2_b.v alone", p.Agents)
	}
}

// A project file's own table with no command, a name given twice, or a
// command that does not render is refused by name, and the project keeps
// the rest.
func TestAProjectFileAgentIsRefusedByName(t *testing.T) {
	_, p := loadOne(t, tabsConfig, `path = "/tmp/demo"

[[agent]]
name = "a"
command = ["a"]

[[agent]]
name = "a"
command = ["again"]

[[agent]]
name = "b"

[[agent]]
name = "c"
command = ["c", "{{.Vars.missing}}"]
`)
	var got []string
	for _, err := range p.AgentRefused {
		what, _, _ := strings.Cut(err.Error(), ":")
		got = append(got, what)
	}
	if want := []string{`agent "a"`, `agent "b"`, `agent "c"`}; !reflect.DeepEqual(got, want) {
		t.Errorf("AgentRefused = %v, want %q", p.AgentRefused, want)
	}
	if !reflect.DeepEqual(p.Agents, []core.NamedAgent{{Name: "a", Command: []string{"a"}}}) {
		t.Errorf("Agents = %+v, want the first a alone", p.Agents)
	}
	if len(config.Problems(p)) != 3 {
		t.Errorf("Problems = %v, want the three refusals", config.Problems(p))
	}
}

// A link runs its host's agents: it inherits none, a table of its own is
// refused, and a panel's name is left for the host to resolve.
func TestALinkHasNoNamedAgents(t *testing.T) {
	_, p := loadOne(t, agentsConfig, link+`
[[agent]]
name = "mine"
command = ["mine"]

[[target]]
name = "agent"
  [target.remote.runtime]
  inside = "home"
    [[target.remote.runtime.panels]]
    kind = "agent"
    agent = "dtop"
`)
	if len(p.Agents) != 0 {
		t.Errorf("Agents = %+v, want none", p.Agents)
	}
	if len(p.AgentRefused) != 1 || !strings.Contains(p.AgentRefused[0].Error(), "agents of its host") {
		t.Errorf("AgentRefused = %v, want the link's own table refused", p.AgentRefused)
	}
	panel := target(t, p, "agent").Runtime.Panels[0]
	if panel.Agent != "dtop" || len(panel.Command) != 0 {
		t.Errorf("panel = %+v, want the name kept and no command", panel)
	}
	if err := refusalOf(p, "agent"); err != nil {
		t.Errorf("agent refused: %v", err)
	}
}

// The assistant revier builds in has no named agents of config.toml's.
func TestTheAssistantInheritsNoAgent(t *testing.T) {
	if p := config.Builtin(t.TempDir(), []string{"revier", "assist", "--exec"}); len(p.Agents) != 0 {
		t.Errorf("Agents = %+v, want none", p.Agents)
	}
}

// The config screen writes a panel's agent, and the file loads with it.
func TestReplaceTargetWritesAPanelsAgent(t *testing.T) {
	root, shared := targetsRoot(t, agentsConfig)
	tab := sharedTyped(t, shared)[1]
	tab.Runtime.Panels = append(tab.Runtime.Panels, revier.PanelSpec{Kind: revier.PanelAgent, Agent: "dtop"})
	tab.Runtime.Panels[0].Agent = "dtop"
	written, err := config.ReplaceTarget(root, 1, shared, config.TargetEdit{Target: tab, PanelFrom: []int{0, 1, -1}})
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(agentsConfig, `    agent = "claude"
    [[target.runtime.panels]]
    kind = "shell"
`, `    agent = "dtop"
    [[target.runtime.panels]]
    kind = "shell"
    [[target.runtime.panels]]
    kind = "agent"
    agent = "dtop"
`, 1)
	if got := readConfig(t, root); got != want {
		t.Errorf("config.toml =\n%s\nwant\n%s", got, want)
	}
	if got := config.ListTargets(written.Shared)[1].Runtime.Panels; !reflect.DeepEqual(got, tab.Runtime.Panels) {
		t.Errorf("panels read back = %+v, want %+v", got, tab.Runtime.Panels)
	}
}

// A write that leaves a panel naming no agent is refused for the project it
// breaks.
func TestATargetEditNamingAnUnknownAgentIsRefused(t *testing.T) {
	root, shared := targetsRoot(t, agentsConfig)
	tab := sharedTyped(t, shared)[1]
	tab.Runtime.Panels[0].Agent = "nobody"
	_, err := config.ReplaceTarget(root, 1, shared, config.TargetEdit{Target: tab, PanelFrom: []int{0, 1}})
	if err == nil || !strings.Contains(err.Error(), `"nobody"`) {
		t.Errorf("ReplaceTarget = %v, want the unknown agent named", err)
	}
	if got := readConfig(t, root); got != agentsConfig {
		t.Errorf("config.toml changed:\n%s", got)
	}
}

// The project screen writes a panel's agent into the project file, and the
// project loads with the inherited agent's command in it.
func TestSaveProjectTargetWritesAPanelsAgent(t *testing.T) {
	root := projectsRoot(t, agentsConfig, map[string]string{"demo.toml": "path = \"/tmp/demo\"\n"})
	cfg, _, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	inh := cfg.Inherited()
	file := config.ProjectFile(root, "demo")
	text, err := config.ReadProject(file, inh)
	if err != nil {
		t.Fatal(err)
	}
	tab := text.Targets[1].Target
	tab.Runtime.Panels[0].Agent = "dtop"
	p, err := config.SaveProjectTarget(file, "agent", config.TargetEdit{Target: tab, PanelFrom: []int{0, 1}}, inh)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(read(t, file), `agent = "dtop"`) {
		t.Errorf("demo.toml =\n%s\nwant the panel's agent", read(t, file))
	}
	if got := target(t, p, "agent").Runtime.Panels[0].Command; !reflect.DeepEqual(got, []string{"dtop"}) {
		t.Errorf("panel command = %v, want dtop's", got)
	}
	if err := refusalOf(p, "agent"); err != nil {
		t.Errorf("agent refused: %v", err)
	}
}
