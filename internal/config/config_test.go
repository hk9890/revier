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
	p := config.LoadProject(write(t, dir, "revier.toml", valid), nil)
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

// A file that still names itself is refused, with and without shared targets:
// a name that disagreed with the file name would otherwise be dropped unseen.
func TestLoadProjectRefusesNameKey(t *testing.T) {
	dir := t.TempDir()
	path := write(t, dir, "other.toml", `name = "revier"`+valid)
	shared := []map[string]any{{"name": "web", "window": map[string]any{
		"launch": []any{"x"}, "match": map[string]any{"class": "^x$"},
	}}}
	for _, s := range [][]map[string]any{nil, shared} {
		err := config.LoadProject(path, s).Invalid
		if err == nil || !strings.Contains(err.Error(), "file name is the project's name") {
			t.Errorf("shared %v: err = %v, want the name key refused", s, err)
		}
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
			"is marked home, and an earlier target already is",
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
		{
			"tab inside an undeclared target",
			tabProject(func(tab *revier.Target) { tab.Runtime.Inside = "nowhere" }),
			`target "tickets" is inside "nowhere", which the project does not declare`,
		},
		{
			"tab inside itself",
			tabProject(func(tab *revier.Target) { tab.Runtime.Inside = "tickets" }),
			`target "tickets" is inside itself`,
		},
		{
			"tab with nothing to run",
			tabProject(func(tab *revier.Target) { tab.Runtime.Launch = nil }),
			"has no launch argv to run in the tab",
		},
		{
			"tab with a window realization",
			tabProject(func(tab *revier.Target) { tab.Window = &base }),
			"a tab has only the runtime",
		},
		{
			"tab inside a tab",
			tabProject(func(tab *revier.Target) { tab.Runtime.Inside = "notes" }),
			`inside "notes", which is itself a tab`,
		},
		{
			"tab inside a window-only target",
			tabProject(func(tab *revier.Target) { tab.Runtime.Inside = "editor" }),
			`inside "editor", which has no runtime realization`,
		},
		{
			"window realization inside a target",
			func() revier.Project {
				p := tabProject(func(*revier.Target) {})
				p.Targets[1].Window.Inside = "home"
				return p
			}(),
			`target "editor" window realization is inside "home"`,
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

// tabProject is a valid project with a tab target, tickets, changed by edit.
// notes is a second tab, so a tab inside a tab has one to name.
func tabProject(edit func(tab *revier.Target)) revier.Project {
	p := revier.Project{Name: "p", Path: "/p", Targets: []revier.Target{
		{Name: "home", Home: true, Runtime: &revier.Realization{Name: "session:p", Launch: []string{"x"}, Match: revier.Match{Title: "^session:p$"}}},
		{Name: "editor", Window: &revier.Realization{Launch: []string{"x"}, Match: revier.Match{Class: "^x$"}}},
		{Name: "notes", Runtime: &revier.Realization{Inside: "home", Launch: []string{"notes"}}},
		{Name: "tickets", Runtime: &revier.Realization{Inside: "home", Launch: []string{"taskmgr-ui"}}},
	}}
	edit(&p.Targets[3])
	return p
}

// A tab is found by its target's name, so it needs neither a match nor a name.
func TestValidateAcceptsATabWithNoMatchAndNoName(t *testing.T) {
	if err := config.Validate(tabProject(func(*revier.Target) {})); err != nil {
		t.Fatalf("Validate: %v", err)
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

// A pattern that does not compile and a template that does not render cost
// their own target and nothing else: the project loads, its other targets
// work, and the refused one carries the reason (decisions.md D85).
func TestLoadProjectRefusesOnlyTheBrokenTarget(t *testing.T) {
	cases := map[string]string{
		"bad regex":   strings.Replace(valid, `class = "^code$"`, `class = "("`, 1),
		"missing key": strings.Replace(valid, `"{{.Path}}"`, `"{{.Vars.absent}}"`, 1),
		"empty match": strings.Replace(valid, `match = { class = "^code$" }`, `match = { }`, 1),
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := write(t, dir, "broken.toml", body)
			p := config.LoadProject(path, nil)
			if p.Invalid != nil {
				t.Fatalf("Invalid = %v, want the project to load", p.Invalid)
			}
			if p.File != path {
				t.Errorf("File = %q, want %q", p.File, path)
			}
			broken := 0
			for i := range p.Targets {
				if p.TargetErr(i) != nil {
					broken++
				}
			}
			if broken != 1 {
				t.Fatalf("%d refused targets, want exactly the one that is broken", broken)
			}
			if len(config.Problems(p)) != 1 {
				t.Errorf("Problems = %v, want the one refusal", config.Problems(p))
			}
		})
	}
}

// A file that is not TOML at all still becomes a project: it is listed, it
// declares no target, and it says why.
func TestLoadProjectKeepsAnUnparseableFile(t *testing.T) {
	dir := t.TempDir()
	path := write(t, dir, "broken.toml", "path = \"/p\"\nthis is not toml\n")
	p := config.LoadProject(path, nil)
	if p.Invalid == nil {
		t.Fatal("Invalid = nil, want the parse failure")
	}
	if !strings.Contains(p.Invalid.Error(), path) {
		t.Errorf("Invalid should name the file: %v", p.Invalid)
	}
	if p.Name != "broken" {
		t.Errorf("Name = %q, want the file name", p.Name)
	}
	if len(p.Targets) != 0 {
		t.Errorf("Targets = %v, want none", p.Targets)
	}
}

// Projects leave Load prepared: templates rendered, so a host never sees one.
func TestLoadProjectRendersTemplates(t *testing.T) {
	dir := t.TempDir()
	p := config.LoadProject(write(t, dir, "revier.toml", valid), nil)
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
	p := config.LoadProject(write(t, dir, "revier.toml", body), nil)
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
// key that does nothing. The action costs itself alone (decisions.md D85):
// it stays listed, carrying the reason, and the file is otherwise read.
func TestLoadRefusesAnActionKeyTheTUICannotRun(t *testing.T) {
	for key, want := range map[string]string{
		"y":            "typed text",
		"shift-y":      "typed text",
		"ctrl-shift-y": "reaches a terminal as ctrl+y",
		"super-y":      "never reaches a terminal",
		"<Nonsense>y":  "Nonsense",
		"alt-c":        "one of revier's own keys",
		"ctrl-n":       "one of revier's own keys",
		"End":          "one of revier's own keys",
		"page_down":    "one of revier's own keys",
	} {
		root := t.TempDir()
		write(t, root, "config.toml", "[[action]]\nkey = \""+key+"\"\nname = \"sync\"\nrun = [\"true\"]\n")
		cfg, _, err := config.Load(root)
		if err != nil {
			t.Errorf("key %q: Load = %v, want the action refused alone", key, err)
			continue
		}
		if len(cfg.Actions) != 1 || cfg.Actions[0].Refused == nil || len(cfg.Problems) != 1 {
			t.Errorf("key %q: actions = %+v, problems = %v, want the one action listed and refused", key, cfg.Actions, cfg.Problems)
			continue
		}
		for _, w := range []string{`action "sync"`, want} {
			if err := cfg.Actions[0].Refused; !strings.Contains(err.Error(), w) {
				t.Errorf("key %q: refused = %q, want it to say %q", key, err, w)
			}
		}
	}
}

// An action with nothing to run is refused at load, not at the keypress.
func TestLoadRefusesAnActionThatRunsNothing(t *testing.T) {
	root := t.TempDir()
	write(t, root, "config.toml", "[[action]]\nkey = \"ctrl-y\"\nname = \"sync\"\n")
	cfg, _, err := config.Load(root)
	if err != nil {
		t.Fatalf("Load = %v, want the action refused alone", err)
	}
	if err := cfg.Actions[0].Refused; err == nil || !strings.Contains(err.Error(), `action "sync" runs nothing`) {
		t.Errorf("refused = %v, want the empty action named", err)
	}
}

// A mistake in one action costs that action and nothing else: the sound
// action beside it is not refused, and every project still loads.
func TestABrokenActionCostsOnlyItself(t *testing.T) {
	root := t.TempDir()
	write(t, root, "config.toml", "[[action]]\nkey = \"y\"\nname = \"bad\"\nrun = [\"true\"]\n\n[[action]]\nkey = \"ctrl-y\"\nname = \"sync\"\nrun = [\"true\"]\n")
	if err := os.MkdirAll(filepath.Join(root, "projects"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(root, "projects"), "revier.toml", valid)
	cfg, projects, err := config.Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(projects) != 1 || projects[0].Invalid != nil {
		t.Errorf("projects = %+v, want the one project loaded whole", projects)
	}
	if cfg.Actions[0].Refused == nil || cfg.Actions[1].Refused != nil {
		t.Errorf("actions = %+v, want only the first refused", cfg.Actions)
	}
}
