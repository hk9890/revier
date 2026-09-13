package config_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/theme"
	"github.com/hk9890/revier/pkg/revier"
)

const valid = `
name = "revier"
path = "/home/hans/dev/github/revier"

[vars]
url = "https://example.invalid/pulls"

[[target]]
name = "home"
home = true
  [target.runtime]
  name = "home"
  match = { title = "^session:{{.Name}}$" }
    [[target.runtime.panels]]
    kind = "agent"
    command = ["claude"]

[[target]]
name = "editor"
key = "ctrl-o"
  [target.window]
  launch = ["code", "{{.Path}}"]
  match = { class = "^code$" }
`

func write(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

func TestLoadProject(t *testing.T) {
	dir := t.TempDir()
	p, err := config.LoadProject(write(t, dir, "revier.toml", valid), nil)
	if err != nil {
		t.Fatalf("LoadProject: %v", err)
	}
	if p.Name != "revier" || p.Path == "" {
		t.Fatalf("project = %+v", p)
	}
	if len(p.Targets) != 2 {
		t.Fatalf("got %d targets, want 2", len(p.Targets))
	}
	home, ok := p.Home()
	if !ok || home.Name != "home" {
		t.Fatal("home target not found")
	}
	if home.Runtime == nil || len(home.Runtime.Panels) != 1 {
		t.Fatalf("home runtime panels = %+v", home.Runtime)
	}
	if home.Runtime.Panels[0].Kind != revier.PanelAgent {
		t.Errorf("panel kind = %q", home.Runtime.Panels[0].Kind)
	}
	if p.Vars["url"] == "" {
		t.Error("vars not decoded")
	}
}

// A file with no name takes the stem, so a project file need not repeat its
// own identity and a rename cannot leave a stale one behind.
func TestLoadProjectNameDefaultsToFileStem(t *testing.T) {
	dir := t.TempDir()
	body := strings.Replace(valid, `name = "revier"`, "", 1)
	p, err := config.LoadProject(write(t, dir, "other.toml", body), nil)
	if err != nil {
		t.Fatalf("LoadProject: %v", err)
	}
	if p.Name != "other" {
		t.Errorf("name = %q, want other", p.Name)
	}
}

// A copied project file keeps its name line. Two files under one name would be
// one project to every lookup and to state, so the delete of the second row
// would remove the first file; the load refuses them and names both.
func TestLoadProjectsRefusesTwoFilesWithOneName(t *testing.T) {
	dir := t.TempDir()
	first := write(t, dir, "revier.toml", valid)
	second := write(t, dir, "revier-copy.toml", valid)

	_, err := config.LoadProjects(dir, nil)
	if err == nil {
		t.Fatal("two files declaring project revier loaded")
	}
	if !strings.Contains(err.Error(), first) || !strings.Contains(err.Error(), second) {
		t.Errorf("err = %v, want both files named", err)
	}
}

// Every rule here names a failure that is otherwise invisible until a key is
// pressed.
func TestValidateRejects(t *testing.T) {
	base := revier.Realization{Launch: []string{"x"}, Match: revier.Match{Class: "^x$"}}
	cases := []struct {
		name    string
		project revier.Project
		want    string
	}{
		{
			"no home target",
			revier.Project{Path: "/p", Targets: []revier.Target{{Name: "a", Window: &base}}},
			"no target is marked home",
		},
		{
			"two home targets",
			revier.Project{Path: "/p", Targets: []revier.Target{
				{Name: "a", Home: true, Window: &base}, {Name: "b", Home: true, Window: &base},
			}},
			"2 targets are marked home",
		},
		{
			"empty match",
			revier.Project{Path: "/p", Targets: []revier.Target{
				{Name: "a", Home: true, Window: &revier.Realization{Launch: []string{"x"}}},
			}},
			"empty match",
		},
		{
			"no launch argv",
			revier.Project{Path: "/p", Targets: []revier.Target{
				{Name: "a", Home: true, Window: &revier.Realization{Match: revier.Match{Class: "^x$"}}},
			}},
			"no launch argv",
		},
		{
			"duplicate key",
			revier.Project{Path: "/p", Targets: []revier.Target{
				{Name: "a", Home: true, Key: "ctrl-o", Window: &base},
				{Name: "b", Key: "ctrl-o", Window: &base},
			}},
			`share key "ctrl+o"`,
		},
		{
			"no realization",
			revier.Project{Path: "/p", Targets: []revier.Target{{Name: "a", Home: true}}},
			"declares no realization",
		},
		{
			"no path",
			revier.Project{Targets: []revier.Target{{Name: "a", Home: true, Window: &base}}},
			"no path",
		},
		{
			"bad prefer",
			revier.Project{Path: "/p", Targets: []revier.Target{
				{Name: "a", Home: true, Prefer: "sideways", Window: &base},
			}},
			"prefer must be",
		},
		{
			// A runtime host names what it opens after the realization, and
			// refuses to open without a name: the key would fail when pressed.
			"runtime realization with no name",
			revier.Project{Path: "/p", Targets: []revier.Target{
				{Name: "a", Home: true, Runtime: &revier.Realization{Launch: []string{"x"}, Match: revier.Match{Title: "^x$"}}},
			}},
			`target "a" runtime realization has no name`,
		},
		{
			// The name is written unquoted into the command a desktop key runs.
			"target name with a space",
			revier.Project{Path: "/p", Targets: []revier.Target{{Name: "my editor", Home: true, Window: &base}}},
			`target name "my editor"`,
		},
		{
			"target name with a quote",
			revier.Project{Path: "/p", Targets: []revier.Target{{Name: `ed"it`, Home: true, Window: &base}}},
			"target name",
		},
		{
			// The rule is ASCII because the name is read back out of a
			// shortcut's command to tell it from somebody else's.
			"target name with a non-ASCII letter",
			revier.Project{Path: "/p", Targets: []revier.Target{{Name: "édit", Home: true, Window: &base}}},
			`target name "édit"`,
		},
		{
			"target name that reads as a flag",
			revier.Project{Path: "/p", Targets: []revier.Target{{Name: "-p", Home: true, Window: &base}}},
			`target name "-p"`,
		},
		{
			// An agent address is <project>:<target>, split at the colon.
			"project name with a colon",
			revier.Project{Name: "a:b", Path: "/p", Targets: []revier.Target{{Name: "a", Home: true, Window: &base}}},
			`project name "a:b"`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := config.Validate(tc.project)
			if err == nil {
				t.Fatal("want an error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to mention %q", err, tc.want)
			}
		})
	}
}

// A window realization needs no name: its launch argv carries the identity its
// match finds. A target name may be any word, dots and dashes included.
func TestValidateAcceptsANamelessWindowAndAWordTargetName(t *testing.T) {
	p := revier.Project{Name: "p", Path: "/p", Targets: []revier.Target{
		{Name: "home", Home: true, Window: &revier.Realization{Launch: []string{"x"}, Match: revier.Match{Class: "^x$"}}},
		{Name: "diff-2.old_x", Window: &revier.Realization{Launch: []string{"x"}, Match: revier.Match{Class: "^y$"}}},
	}}
	if err := config.Validate(p); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

// A pattern that does not compile and a template that does not render are
// refused at load, naming the file, like every other invalid project.
func TestLoadProjectRejectsWhatPrepareRejects(t *testing.T) {
	cases := map[string]string{
		"bad regex":   strings.Replace(valid, `class = "^code$"`, `class = "("`, 1),
		"missing key": strings.Replace(valid, `"{{.Path}}"`, `"{{.Vars.absent}}"`, 1),
		"empty match": strings.Replace(valid, `match = { class = "^code$" }`, `match = { }`, 1),
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := write(t, dir, "broken.toml", body)
			_, err := config.LoadProject(path, nil)
			if err == nil {
				t.Fatal("want an error")
			}
			if !strings.Contains(err.Error(), path) {
				t.Errorf("error should name the file: %v", err)
			}
		})
	}
}

// Projects leave Load prepared: templates rendered, so a host never sees one.
func TestLoadProjectRendersTemplates(t *testing.T) {
	dir := t.TempDir()
	p, err := config.LoadProject(write(t, dir, "revier.toml", valid), nil)
	if err != nil {
		t.Fatalf("LoadProject: %v", err)
	}
	home, _ := p.Home()
	if home.Runtime.Match.Title != "^session:revier$" {
		t.Errorf("match title = %q, want it rendered", home.Runtime.Match.Title)
	}
	editor, _ := p.Target("editor")
	if editor.Window.Launch[1] != "/home/hans/dev/github/revier" {
		t.Errorf("launch = %v, want it rendered", editor.Window.Launch)
	}
}

func TestLoadMissingRootIsEmptyNotAnError(t *testing.T) {
	cfg, projects, err := config.Load(filepath.Join(t.TempDir(), "absent"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg == nil || len(projects) != 0 {
		t.Errorf("cfg=%v projects=%d, want an empty config and no projects", cfg, len(projects))
	}
}

func TestLoadReadsConfigAndProjects(t *testing.T) {
	root := t.TempDir()
	write(t, root, "config.toml", `
[hosts]
runtime = ["tmux"]
window = ["gnome"]

[[action]]
key = "ctrl-y"
name = "copy path"
run = ["wl-copy", "{{.Path}}"]
`)
	if err := os.MkdirAll(filepath.Join(root, "projects"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(root, "projects"), "revier.toml", valid)

	cfg, projects, err := config.Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Hosts.Runtime) != 1 || cfg.Hosts.Runtime[0] != "tmux" {
		t.Errorf("hosts.runtime = %v", cfg.Hosts.Runtime)
	}
	if len(cfg.Actions) != 1 || cfg.Actions[0].Key != "ctrl-y" {
		t.Errorf("actions = %+v", cfg.Actions)
	}
	if len(projects) != 1 {
		t.Fatalf("got %d projects, want 1", len(projects))
	}
}

func TestRootHonoursOverride(t *testing.T) {
	t.Setenv("REVIER_CONFIG_HOME", "/tmp/scratch-revier")
	got, err := config.Root()
	if err != nil {
		t.Fatal(err)
	}
	if got != "/tmp/scratch-revier" {
		t.Errorf("Root = %q", got)
	}
}

// A tilde is expanded once at load so no consumer hands a literal "~" to a
// program that does not expand it.
func TestLoadProjectExpandsHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	dir := t.TempDir()
	body := strings.Replace(valid, `path = "/home/hans/dev/github/revier"`, `path = "~/dev/github/revier"`, 1)
	p, err := config.LoadProject(write(t, dir, "revier.toml", body), nil)
	if err != nil {
		t.Fatalf("LoadProject: %v", err)
	}
	if want := filepath.Join(home, "dev/github/revier"); p.Path != want {
		t.Errorf("path = %q, want %q", p.Path, want)
	}
	editor, _ := p.Target("editor")
	if editor.Window.Launch[1] != p.Path {
		t.Errorf("{{.Path}} rendered %q, want the expanded path", editor.Window.Launch[1])
	}
	if editor.Window.Dir != p.Path {
		t.Errorf("dir = %q, want the expanded path", editor.Window.Dir)
	}
}

// A home target is its panels; it needs no launch of its own. A window
// realization cannot have panels, because a window host cannot see inside.
func TestValidatePanelsStandInForLaunch(t *testing.T) {
	panels := []revier.PanelSpec{{Kind: revier.PanelAgent, Command: []string{"claude"}}, {Kind: revier.PanelShell}}
	ok := revier.Project{Path: "/p", Targets: []revier.Target{
		{Name: "home", Home: true, Runtime: &revier.Realization{Name: "h", Match: revier.Match{Title: "^h$"}, Panels: panels}},
	}}
	if err := config.Validate(ok); err != nil {
		t.Errorf("a runtime realization with panels and no launch should validate: %v", err)
	}
	bad := revier.Project{Path: "/p", Targets: []revier.Target{
		{Name: "home", Home: true, Window: &revier.Realization{Launch: []string{"x"}, Match: revier.Match{Class: "^x$"}, Panels: panels}},
	}}
	if err := config.Validate(bad); err == nil || !strings.Contains(err.Error(), "panels") {
		t.Errorf("a window realization with panels should be rejected, got %v", err)
	}
	// A launch beside panels would be dropped silently by every host, and
	// the agent it named never started; it is refused instead.
	both := revier.Project{Path: "/p", Targets: []revier.Target{
		{Name: "home", Home: true, Runtime: &revier.Realization{Name: "h", Launch: []string{"claude"}, Match: revier.Match{Title: "^h$"}, Panels: panels}},
	}}
	if err := config.Validate(both); err == nil || !strings.Contains(err.Error(), "both launch and panels") {
		t.Errorf("launch beside panels should be rejected, got %v", err)
	}
}

// A [[probe]] entry is loaded with its binary path expanded; a half-declared
// one is refused at load.
func TestLoadProbes(t *testing.T) {
	root := t.TempDir()
	write(t, root, "config.toml", "[[probe]]\nname = \"aider\"\nexec = \"~/bin/aider-probe\"\n")
	cfg, _, err := config.Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	home, _ := os.UserHomeDir()
	if len(cfg.Probes) != 1 || cfg.Probes[0].Name != "aider" || cfg.Probes[0].Exec != filepath.Join(home, "bin/aider-probe") {
		t.Errorf("probes = %+v", cfg.Probes)
	}

	write(t, root, "config.toml", "[[probe]]\nname = \"aider\"\n")
	if _, _, err := config.Load(root); err == nil {
		t.Error("a probe without exec should be refused at load")
	}
}

func TestLoadReadsTheUITable(t *testing.T) {
	root := t.TempDir()
	write(t, root, "config.toml", `
[ui]
theme = "catppuccin-latte"
glyphs = "nerd"
`)
	cfg, _, err := config.Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	th, err := cfg.Theme()
	if err != nil {
		t.Fatalf("Theme: %v", err)
	}
	if th.Name != "catppuccin-latte" || th.Glyphs.Cursor == "" {
		t.Errorf("theme = %q, glyphs = %+v", th.Name, th.Glyphs)
	}
}

// A typo in a theme name must stop revier at startup. Falling back to the
// default would leave the user looking for a colour that never arrives.
func TestLoadRejectsAnUnknownThemeAndNamesTheValidOnes(t *testing.T) {
	root := t.TempDir()
	write(t, root, "config.toml", "[ui]\ntheme = \"dracula\"\n")

	_, _, err := config.Load(root)
	if err == nil {
		t.Fatal("Load: no error for an unknown theme")
	}
	if !strings.Contains(err.Error(), "catppuccin-mocha") {
		t.Errorf("error = %q, want it to list the valid names", err)
	}
}

func TestLoadWithNoUITableGetsTheDefault(t *testing.T) {
	cfg, _, err := config.Load(t.TempDir())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	th, err := cfg.Theme()
	if err != nil {
		t.Fatalf("Theme: %v", err)
	}
	if th.Name != theme.DefaultTheme || th.Glyphs != theme.Default().Glyphs {
		t.Errorf("default theme = %q with glyphs %+v", th.Name, th.Glyphs)
	}
}

// A geometry short of its four tokens would reach the window host and be
// refused there, after the window had already opened.
func TestValidateRejectsAShortPlacement(t *testing.T) {
	p := revier.Project{Name: "p", Path: "/p", Targets: []revier.Target{
		{Name: "home", Home: true, Runtime: &revier.Realization{
			Name: "x", Launch: []string{"x"}, Match: revier.Match{Title: "^x$"},
			Place: "right top",
		}},
	}}
	err := config.Validate(p)
	if err == nil {
		t.Fatal("Validate accepted a two-token placement")
	}
	if !strings.Contains(err.Error(), "four tokens") {
		t.Errorf("error = %v, want it to say what a placement needs", err)
	}
}

func TestValidateAcceptsAFullPlacement(t *testing.T) {
	p := revier.Project{Name: "p", Path: "/p", Targets: []revier.Target{
		{Name: "home", Home: true, Runtime: &revier.Realization{
			Name: "x", Launch: []string{"x"}, Match: revier.Match{Title: "^x$"},
			Place: "right top 75% 100%",
		}},
	}}
	if err := config.Validate(p); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

// The chord that opens revier has a default, so `revier keys status` reports
// the key on a machine with no config.toml at all.
func TestTriggerKeyDefaults(t *testing.T) {
	cfg, _, err := config.Load(t.TempDir())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got, err := cfg.TriggerKey()
	if err != nil {
		t.Fatalf("TriggerKey: %v", err)
	}
	if got != "alt+space" {
		t.Errorf("trigger key = %q, want alt+space", got)
	}
}

// Whatever spelling the file uses, the resolved chord is the canonical one:
// comparing it with what GNOME reports is the whole point of resolving it.
func TestTriggerKeyIsCanonical(t *testing.T) {
	for _, in := range []string{"ctrl-shift-space", "<Shift><Control>space", "ctrl+shift+space"} {
		root := t.TempDir()
		write(t, root, "config.toml", "[ui]\ntrigger_key = \""+in+"\"\n")
		cfg, _, err := config.Load(root)
		if err != nil {
			t.Fatalf("Load(%q): %v", in, err)
		}
		got, err := cfg.TriggerKey()
		if err != nil {
			t.Fatalf("TriggerKey(%q): %v", in, err)
		}
		if got != "ctrl+shift+space" {
			t.Errorf("trigger_key %q resolved to %q", in, got)
		}
	}
}

// A target key that cannot be read must be refused, not dropped. Dropped, the
// target loses its desktop chord and its row in `revier keys status`, which is
// the one place a user would look to find out what happened to it.
func TestValidateRejectsATargetKeyItCannotRead(t *testing.T) {
	p := revier.Project{Name: "p", Path: "/p", Targets: []revier.Target{
		{Name: "home", Home: true, Key: "<Nonsense>u", Runtime: &revier.Realization{
			Name: "x", Launch: []string{"x"}, Match: revier.Match{Title: "^x$"},
		}},
	}}
	err := config.Validate(p)
	if err == nil {
		t.Fatal("Validate accepted a key it cannot read")
	}
	for _, want := range []string{"home", "Nonsense"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to name %q", err, want)
		}
	}
}

// A keyed target's name is the row, the shortcut's entry, and a word in the
// command the key runs. "picker" is the row of the key that opens revier, so
// the two collided and every install was refused; a name that is not one plain
// word would run as shell syntax, and revier would not know the shortcut as
// its own.
func TestValidateRejectsANameAKeyedTargetCannotHave(t *testing.T) {
	for _, name := range []revier.TargetName{"picker", "my editor", `x"; rm -rf ~; "`, "-x"} {
		p := revier.Project{Name: "p", Path: "/p", Targets: []revier.Target{
			{Name: "home", Home: true, Runtime: &revier.Realization{
				Launch: []string{"x"}, Match: revier.Match{Title: "^x$"},
			}},
			{Name: name, Key: "ctrl-shift-p", Window: &revier.Realization{
				Launch: []string{"y"}, Match: revier.Match{Class: "^y$"},
			}},
		}}
		err := config.Validate(p)
		if err == nil {
			t.Errorf("Validate accepted a keyed target named %q", name)
			continue
		}
		if !strings.Contains(err.Error(), strconv.Quote(string(name))) {
			t.Errorf("error = %q, want it to name the target", err)
		}
	}
}

// Without a key the name reaches no desktop, so it is not restricted.
func TestATargetWithNoKeyMayBeCalledPicker(t *testing.T) {
	p := revier.Project{Name: "p", Path: "/p", Targets: []revier.Target{
		{Name: "picker", Home: true, Runtime: &revier.Realization{
			Name: "x", Launch: []string{"x"}, Match: revier.Match{Title: "^x$"},
		}},
	}}
	if err := config.Validate(p); err != nil {
		t.Errorf("Validate: %v", err)
	}
}

// Two spellings of one key are one key. Compared as text they are two, and the
// second silently takes the chord from the first at the desktop.
func TestTwoSpellingsOfOneKeyAreADuplicate(t *testing.T) {
	p := revier.Project{Name: "p", Path: "/p", Targets: []revier.Target{
		{Name: "home", Home: true, Key: "ctrl-o", Runtime: &revier.Realization{
			Name: "x", Launch: []string{"x"}, Match: revier.Match{Title: "^x$"},
		}},
		{Name: "editor", Key: "<Control>O", Window: &revier.Realization{
			Launch: []string{"code"}, Match: revier.Match{Class: "^code$"},
		}},
	}}
	err := config.Validate(p)
	if err == nil {
		t.Fatal("Validate accepted two spellings of one key")
	}
	// Both spellings are named, because that is what the reader has to find
	// in the file.
	for _, want := range []string{"ctrl-o", "<Control>O", "ctrl+o"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to name %q", err, want)
		}
	}
}

// A key that does not parse fails at startup, naming the value. Reported at a
// keypress instead, it would look like a broken keyboard.
func TestLoadRejectsAnUnreadableTriggerKey(t *testing.T) {
	root := t.TempDir()
	write(t, root, "config.toml", "[ui]\ntrigger_key = \"<Nonsense>space\"\n")

	_, _, err := config.Load(root)
	if err == nil {
		t.Fatal("Load accepted a key it cannot read")
	}
	for _, want := range []string{"trigger_key", "Nonsense"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to name %q", err, want)
		}
	}
}

// An action's key is the TUI's alone, so a key the TUI cannot receive, or
// reads as filter text, is refused at load and named, rather than bound to a
// key that does nothing.
func TestLoadRefusesAnActionKeyTheTUICannotRun(t *testing.T) {
	for key, want := range map[string]string{
		"y":            "typed text",
		"shift-y":      "typed text",
		"ctrl-shift-y": "reaches a terminal as ctrl+y",
		"super-y":      "never reaches a terminal",
		"<Nonsense>y":  "Nonsense",
	} {
		root := t.TempDir()
		write(t, root, "config.toml", "[[action]]\nkey = \""+key+"\"\nname = \"sync\"\nrun = [\"true\"]\n")
		_, _, err := config.Load(root)
		if err == nil {
			t.Errorf("Load accepted action key %q", key)
			continue
		}
		for _, w := range []string{`action "sync"`, want} {
			if !strings.Contains(err.Error(), w) {
				t.Errorf("key %q: error = %q, want it to say %q", key, err, w)
			}
		}
	}
}

// An action with nothing to run fails at load, not at the keypress.
func TestLoadRefusesAnActionThatRunsNothing(t *testing.T) {
	root := t.TempDir()
	write(t, root, "config.toml", "[[action]]\nkey = \"ctrl-y\"\nname = \"sync\"\n")
	if _, _, err := config.Load(root); err == nil || !strings.Contains(err.Error(), `action "sync" runs nothing`) {
		t.Errorf("Load = %v, want the empty action named", err)
	}
}
