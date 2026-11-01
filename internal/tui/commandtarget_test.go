package tui_test

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/internal/session"
	"github.com/hk9890/revier/internal/theme"
	"github.com/hk9890/revier/internal/tui"
	"github.com/hk9890/revier/pkg/revier"
)

// ran is one run of a command target's command, as the core asked for it.
type ran struct {
	argv []string
	dir  string
}

// commandWorld is one project, work, with a home and a command target, web,
// and the runs its core makes.
func commandWorld() (*core.Core, []core.Project, *[]ran) {
	var runs []ran
	c := &core.Core{Runtime: hosttest.NewRuntime("rt"), Window: hosttest.New("wm"), Run: func(_ context.Context, argv []string, dir string) error {
		runs = append(runs, ran{argv: argv, dir: dir})
		return nil
	}}
	projects := core.Prepare([]revier.Project{{Name: "work", Path: "/p/work", Targets: []revier.Target{
		{Name: "home", Home: true, Runtime: &revier.Realization{Name: "work", Launch: []string{"x"}, Match: revier.Match{Title: "^work$"}}},
		{Name: "web", Key: "ctrl-shift-i", Command: &revier.Command{Run: []string{"repo-web", "{{.Name}}"}}},
	}}})
	return c, projects, &runs
}

// A command target's row says it runs a command, never that it is stopped
// or has no host, and Enter on it runs the command in the project's
// directory.
func TestEnterOnACommandTargetRunsItsCommand(t *testing.T) {
	c, projects, runs := commandWorld()
	m := refreshed(t, c, projects, stateWith(t, nil), nil)
	m, _ = press(m, "alt+t")
	m, _ = press(m, "down") // web
	row := selectedRow(t, m)
	if !strings.Contains(row, "web") || !strings.Contains(row, "command") || strings.Contains(row, "stopped") {
		t.Errorf("row = %q, want web saying command", row)
	}
	m, cmd := press(m, "enter")
	run(m, cmd)
	want := []ran{{argv: []string{"repo-web", "work"}, dir: "/p/work"}}
	if !slices.EqualFunc(*runs, want, func(a, b ran) bool { return slices.Equal(a.argv, b.argv) && a.dir == b.dir }) {
		t.Errorf("runs = %v, want %v", *runs, want)
	}
}

// A session that held a target that runs a command now plans to skip it,
// says why, and does not call it stopped.
func TestTheRestorePlanSkipsACommandTarget(t *testing.T) {
	c, projects, runs := commandWorld()
	root := stateWith(t, nil)
	saved(t, root, "work", 10, session.Project{Name: "work", Targets: []session.Target{{Name: "web"}}})
	m := resize(refreshed(t, c, projects, root, nil), 150, 30)
	m, _ = press(m, "alt+s")

	g := theme.Default().Glyphs
	found := false
	for _, line := range strings.Split(pane(m), "\n") {
		found = found || strings.Contains(line, g.Skip+" skip") && strings.Contains(line, "web") &&
			!strings.Contains(line, "stopped") && strings.Contains(line, "runs a command; not restored")
	}
	if !found {
		t.Errorf("pane does not skip web for its command:\n%s", pane(m))
	}
	if len(*runs) != 0 {
		t.Errorf("runs = %v, want the plan to run nothing", *runs)
	}
}

// The form's last section is the command: a new target given a run and a
// timeout is written as [target.command] and listed with its command.
func TestTheConfigScreenAddsACommandTarget(t *testing.T) {
	m, root := targetsSurface(t)
	m, _ = press(onTargetRow(m, 3), "enter")
	m = typeInto(m, "web")
	m = downs(m, 17) // name, key, home, the runtime's eight fields, add a panel, the window's five: the command's run
	m = typeInto(m, "repo-web {{.Name}}")
	m = downs(m, 1)
	m = typeInto(m, "5s")
	m, _ = press(m, "enter")

	want := sharedTargets + "\n[[target]]\nname = \"web\"\n  [target.command]\n  run = [\"repo-web\", \"{{.Name}}\"]\n  timeout = \"5s\"\n"
	if got := fileText(t, config.File(root)); got != want {
		t.Errorf("config.toml =\n%s\nwant\n%s", got, want)
	}
	if !strings.Contains(screen(m), "command: repo-web {{.Name}}") {
		t.Errorf("config screen does not list the new target:\n%s", screen(m))
	}
}

// A window target becomes a command target when its window's fields are
// emptied and a run is given: the window's table goes, its comment stays, and
// the command's table comes.
func TestTheConfigScreenMakesAWindowTargetACommand(t *testing.T) {
	m, root := targetsSurface(t)
	m, _ = press(onTargetRow(m, 1), "enter")
	m = clearField(downs(m, 13)) // the window's command
	m = clearField(downs(m, 2))  // its match class
	m = downs(m, 2)              // the command's run
	m = typeInto(m, "repo-web")
	m, _ = press(m, "enter")

	want := strings.Replace(sharedTargets, `  [target.window]
  launch = ["idea", "{{.Path}}"] # IntelliJ
  match = { class = "^jetbrains-idea" }
`, `  # IntelliJ
  [target.command]
  run = ["repo-web"]
`, 1)
	if got := fileText(t, config.File(root)); got != want {
		t.Errorf("config.toml =\n%s\nwant\n%s", got, want)
	}
	if !strings.Contains(screen(m), "command: repo-web") {
		t.Errorf("config screen does not show the command:\n%s", screen(m))
	}
}

// A command target's form shows its run and timeout, and a change to one is
// written in place.
func TestTheConfigScreenChangesACommandTarget(t *testing.T) {
	const web = "\n[[target]]\nname = \"web\"\n  [target.command]\n  run = [\"repo-web\", \"{{.Name}}\"]\n  timeout = \"5s\"\n"
	m, file, _ := projectSurfaceOver(t, sharedTargets+web, "path = \"/tmp/demo\"\n", hosttest.NewRuntime("rt"))
	root := filepath.Dir(filepath.Dir(file))
	m, _ = press(onTargetRow(m, 3), "enter")
	if !strings.Contains(screen(m), "repo-web {{.Name}}") {
		t.Errorf("the form does not show the run:\n%s", screen(m))
	}
	m = downs(m, 18) // the command's timeout
	m = typeInto(clearField(m), "20s")
	_, _ = press(m, "enter")

	want := sharedTargets + strings.Replace(web, `"5s"`, `"20s"`, 1)
	if got := fileText(t, config.File(root)); got != want {
		t.Errorf("config.toml =\n%s\nwant\n%s", got, want)
	}
}

// A command the form cannot write is refused there with the reason, and
// nothing is written: one beside a window, a home, and a timeout that is no
// duration or is too long.
func TestTheConfigScreenRefusesACommandItCannotWrite(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(m tui.Model) tui.Model
		want string
	}{
		{"beside a window", func(m tui.Model) tui.Model {
			return typeInto(downs(m, 17), "repo-web")
		}, "not both"},
		{"home", func(m tui.Model) tui.Model {
			m = clearField(downs(m, 13))
			m = clearField(downs(m, 2))
			m = typeInto(downs(m, 2), "repo-web")
			for range 15 { // up to home
				m, _ = press(m, "up")
			}
			m, _ = press(m, "right") // home to yes
			return m
		}, "is no home"},
		{"no duration", func(m tui.Model) tui.Model {
			m = clearField(downs(m, 13))
			m = clearField(downs(m, 2))
			m = typeInto(downs(m, 2), "repo-web")
			return typeInto(downs(m, 1), "soon")
		}, "is no duration"},
		{"too long", func(m tui.Model) tui.Model {
			m = clearField(downs(m, 13))
			m = clearField(downs(m, 2))
			m = typeInto(downs(m, 2), "repo-web")
			return typeInto(downs(m, 1), "1m")
		}, "longer than"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, root := targetsSurface(t)
			m, _ = press(onTargetRow(m, 1), "enter")
			m = tc.edit(m)
			m, _ = press(m, "enter")
			if !strings.Contains(footer(m), tc.want) {
				t.Errorf("footer = %q, want %q", footer(m), tc.want)
			}
			if got := fileText(t, config.File(root)); got != sharedTargets {
				t.Errorf("config.toml changed:\n%s", got)
			}
		})
	}
}
