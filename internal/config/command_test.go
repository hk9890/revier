package config_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/pkg/revier"
)

// web is a command target, as a project file writes it after valid's
// targets.
const web = `
[[target]]
name = "web"
key = "ctrl-shift-i"
  [target.command]
  run = ["repo-web", "{{.Name}}", "{{.Vars.url}}"]
`

// loadDemo is the one project a configuration root with no config.toml and
// the project file body loads to.
func loadDemo(t *testing.T, body string) core.Project {
	t.Helper()
	_, loaded, err := config.Load(projectsRoot(t, "", map[string]string{"demo.toml": body}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return loaded[0]
}

// A command target loads with its run argv rendered, and no timeout is the
// default.
func TestACommandTargetLoads(t *testing.T) {
	p := loadDemo(t, valid+web)
	if probs := config.Problems(p); len(probs) > 0 {
		t.Fatalf("Problems = %v, want none", probs)
	}
	got := target(t, p, "web").Command
	if got == nil || !slices.Equal(got.Run, []string{"repo-web", "demo", "https://example.invalid/pulls"}) {
		t.Errorf("command = %+v, want the run argv rendered for demo", got)
	}
}

// Each rule refuses the command target alone, and the project keeps the
// others (decisions.md D85).
func TestACommandTargetIsRefusedAlone(t *testing.T) {
	for name, tc := range map[string]struct {
		body string
		bad  revier.TargetName
		want string
	}{
		"no run": {
			"\n[[target]]\nname = \"web\"\n  [target.command]\n  timeout = \"5s\"\n", "web", "has no run argv",
		},
		"a window beside": {
			web + "  [target.window]\n  launch = [\"chrome\"]\n  match = { class = \"^chrome$\" }\n", "web", "has a command and a realization",
		},
		"home":                {strings.Replace(web, "key = ", "home = true\nkey = ", 1), "web", "runs a command and is home"},
		"prefer":              {strings.Replace(web, "key = ", "prefer = \"window\"\nkey = ", 1), "web", "runs a command and has prefer"},
		"timeout no duration": {web + "  timeout = \"soon\"\n", "web", "is no duration"},
		"timeout zero":        {web + "  timeout = \"0s\"\n", "web", "is not more than zero"},
		"timeout negative":    {web + "  timeout = \"-1s\"\n", "web", "is not more than zero"},
		"timeout too long":    {web + "  timeout = \"31s\"\n", "web", "is longer than 30s"},
		"inside a command": {
			web + "\n[[target]]\nname = \"tab\"\n  [target.runtime]\n  inside = \"web\"\n  launch = [\"top\"]\n", "tab", "runs a command and opens no tab",
		},
	} {
		t.Run(name, func(t *testing.T) {
			p := loadDemo(t, valid+tc.body)
			if p.Invalid != nil {
				t.Fatalf("project refused: %v, want the target alone", p.Invalid)
			}
			for i, tg := range p.Targets {
				err := p.TargetErr(i)
				switch {
				case tg.Name == tc.bad && (err == nil || !strings.Contains(err.Error(), tc.want)):
					t.Errorf("%s refused for %v, want %q", tg.Name, err, tc.want)
				case tg.Name != tc.bad && tg.Name != "web" && err != nil:
					t.Errorf("%s refused for %v, want it loaded", tg.Name, err)
				}
			}
		})
	}
}

// A command target marked home before the real home costs only itself: the
// real home loads and stays the project's (decisions.md D85).
func TestACommandTargetMarkedHomeTakesNoHomesPlace(t *testing.T) {
	homeWeb := strings.Replace(web, "key = ", "home = true\nkey = ", 1)
	first := "\n[[target]]\nname = \"home\""
	p := loadDemo(t, strings.Replace(valid, first, homeWeb+first, 1))
	if err := refusalOf(p, "web"); err == nil || !strings.Contains(err.Error(), "runs a command and is home") {
		t.Errorf("web refused for %v, want it refused as home", err)
	}
	if err := refusalOf(p, "home"); err != nil {
		t.Errorf("home refused for %v, want it loaded", err)
	}
	if h, ok := p.Home(); !ok || h.Name != "home" {
		t.Errorf("Home = %q, %v, want home", h.Name, ok)
	}
}

// A project whose only home is a command target has no home, and is refused
// as a project with none.
func TestAProjectWhoseOnlyHomeIsACommandHasNoHome(t *testing.T) {
	homeWeb := strings.Replace(web, "key = ", "home = true\nkey = ", 1)
	p := loadDemo(t, "path = \"/home/user/dev/github/revier\"\n"+homeWeb)
	if p.Invalid == nil || !strings.Contains(p.Invalid.Error(), "no target is marked home") {
		t.Errorf("project refused for %v, want no home", p.Invalid)
	}
}

// An empty run list refuses the target as a missing one does.
func TestACommandTargetWithAnEmptyRunIsRefused(t *testing.T) {
	p := loadDemo(t, valid+"\n[[target]]\nname = \"web\"\n  [target.command]\n  run = []\n")
	if err := refusalOf(p, "web"); err == nil || !strings.Contains(err.Error(), "has no run argv") {
		t.Errorf("web refused for %v, want no run argv", err)
	}
}

// A run argument that renders empty refuses the target in that project
// (decisions.md D83).
func TestACommandArgumentThatRendersEmptyIsRefused(t *testing.T) {
	p := loadDemo(t, strings.Replace(valid, `url = "https://example.invalid/pulls"`, `url = ""`, 1)+web)
	if err := refusalOf(p, "web"); err == nil || !strings.Contains(err.Error(), "renders to an empty argument") {
		t.Errorf("web refused for %v, want the empty argument", err)
	}
}

// commandConfig is config.toml with one shared command target beside the
// shared home.
const commandConfig = `
[[target]]
name = "home"
home = true
  [target.runtime]
  name = "session:{{.Name}}"
  match = { title = "^session:{{.Name}}$" }
  launch = ["sh"]
  [target.remote.runtime]
  name = "session:{{.Name}}"
  match = { title = "^session:{{.Name}}$" }
  launch = ["sh"]

[[target]]
name = "web"
key = "ctrl-shift-i"
  [target.command]
  run = ["repo-web", "{{.Name}}"]
  timeout = "5s"
`

// A shared command target reaches every local project, and no link: a link
// runs no command target.
func TestASharedCommandTargetReachesNoLink(t *testing.T) {
	_, loaded, err := config.Load(projectsRoot(t, commandConfig, map[string]string{
		"demo.toml": "path = \"/tmp/demo\"\n",
		"far.toml":  link,
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range loaded {
		_, has := p.Target("web")
		if has == (p.Remote != nil) {
			t.Errorf("%s: has web = %v, want it on the local project alone", p.Name, has)
		}
	}
}

// A link that writes a command, at the top of a target or under
// [target.remote], has that target refused.
func TestALinksCommandIsRefused(t *testing.T) {
	for name, body := range map[string]string{
		"command":        "\n[[target]]\nname = \"web\"\n  [target.command]\n  run = [\"x\"]\n",
		"remote command": "\n[[target]]\nname = \"web\"\n  [target.remote.command]\n  run = [\"x\"]\n",
	} {
		t.Run(name, func(t *testing.T) {
			p := loadDemo(t, link+body)
			if err := refusalOf(p, "web"); err == nil || !strings.Contains(err.Error(), "a link runs no command target") {
				t.Errorf("web refused for %v, want a link's refusal", err)
			}
		})
	}
}

// The config screen writes a new command target as a [target.command] table,
// and changes its timeout in place.
func TestTheConfigScreenWritesACommandTarget(t *testing.T) {
	root, shared := targetsRoot(t, targetsConfig)
	web := revier.Target{Name: "web", Key: "ctrl-shift-i", Command: &revier.Command{Run: []string{"repo-web", "{{.Name}}"}, Timeout: "5s"}}
	written, err := config.AddTarget(root, shared, web)
	if err != nil {
		t.Fatal(err)
	}
	want := targetsConfig + `
[[target]]
name = "web"
key = "ctrl-shift-i"
  [target.command]
  run = ["repo-web", "{{.Name}}"]
  timeout = "5s"
`
	if got := readConfig(t, root); got != want {
		t.Fatalf("config.toml =\n%s\nwant\n%s", got, want)
	}

	web.Command = &revier.Command{Run: web.Command.Run}
	if _, err := config.ReplaceTarget(root, 3, written.Shared, config.TargetEdit{Target: web}); err != nil {
		t.Fatal(err)
	}
	if got, want := readConfig(t, root), strings.Replace(want, "  timeout = \"5s\"\n", "", 1); got != want {
		t.Errorf("config.toml =\n%s\nwant\n%s", got, want)
	}
}

// A project writes the part of a shared command it changes, and no more.
func TestAProjectOverridesASharedCommandsTimeout(t *testing.T) {
	root := projectsRoot(t, commandConfig, map[string]string{"demo.toml": "path = \"/tmp/demo\"\n"})
	cfg, _, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	file, inh := config.ProjectFile(root, "demo"), config.Inherited{Targets: cfg.Targets}
	text, err := config.ReadProject(file, inh)
	if err != nil {
		t.Fatal(err)
	}
	web := text.Targets[1].Target
	web.Command = &revier.Command{Run: web.Command.Run, Timeout: "20s"}

	p, err := config.SaveProjectTarget(file, "web", config.TargetEdit{Target: web}, inh)
	if err != nil {
		t.Fatalf("SaveProjectTarget: %v", err)
	}
	want := "path = \"/tmp/demo\"\n\n[[target]]\nname = \"web\"\n  [target.command]\n  timeout = \"20s\"\n"
	if got := read(t, file); got != want {
		t.Errorf("file =\n%s\nwant\n%s", got, want)
	}
	if got := target(t, p, "web").Command; got.Timeout != "20s" || !slices.Equal(got.Run, []string{"repo-web", "demo"}) {
		t.Errorf("web command = %+v, want the shared run and the project's timeout", got)
	}
}
