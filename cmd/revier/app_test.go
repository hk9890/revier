package main

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/internal/state"
	"github.com/hk9890/revier/pkg/revier"
)

// press is one keypress process: an app over the state as it is on disk, as
// a desktop binding starts one, running goTarget once.
func press(t *testing.T, c *core.Core, p core.Project, root string, name revier.TargetName) revier.TargetRef {
	t.Helper()
	st, err := state.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	a := &app{cfg: &config.Config{}, projects: []core.Project{p}, state: st, stateRoot: root, core: c}
	ref, err := a.goTarget(context.Background(), p, name)
	if err != nil {
		t.Fatalf("goTarget %s: %v", name, err)
	}
	return ref
}

// demoProject is a workspace and a diff pane on the runtime, and an editor
// window.
func demoProject(t *testing.T) core.Project {
	t.Helper()
	p, err := core.PrepareProject(revier.Project{Name: "demo", Path: t.TempDir(), Targets: []revier.Target{
		{Name: "home", Home: true, Runtime: &revier.Realization{
			Name: "home", Launch: []string{"x"}, Match: revier.Match{Title: "^home$"}}},
		{Name: "diff", Key: "ctrl-shift-d", Runtime: &revier.Realization{
			Name: "diff", Launch: []string{"x"}, Match: revier.Match{Title: "^diff$"}}},
		{Name: "editor", Key: "ctrl-shift-o", Window: &revier.Realization{
			Launch: []string{"code"}, Match: revier.Match{Class: "^code$"}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// The press that toggles back goes home, and pins home. Pinned to diff, the
// home window would be where the diff key lands from then on: every later
// press would find home through the binding and toggle back to it again.
func TestAToggleBackPinsHomeNotThePressedTarget(t *testing.T) {
	rt := hosttest.NewRuntime("tmux")
	homeRef := rt.Add("home", "")
	diffRef := rt.Add("diff", "")
	c := &core.Core{Runtime: rt}
	p, root := demoProject(t), t.TempDir()

	rt.SetFocus(diffRef)
	if got := press(t, c, p, root, "diff"); got != homeRef {
		t.Fatalf("second press = %v, want home %v", got, homeRef)
	}
	if got := press(t, c, p, root, "diff"); got != diffRef {
		t.Errorf("third press = %v, want diff %v: the key must still reach its target", got, diffRef)
	}
}

// lateWindows is a window host whose windows appear later than the launch
// that asked for them: Open starts nothing it can list yet.
type lateWindows struct {
	*hosttest.Fake
	opened int
}

func (l *lateWindows) Open(context.Context, revier.Realization) (revier.TargetRef, error) {
	l.opened++
	return revier.TargetRef{}, nil
}

// A press while the target's launch is still coming up reports it rather than
// launching a second copy - also when state still holds a binding from a
// window of that target that has since closed, which is what made this
// launch necessary.
func TestASecondPressDuringALaunchReportsItComingUp(t *testing.T) {
	wm := &lateWindows{Fake: hosttest.New("gnome")}
	c := &core.Core{Runtime: hosttest.NewRuntime("tmux"), Window: wm}
	p, root := demoProject(t), t.TempDir()
	st := &state.State{Launch: &state.Launch{Project: "demo", Target: "editor", At: time.Now().Add(-3 * time.Second)}}
	st.Bind("demo", "editor", revier.TargetRef{Host: "gnome", ID: "99"}) // a window closed since
	if err := st.Save(root); err != nil {
		t.Fatal(err)
	}

	if ref := press(t, c, p, root, "editor"); !ref.IsZero() {
		t.Errorf("ref = %v, want none: the target is still coming up", ref)
	}
	if wm.opened != 0 {
		t.Errorf("the editor was launched %d more times, want none", wm.opened)
	}
}

// The first press gave up waiting before the window came up. Once it is there,
// the next press raises it, launch on record or not: only a second launch is
// refused, never the raise.
func TestAPressDuringALaunchRaisesTheWindowOnceItIsThere(t *testing.T) {
	wm := &lateWindows{Fake: hosttest.New("gnome")}
	editor := wm.Add("demo - README.md", "code")
	c := &core.Core{Runtime: hosttest.NewRuntime("tmux"), Window: wm}
	p, root := demoProject(t), t.TempDir()
	st := &state.State{Launch: &state.Launch{Project: "demo", Target: "editor", At: time.Now().Add(-40 * time.Second)}}
	if err := st.Save(root); err != nil {
		t.Fatal(err)
	}

	if ref := press(t, c, p, root, "editor"); ref != editor {
		t.Errorf("ref = %v, want the editor window %v raised", ref, editor)
	}
	if wm.opened != 0 {
		t.Errorf("the editor was launched %d more times, want none", wm.opened)
	}
	if got, _ := state.Load(root); got.Launch != nil {
		t.Errorf("launch = %+v, want it consumed: the target has landed", got.Launch)
	}
}

// `revier list` drops refs to windows that are gone, and the drop reaches the
// file: the state saved is the state pruned, not a fresh copy from disk.
func TestListSavesItsPrune(t *testing.T) {
	wm := hosttest.New("gnome")
	c := &core.Core{Runtime: hosttest.NewRuntime("tmux"), Window: wm}
	p, root := demoProject(t), t.TempDir()
	gone := revier.TargetRef{Host: "gnome", ID: "99"}
	st := &state.State{}
	st.Attach("demo", gone)
	st.Bind("demo", "editor", gone)
	if err := st.Save(root); err != nil {
		t.Fatal(err)
	}
	loaded, err := state.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	a := &app{cfg: &config.Config{}, projects: []core.Project{p}, state: loaded, stateRoot: root, core: c}

	stdout := os.Stdout
	devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = devnull // the listing itself is not what is under test
	err = cmdList(context.Background(), a, []string{"--json"})
	os.Stdout = stdout
	_ = devnull.Close()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	got, err := state.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Attached) != 0 || len(got.Bound) != 0 {
		t.Errorf("state on disk = attached %v, bound %v; want both pruned", got.Attached, got.Bound)
	}
}

// `revier list --json` carries what was attached to a project, marked so, as
// the TUI lists it under the project.
func TestListJSONCarriesAttachments(t *testing.T) {
	wm := hosttest.New("gnome")
	stray := wm.Add("Pull requests", "chromium")
	c := &core.Core{Runtime: hosttest.NewRuntime("tmux"), Window: wm}
	p, root := demoProject(t), t.TempDir()
	st := &state.State{}
	st.Attach("demo", stray)
	a := &app{cfg: &config.Config{}, projects: []core.Project{p}, state: st, stateRoot: root, core: c}

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout := os.Stdout
	os.Stdout = w
	err = cmdList(context.Background(), a, []string{"--json"})
	os.Stdout = stdout
	_ = w.Close()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var views []revier.ProjectView
	if err := json.NewDecoder(r).Decode(&views); err != nil {
		t.Fatal(err)
	}
	var attached []revier.TargetView
	for _, tv := range views[0].Targets {
		if tv.Attached {
			attached = append(attached, tv)
		}
	}
	if len(attached) != 1 || attached[0].Ref != stray {
		t.Errorf("attached targets = %+v, want the stray window %v", attached, stray)
	}
}
