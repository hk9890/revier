package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/pkg/revier"
)

func TestALinkDerivesItsHomeItsAgentTabAndItsNameOnTheHost(t *testing.T) {
	p := config.LoadProject(write(t, t.TempDir(), "far.toml", link), nil)
	if p.Remote == nil || p.Remote.Host != "buildbox" || p.Remote.Project != "far" {
		t.Fatalf("remote = %+v, want buildbox and the link's own name there", p.Remote)
	}
	if probs := config.Problems(p); len(probs) > 0 {
		t.Fatalf("Problems = %v, want none", probs)
	}
	home, ok := p.Home()
	if !ok || home.Runtime == nil {
		t.Fatalf("home = %+v, want a derived runtime target", home)
	}
	if !slices.Equal(home.Runtime.Tabs, []revier.TargetName{"agent"}) || len(home.Runtime.Panels) != 0 || len(home.Runtime.Launch) != 0 {
		t.Fatalf("home runtime = %+v, want a container that lists the agent tab alone", home.Runtime)
	}
	if got := names(p); !slices.Equal(got, []revier.TargetName{"home", "agent"}) {
		t.Fatalf("targets = %v, want the home and its tab after it", got)
	}
	tab := target(t, p, "agent")
	if tab.Runtime.Inside != "home" || tab.Key != "" || len(tab.Runtime.Launch) != 0 {
		t.Errorf("agent = %+v, want a keyless tab of home that its panels start", tab)
	}
	panels := tab.Runtime.Panels
	if len(panels) != 2 || panels[0].Kind != revier.PanelAgent || panels[1].Kind != revier.PanelShell {
		t.Fatalf("panels = %+v, want an agent and a shell", panels)
	}
	// The argv that reaches the host is the remote port's, asked by the
	// core where the tabs are read: the file derives the kinds alone.
	if len(panels[0].Command) != 0 || len(panels[1].Command) != 0 {
		t.Errorf("commands = %q, %q; want none: the transport is the core's to ask", panels[0].Command, panels[1].Command)
	}
	if home.Runtime.Name != "session:far" || home.Runtime.Match.Title != "^session:far$" {
		t.Errorf("name %q, match %q: the workspace must be found again by its title", home.Runtime.Name, home.Runtime.Match.Title)
	}
	if home.Runtime.Dir != "" {
		t.Errorf("dir = %q, want none: nothing of the project is here", home.Runtime.Dir)
	}
}

// The link's name here and the project's name on the host may differ: the
// host is asked by its name, the list shows this one.
func TestALinkMayNameTheProjectDifferentlyOnTheHost(t *testing.T) {
	body := strings.Replace(link, `host = "buildbox"`, "host = \"buildbox\"\nproject = \"far\"", 1)
	p := config.LoadProject(write(t, t.TempDir(), "build.toml", body), nil)
	if p.Name != "build" || p.Remote.Project != "far" {
		t.Errorf("name %q, on the host %q; want build here and far there", p.Name, p.Remote.Project)
	}
}

// A link may declare targets of its own, windows here that reach the project
// there; it writes them under [target.remote]. A declared home keeps what it
// says, and one with a launch gets no agent tab. The host's path, when written, is kept as
// written for templates: it is not a path here.
func TestALinkKeepsItsOwnTargetsAndTheHostsPath(t *testing.T) {
	body := "path = \"~/dev/far\"\n" + link + `
[[target]]
name = "editor"
key = "ctrl-o"
  [target.remote.window]
  launch = ["code", "--remote", "ssh-remote+buildbox", "{{.Path}}"]
  match = { title = "far \\[SSH: buildbox\\]" }
`
	p := config.LoadProject(write(t, t.TempDir(), "far.toml", body), nil)
	if p.Path != "~/dev/far" {
		t.Errorf("path = %q, want the host's, kept as written", p.Path)
	}
	editor, ok := p.Target("editor")
	if !ok || editor.Window.Launch[3] != "~/dev/far" {
		t.Errorf("editor = %+v, want the host's path rendered into its launch", editor)
	}
	if _, ok := p.Home(); !ok {
		t.Error("the home is still derived beside a declared target")
	}
	if got := names(p); !slices.Equal(got, []revier.TargetName{"home", "agent", "editor"}) {
		t.Errorf("targets = %v, want the derived home, its tab, and the editor", got)
	}

	own := link + `
[[target]]
name = "shell"
home = true
  [target.remote.runtime]
  name = "far"
  launch = ["ssh", "buildbox"]
  match = { title = "^far$" }
`
	p = config.LoadProject(write(t, t.TempDir(), "far.toml", own), nil)
	if home, _ := p.Home(); home.Name != "shell" || len(p.Targets) != 1 {
		t.Errorf("targets = %+v, want the declared home alone", p.Targets)
	}
	if probs := config.Problems(p); len(probs) > 0 {
		t.Errorf("Problems = %v, want a home with a launch and no tabs whole", probs)
	}
	if home, _ := p.Home(); !slices.Equal(home.Runtime.Launch, []string{"ssh", "buildbox"}) {
		t.Errorf("launch = %q, want the one the link declared", home.Runtime.Launch)
	}
}

// A home the link does not launch itself still reaches the workspace: the
// derived name, match and tab fill what it left empty, so a placement alone
// is enough. The home may have any name, and the tab is inside that one.
func TestALinkHomeTakesWhatItLeavesOutFromTheDerivedOne(t *testing.T) {
	body := link + `
[[target]]
name = "home"
home = true
  [target.remote.runtime]
  place = "right top 75% 100%"
`
	p := config.LoadProject(write(t, t.TempDir(), "far.toml", body), nil)
	home, ok := p.Home()
	if !ok || home.Runtime == nil {
		t.Fatalf("home = %+v, want the derived home under the declared target", home)
	}
	if home.Runtime.Place != "right top 75% 100%" {
		t.Errorf("place = %q, want the one the link declared", home.Runtime.Place)
	}
	if !slices.Equal(home.Runtime.Tabs, []revier.TargetName{"agent"}) || home.Runtime.Match.Title != "^session:far$" {
		t.Errorf("home runtime = %+v, want the derived tab and match", home.Runtime)
	}
	if tab := target(t, p, "agent"); tab.Runtime.Inside != "home" || len(tab.Runtime.Panels) != 2 {
		t.Errorf("agent = %+v, want the derived tab of home", tab)
	}
	if probs := config.Problems(p); len(probs) > 0 {
		t.Errorf("Problems = %v, want none", probs)
	}

	p = config.LoadProject(write(t, t.TempDir(), "far.toml", strings.Replace(body, `name = "home"`, `name = "work"`, 1)), nil)
	if got := names(p); !slices.Equal(got, []revier.TargetName{"work", "agent"}) || target(t, p, "agent").Runtime.Inside != "work" {
		t.Errorf("targets = %+v, want the tab inside the home as the link named it", p.Targets)
	}
	if probs := config.Problems(p); len(probs) > 0 {
		t.Errorf("Problems = %v, want none", probs)
	}
}

// A target named agent that the link has is the tab when it is inside the
// home: it gets the two panels when it has nothing to start, which is how a
// shared target gives the tab its key, and keeps a launch or panels it has.
func TestALinksDeclaredAgentTabIsTheDerivedOne(t *testing.T) {
	body := link + `
[[target]]
name = "agent"
key = "ctrl-a"
  [target.remote.runtime]
  inside = "home"
`
	p := config.LoadProject(write(t, t.TempDir(), "far.toml", body), nil)
	if probs := config.Problems(p); len(probs) > 0 {
		t.Fatalf("Problems = %v, want none", probs)
	}
	if got := names(p); !slices.Equal(got, []revier.TargetName{"home", "agent"}) {
		t.Fatalf("targets = %v, want no second agent", got)
	}
	if tab := target(t, p, "agent"); tab.Key != "ctrl-a" || len(tab.Runtime.Panels) != 2 {
		t.Errorf("agent = %+v, want the declared key and the derived panels", tab)
	}

	p = config.LoadProject(write(t, t.TempDir(), "far.toml", body+"  launch = [\"ssh\", \"buildbox\"]\n"), nil)
	if probs := config.Problems(p); len(probs) > 0 {
		t.Fatalf("Problems = %v, want none", probs)
	}
	if tab := target(t, p, "agent"); len(tab.Runtime.Panels) != 0 || len(tab.Runtime.Launch) != 2 {
		t.Errorf("agent = %+v, want the declared launch and no panels beside it", tab)
	}
	if home, _ := p.Home(); !slices.Equal(home.Runtime.Tabs, []revier.TargetName{"agent"}) {
		t.Errorf("home tabs = %v, want the declared tab listed", home.Runtime.Tabs)
	}
}

// A target named agent that is no tab of the home is left as the link wrote
// it. The home still lists the name, and is refused with the fix.
func TestALinksAgentTargetThatIsNoTabRefusesTheHome(t *testing.T) {
	body := link + `
[[target]]
name = "agent"
  [target.remote.window]
  launch = ["code"]
  match = { class = "^Code$" }
`
	p := config.LoadProject(write(t, t.TempDir(), "far.toml", body), nil)
	if got := names(p); !slices.Equal(got, []revier.TargetName{"home", "agent"}) || target(t, p, "agent").Runtime != nil {
		t.Fatalf("targets = %+v, want the declared window target alone under the name", p.Targets)
	}
	want := `target "home" opens a link with the tab "agent", and target "agent" is not a tab of it; give that target another name, or inside = "home"`
	if err := refusalOf(p, "home"); err == nil || err.Error() != want {
		t.Errorf("home refused = %v\nwant %s", err, want)
	}
	if err := refusalOf(p, "agent"); err != nil {
		t.Errorf("agent refused = %v, want the declared target whole", err)
	}
}

// A tab is refused inside a target that also has a window, so a home with
// both realizations gets no tab: its runtime is refused for having nothing to
// start, and says so.
func TestALinkHomeWithAWindowAndARuntimeGetsNoTab(t *testing.T) {
	body := link + `
[[target]]
name = "home"
home = true
  [target.remote.window]
  launch = ["code", "--remote", "ssh-remote+buildbox"]
  match = { class = "^Code$" }
  [target.remote.runtime]
  place = "right top 75% 100%"
`
	p := config.LoadProject(write(t, t.TempDir(), "far.toml", body), nil)
	if got := names(p); !slices.Equal(got, []revier.TargetName{"home"}) {
		t.Fatalf("targets = %v, want no derived tab", got)
	}
	if err := refusalOf(p, "home"); err == nil || !strings.Contains(err.Error(), "runtime realization has no launch argv and no tabs") {
		t.Errorf("home refused = %v, want the runtime refused for what it lacks", err)
	}
}

// A shared target reaches a link through its remote part alone. One with no
// remote part is a local target, and no link has it (decisions.md D82).
func TestSharedTargetsReachALinkThroughTheirRemotePart(t *testing.T) {
	root := t.TempDir()
	write(t, root, "config.toml", `
[[target]]
name = "home"
home = true
key = "ctrl-u"
  [target.runtime]
  name = "session:{{.Name}}"
  launch = ["kitty"]
  match = { title = "^session:{{.Name}}$" }
  place = "left top 50% 100%"
  [target.remote.runtime]
  place = "right top 75% 100%"

[[target]]
name = "editor"
key = "ctrl-o"
  [target.window]
  launch = ["idea", "{{.Path}}"]
  match = { class = "^jetbrains-idea" }
  [target.remote.window]
  launch = ["code", "--remote", "ssh-remote+{{.Remote.Host}}", "{{.Path}}"]
  match = { class = "^Code$", title = "\\[SSH: {{.Remote.Host}}\\]" }

[[target]]
name = "tickets"
  [target.runtime]
  inside = "home"
  launch = ["tickets"]
`)
	if err := os.MkdirAll(filepath.Join(root, "projects"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(root, "projects"), "far.toml", "path = \"/srv/far\"\n"+link)
	write(t, filepath.Join(root, "projects"), "near.toml", "path = \"/srv/near\"\n")
	_, loaded, err := config.Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	p, local := loaded[0], loaded[1]
	if p.Name != "far" {
		p, local = local, p
	}
	if _, ok := p.Target("tickets"); ok {
		t.Error("a shared target with no remote part is local; a link does not get it")
	}
	editor, ok := p.Target("editor")
	if !ok || editor.Window == nil || editor.Key != "ctrl-o" {
		t.Fatalf("editor = %+v, want the remote realization under the shared name and key", editor)
	}
	want := []string{"code", "--remote", "ssh-remote+buildbox", "/srv/far"}
	if !slices.Equal(editor.Window.Launch, want) {
		t.Errorf("launch = %q, want %q", editor.Window.Launch, want)
	}
	if editor.Runtime != nil {
		t.Errorf("runtime = %+v, want the local part left behind", editor.Runtime)
	}
	home, _ := p.Home()
	if home.Runtime.Place != "right top 75% 100%" {
		t.Errorf("place = %q, want the remote part's", home.Runtime.Place)
	}
	if home.Runtime.Match.Title != "^session:far$" {
		t.Errorf("match = %q, want the derived one, not the local part's", home.Runtime.Match.Title)
	}
	if tab, ok := p.Target("agent"); !ok || tab.Runtime.Inside != "home" || len(tab.Runtime.Panels) != 2 {
		t.Errorf("agent = %+v, want the derived tab beside the shared targets", tab)
	}

	near, _ := local.Target("editor")
	if near.Window.Launch[0] != "idea" {
		t.Errorf("launch = %q, want the local part for a local project", near.Window.Launch)
	}
	if _, ok := local.Target("tickets"); !ok {
		t.Error("a local project keeps a target with no remote part")
	}
}

// The part of the other kind would never be read, so the file that writes it
// does not load.
func TestAProjectFileWritesOnlyThePartOfItsKind(t *testing.T) {
	remoteOnLocal := valid + `
[[target]]
name = "pages"
  [target.remote.window]
  launch = ["code"]
  match = { class = "^Code$" }
`
	p := config.LoadProject(write(t, t.TempDir(), "near.toml", remoteOnLocal), nil)
	if err := refusalOf(p, "pages"); err == nil || !strings.Contains(err.Error(), "[target.remote]") {
		t.Errorf("err = %v, want [target.remote] refused on a local project", err)
	}
	// The mistake is one target's, and costs that target: the project loads
	// with its home, as any refused target leaves it (decisions.md D85).
	if p.Invalid != nil || refusalOf(p, "home") != nil {
		t.Errorf("Invalid = %v, home refused = %v; want the project and its home whole", p.Invalid, refusalOf(p, "home"))
	}

	localOnLink := link + `
[[target]]
name = "pages"
  [target.window]
  launch = ["code"]
  match = { class = "^Code$" }
`
	p = config.LoadProject(write(t, t.TempDir(), "far.toml", localOnLink), nil)
	if err := refusalOf(p, "pages"); err == nil || !strings.Contains(err.Error(), "[target.remote.window]") {
		t.Errorf("err = %v, want a link's realization asked for under [target.remote.window]", err)
	}
	if p.Invalid != nil || refusalOf(p, "home") != nil {
		t.Errorf("Invalid = %v, home refused = %v; want the link and its derived home whole", p.Invalid, refusalOf(p, "home"))
	}
}

// The drop rule runs both ways: a shared target with only a remote part is a
// link's, and a local project is not left holding a target with nothing to
// open.
func TestASharedTargetWithNoLocalPartIsNotALocalProjectsTarget(t *testing.T) {
	root := t.TempDir()
	write(t, root, "config.toml", `
[[target]]
name = "shell"
  [target.remote.runtime]
  name = "far"
  launch = ["ssh", "buildbox"]
  match = { title = "^far$" }
`)
	if err := os.MkdirAll(filepath.Join(root, "projects"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(root, "projects"), "near.toml", `
path = "/srv/near"
[[target]]
name = "home"
home = true
  [target.runtime]
  name = "session:near"
  launch = ["kitty"]
  match = { title = "^session:near$" }
`)
	_, loaded, err := config.Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, ok := loaded[0].Target("shell"); ok {
		t.Error("a shared target with no local part is a link's; a local project does not get it")
	}
}

// A target the link declares itself is refused when it names no realization,
// the way a local project's is. Dropped instead, its key would answer to
// nothing and no message would say why.
func TestALinkTargetWithNoRealizationIsRefused(t *testing.T) {
	body := link + `
[[target]]
name = "editor"
key = "ctrl-o"
`
	err := loadErr(write(t, t.TempDir(), "far.toml", body), nil)
	if err == nil || !strings.Contains(err.Error(), "declares no realization") {
		t.Errorf("err = %v, want the link's own editor refused for declaring no realization", err)
	}
}

// A home the link opens as a window has named the tool that reaches the
// workspace, so no runtime and no agent tab are put beside it: with no window
// host here they would answer instead of the window the link asked for.
func TestALinkHomeThatIsAWindowKeepsTheDerivedHomeOut(t *testing.T) {
	body := link + `
[[target]]
name = "home"
home = true
  [target.remote.window]
  launch = ["code", "--remote", "ssh-remote+buildbox"]
  match = { class = "^Code$" }
`
	p := config.LoadProject(write(t, t.TempDir(), "far.toml", body), nil)
	if home, _ := p.Home(); home.Runtime != nil {
		t.Errorf("home runtime = %+v, want the declared window alone", home.Runtime)
	}
	if _, ok := p.Target("agent"); ok {
		t.Error("an agent tab was derived for a home that is a window")
	}
}

// Panels are on a tab alone, on a link as on a local project (decisions.md
// D128): a link's home that declares them is refused with the fix, and the
// tab is not derived beside them.
func TestALinkHomeThatDeclaresPanelsIsRefused(t *testing.T) {
	body := link + `
[[target]]
name = "home"
home = true
  [target.remote.runtime]
    [[target.remote.runtime.panels]]
    kind = "shell"
    title = "shell"
`
	p := config.LoadProject(write(t, t.TempDir(), "far.toml", body), nil)
	want := `target "home" runtime realization declares panels; only a tab has them, and a link opens with the tab "agent", so move them into a target named "agent" with inside = "home"`
	if err := refusalOf(p, "home"); err == nil || err.Error() != want {
		t.Errorf("home refused = %v\nwant %s", err, want)
	}
	if p.Invalid != nil {
		t.Errorf("Invalid = %v, want the link listed with its refused home", p.Invalid)
	}
	if got := names(p); !slices.Equal(got, []revier.TargetName{"home"}) {
		t.Errorf("targets = %v, want no tab derived beside the declared panels", got)
	}

	// The move the refusal names loads: the panels are the tab's.
	moved := link + `
[[target]]
name = "agent"
  [target.remote.runtime]
  inside = "home"
    [[target.remote.runtime.panels]]
    kind = "shell"
    title = "shell"
`
	p = config.LoadProject(write(t, t.TempDir(), "far.toml", moved), nil)
	if probs := config.Problems(p); len(probs) > 0 {
		t.Errorf("Problems = %v, want none after the move", probs)
	}
	if tab := target(t, p, "agent"); len(tab.Runtime.Panels) != 1 {
		t.Errorf("agent = %+v, want the declared panel alone", tab)
	}
}

// [target.remote] holds a realization each and nothing else: a key beside
// them is lifted onto the target for a link alone, and would change what the
// name and the key mean there.
func TestARemoteTableHoldsOnlyRealizations(t *testing.T) {
	root := t.TempDir()
	write(t, root, "config.toml", `
[[target]]
name = "editor"
  [target.window]
  launch = ["idea"]
  match = { class = "^idea$" }
  [target.remote]
  name = "renamed"
`)
	cfg, _, err := config.Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Problems) != 1 || !strings.Contains(cfg.Problems[0].Error(), "[target.remote] holds") {
		t.Errorf("problems = %v, want \"name\" under [target.remote] refused", cfg.Problems)
	}

	body := link + `
[[target]]
name = "editor"
  [target.remote]
  home = true
    [target.remote.window]
    launch = ["code"]
    match = { class = "^Code$" }
`
	if err := loadErr(write(t, t.TempDir(), "far.toml", body), nil); err == nil ||
		!strings.Contains(err.Error(), "[target.remote] holds") {
		t.Errorf("err = %v, want \"home\" under [target.remote] refused in a link file too", err)
	}
}

// `revier link` loads the new link with the shared targets, so the project it
// hands back is the one the next start reads, and a shared target that would
// refuse the link is caught before the file stays.
func TestCreateLinkGivesTheLinkItsSharedRemoteTargets(t *testing.T) {
	root := t.TempDir()
	write(t, root, "config.toml", `
[[target]]
name = "editor"
key = "ctrl-o"
  [target.window]
  launch = ["idea"]
  match = { class = "^idea$" }
  [target.remote.window]
  launch = ["code", "--remote", "ssh-remote+{{.Remote.Host}}"]
  match = { class = "^Code$" }
`)
	p, err := config.CreateLink(root, "far", "buildbox", revier.Project{Path: "/srv/far"})
	if err != nil {
		t.Fatalf("CreateLink: %v", err)
	}
	editor, ok := p.Target("editor")
	if !ok || editor.Key != "ctrl-o" {
		t.Fatalf("targets = %+v, want the shared editor under its key", p.Targets)
	}
	want := []string{"code", "--remote", "ssh-remote+buildbox"}
	if !slices.Equal(editor.Window.Launch, want) {
		t.Errorf("launch = %q, want %q", editor.Window.Launch, want)
	}
}

// `revier link` writes the smallest file that says where the project is,
// and loads it back: the name on the host only when it differs.
func TestCreateLinkWritesTheRemoteTableAndLoadsItBack(t *testing.T) {
	root := t.TempDir()
	p, err := config.CreateLink(root, "far", "buildbox", revier.Project{Path: "/srv/far"})
	if err != nil {
		t.Fatalf("CreateLink: %v", err)
	}
	if p.Name != "far" || p.Remote == nil || p.Remote.Host != "buildbox" || p.Remote.Project != "far" {
		t.Errorf("link = %+v, want far on buildbox", p.Project)
	}
	body, err := os.ReadFile(config.ProjectFile(root, "far"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "project =") {
		t.Errorf("file = %q: the same name is not written twice", body)
	}

	build, err := config.CreateLink(root, "build", "buildbox", revier.Project{Name: "far", Path: "/srv/far"})
	if err != nil {
		t.Fatalf("CreateLink: %v", err)
	}
	if build.Name != "build" || build.Remote.Project != "far" {
		t.Errorf("link = %+v, want build here and far there", build.Project)
	}
	if _, err := config.CreateLink(root, "far", "buildbox", revier.Project{Path: "/srv/far"}); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("err = %v, want the existing file kept", err)
	}
	if _, err := config.CreateLink(root, "bad", "-oProxyCommand=x", revier.Project{Path: "/srv/far"}); err == nil {
		t.Error("want the host refused before anything is written")
	}
	// A project that is a link on the host reaches a third machine, whose
	// checkout `revier agent exec` on the host refuses to serve.
	chained := revier.Project{Name: "far", Path: "/srv/far", Remote: &revier.Link{Host: "third", Project: "far"}}
	if _, err := config.CreateLink(root, "chained", "buildbox", chained); err == nil || !strings.Contains(err.Error(), "third") {
		t.Errorf("err = %v, want the link refused, naming the machine that holds the project", err)
	}
	if _, err := os.Stat(config.ProjectFile(root, "chained")); err == nil {
		t.Error("a refused link left a file behind")
	}
}

// On a link a target that is not home lists no tabs, so panels on it have one
// place to go: a tab of the home, which its key opens. The refusal says that,
// and the move it names loads.
func TestALinkTargetThatIsNotHomeIsToldWhereItsPanelsGo(t *testing.T) {
	const logs = `
[[target]]
name = "logs"
  [target.remote.runtime]
  name = "logs"
  match = { title = "^logs$" }
`
	const panel = `    [[target.remote.runtime.panels]]
    kind = "tool"
    command = ["tail"]
`
	p := config.LoadProject(write(t, t.TempDir(), "far.toml", link+logs+panel), nil)
	want := `target "logs" runtime realization declares panels; only a tab has them, and on a link only the home has tabs, so move them into a target with inside = "home"`
	if err := refusalOf(p, "logs"); err == nil || err.Error() != want {
		t.Errorf("logs refused = %v\nwant %s", err, want)
	}

	// A list of tabs it declares is dropped, and the refusal does not tell
	// the user to write one.
	p = config.LoadProject(write(t, t.TempDir(), "far.toml", link+logs+"  tabs = [\"tail\"]\n"), nil)
	want = `target "logs" runtime realization has no launch argv, and on a link only the home has tabs`
	if err := refusalOf(p, "logs"); err == nil || err.Error() != want {
		t.Errorf("logs refused = %v\nwant %s", err, want)
	}

	moved := link + `
[[target]]
name = "logs"
  [target.remote.runtime]
  inside = "home"
` + panel
	p = config.LoadProject(write(t, t.TempDir(), "far.toml", moved), nil)
	if probs := config.Problems(p); len(probs) > 0 {
		t.Errorf("Problems = %v, want none after the move", probs)
	}
}

// A link whose home is a window has no target a tab can be inside, and the
// refusal of panels does not name one.
func TestALinkWithAWindowHomeHasNoPlaceForPanels(t *testing.T) {
	body := link + `
[[target]]
name = "home"
home = true
  [target.remote.window]
  launch = ["code"]
  match = { class = "^Code$" }

[[target]]
name = "logs"
  [target.remote.runtime]
  name = "logs"
  match = { title = "^logs$" }
    [[target.remote.runtime.panels]]
    kind = "tool"
    command = ["tail"]
`
	p := config.LoadProject(write(t, t.TempDir(), "far.toml", body), nil)
	want := `target "logs" runtime realization declares panels; only a tab has them, and the home of this link has no runtime realization to open a tab in`
	if err := refusalOf(p, "logs"); err == nil || err.Error() != want {
		t.Errorf("logs refused = %v\nwant %s", err, want)
	}
}

// The derived tab has the name agent, so a home of that name gets no tab and
// is refused for the name. One that launches something needs no tab, and
// keeps the name.
func TestALinkHomeNamedAgentIsRefusedForTheName(t *testing.T) {
	body := link + `
[[target]]
name = "agent"
home = true
  [target.remote.runtime]
  place = "right top 75% 100%"
`
	p := config.LoadProject(write(t, t.TempDir(), "far.toml", body), nil)
	want := `target "agent" is the home of a link, and "agent" is the name of the tab a link opens with; give the home another name`
	if err := refusalOf(p, "agent"); err == nil || err.Error() != want {
		t.Errorf("agent refused = %v\nwant %s", err, want)
	}
	if got := names(p); !slices.Equal(got, []revier.TargetName{"agent"}) {
		t.Errorf("targets = %v, want no tab derived", got)
	}

	p = config.LoadProject(write(t, t.TempDir(), "far.toml", body+"  launch = [\"ssh\", \"buildbox\"]\n"), nil)
	if probs := config.Problems(p); len(probs) > 0 {
		t.Errorf("Problems = %v, want a home that launches whole under any name", probs)
	}
}

// The form shows a link's home with the derived tab. Saved as shown, the list
// is not written; another list, or an active tab, is refused where the load
// would drop it without a word.
func TestSaveProjectTargetOnALinkRefusesTabsOfItsOwn(t *testing.T) {
	root := projectsRoot(t, "", map[string]string{"far.toml": link})
	file := config.ProjectFile(root, "far")
	p, err := config.ReadProject(file, nil)
	if err != nil {
		t.Fatalf("ReadProject: %v", err)
	}
	const want = `a link opens with the tab "agent" alone, and has no tabs or active of its own`
	for _, change := range []func(*revier.Realization){
		func(r *revier.Realization) { r.Tabs = []revier.TargetName{"tickets", "agent"} },
		func(r *revier.Realization) { r.Active = "agent" },
	} {
		home := p.Targets[0].Target
		r := *home.Runtime
		change(&r)
		home.Runtime = &r
		if _, err := config.SaveProjectTarget(file, "home", config.TargetEdit{Target: home}, nil); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("err = %v, want %s", err, want)
		}
	}
	if got := read(t, file); got != link {
		t.Errorf("file =\n%s\nwant it unchanged", got)
	}
}

// A link has no list of tabs of its own: the one its home declares is
// dropped, and the home lists the derived tab alone (decisions.md D128).
func TestALinkHomeDropsItsTabs(t *testing.T) {
	body := link + `
[[target]]
name = "home"
home = true
  [target.remote.runtime]
  tabs = ["tickets"]
  active = "tickets"
`
	p := config.LoadProject(write(t, t.TempDir(), "far.toml", body), nil)
	if probs := config.Problems(p); len(probs) > 0 {
		t.Fatalf("Problems = %v, want none", probs)
	}
	home, _ := p.Home()
	if !slices.Equal(home.Runtime.Tabs, []revier.TargetName{"agent"}) || home.Runtime.Active != "" {
		t.Errorf("home runtime = %+v, want the derived tab alone and no active tab", home.Runtime)
	}
}

// The tabs a shared target lists for a link are dropped from what its file is
// compared against too: a change to another value of the target is written,
// and is not taken for an attempt to empty the list. The derived list the
// home was shown with is not written either.
func TestSaveProjectTargetOnALinkIgnoresTheSharedTabs(t *testing.T) {
	const cfg = `
[[target]]
name = "home"
home = true
  [target.remote.runtime]
  tabs = ["tickets"]
  active = "tickets"

[[target]]
name = "tickets"
  [target.remote.runtime]
  inside = "home"
  launch = ["taskmgr-ui"]
`
	root := projectsRoot(t, cfg, map[string]string{"far.toml": link})
	file, shared := config.ProjectFile(root, "far"), sharedOf(t, root)
	p, err := config.ReadProject(file, shared)
	if err != nil {
		t.Fatalf("ReadProject: %v", err)
	}
	home := p.Targets[0].Target
	home.Key = "ctrl-h"

	loaded, err := config.SaveProjectTarget(file, "home", config.TargetEdit{Target: home}, shared)
	if err != nil {
		t.Fatalf("SaveProjectTarget: %v", err)
	}
	if got, _ := loaded.Home(); got.Key != "ctrl-h" || !slices.Equal(got.Runtime.Tabs, []revier.TargetName{"agent"}) {
		t.Errorf("home = %+v, want the key written and the derived tab alone", got)
	}
	if got := read(t, file); strings.Contains(got, "tabs") || strings.Contains(got, "active") {
		t.Errorf("file =\n%s\nwant no list of tabs written", got)
	}
}

// A link holds the repository the host records, for a target here that
// renders it. The checkout it names is still the host's, which
// internal/checkout refuses to clone here (decisions.md D83).
func TestALinkKeepsTheHostsGitURL(t *testing.T) {
	body := "path = \"/srv/far\"\ngit_url = \"git@github.com:example/far.git\"\n" + link
	p := config.LoadProject(write(t, t.TempDir(), "far.toml", body), nil)
	if p.GitURL != "git@github.com:example/far.git" {
		t.Errorf("git_url = %q, want the host's", p.GitURL)
	}
	bad := "path = \"/srv/far\"\ngit_url = \"git@github.com:$(id).git\"\n" + link
	if err := loadErr(write(t, t.TempDir(), "bad.toml", bad), nil); err == nil {
		t.Error("a git_url a link records is validated as any other")
	}
}

// `revier link` records what the host says about the project, so a target
// here renders a path and a repository that name nothing on this machine.
func TestCreateLinkRecordsTheHostsPathAndRepository(t *testing.T) {
	root := t.TempDir()
	p, err := config.CreateLink(root, "far", "buildbox", revier.Project{
		Name:   "far",
		Path:   "/home/user/dev/far",
		GitURL: "git@github.com:example/far.git",
	})
	if err != nil {
		t.Fatalf("CreateLink: %v", err)
	}
	if p.Path != "/home/user/dev/far" || p.GitURL != "git@github.com:example/far.git" {
		t.Errorf("link = %+v, want the host's path and repository", p.Project)
	}
	body, err := os.ReadFile(config.ProjectFile(root, "far"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `path = "/home/user/dev/far"`) || !strings.Contains(string(body), "git_url =") {
		t.Errorf("file = %q, want the host's answer written into it", body)
	}
}

func TestHostIsValidatedAtLoad(t *testing.T) {
	for _, tc := range []struct{ name, host, want string }{
		{"flag", "-oProxyCommand=x", "dash"},
		{"whitespace", "build box", "whitespace"},
	} {
		body := strings.Replace(link, `host = "buildbox"`, `host = "`+tc.host+`"`, 1)
		err := loadErr(write(t, t.TempDir(), tc.name+".toml", body), nil)
		if err == nil || !strings.Contains(err.Error(), "remote.host:") || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want one naming remote.host and %q", tc.name, err, tc.want)
		}
	}
	for _, host := range []string{"buildbox", "hans@build.example.com", "10.0.0.7"} {
		body := strings.Replace(link, `host = "buildbox"`, `host = "`+host+`"`, 1)
		if err := loadErr(write(t, t.TempDir(), "ok.toml", body), nil); err != nil {
			t.Errorf("%s: %v", host, err)
		}
	}
}

// loadErr is every reason a file did not load whole, joined: what LoadProject
// returned as its error before a project file's mistake stopped costing the
// project, or the set, more than the part that is wrong (decisions.md D85).
func loadErr(path string, shared []map[string]any) error {
	return errors.Join(config.Problems(config.LoadProject(path, shared))...)
}
