package tui_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/internal/state"
	"github.com/hk9890/revier/internal/theme"
	"github.com/hk9890/revier/internal/tui"
	"github.com/hk9890/revier/pkg/revier"
)

// Without the focus nobody reads the surface closely, so it refreshes every
// ten seconds instead of every second, and with the focus back at the
// refresh again.
func TestWithoutTheFocusTheSurfaceRefreshesSlowly(t *testing.T) {
	_, _, c, projects := world(t, 3)
	m := refreshed(t, c, projects, stateWith(t, nil), nil)
	if got := m.Interval(); got != time.Second {
		t.Fatalf("interval with the focus = %v, want the refresh", got)
	}
	m, _ = update(m, tea.BlurMsg{})
	if got := m.Interval(); got != 10*time.Second {
		t.Errorf("interval without the focus = %v, want 10s", got)
	}
	m, _ = update(m, tea.FocusMsg{})
	if got := m.Interval(); got != time.Second {
		t.Errorf("interval with the focus back = %v, want the refresh", got)
	}
}

// Without the focus the pointer is reported only while a button is held: a
// pointer that only moves would rebuild the surface for every cell it
// crosses, with nobody at it. The hover needs every motion back with the
// focus.
func TestWithoutTheFocusOnlyAHeldPointerIsReported(t *testing.T) {
	_, _, c, projects := world(t, 3)
	m := refreshed(t, c, projects, stateWith(t, nil), nil)
	if got := m.View().MouseMode; got != tea.MouseModeAllMotion {
		t.Fatalf("mouse mode with the focus = %v, want every motion", got)
	}
	m, _ = update(m, tea.BlurMsg{})
	if got := m.View().MouseMode; got != tea.MouseModeCellMotion {
		t.Errorf("mouse mode without the focus = %v, want motion with a button held only", got)
	}
	m, _ = update(m, tea.FocusMsg{})
	if got := m.View().MouseMode; got != tea.MouseModeAllMotion {
		t.Errorf("mouse mode with the focus back = %v, want every motion", got)
	}
}

// The focus regained surveys at once, and the slow tick that was out is
// dropped when it comes: one chain runs, at the refresh.
func TestTheFocusRegainedSurveysAtOnce(t *testing.T) {
	_, _, c, projects := world(t, 3)
	m := refreshed(t, c, projects, stateWith(t, nil), nil)
	m, _ = update(m, tea.BlurMsg{})
	slow := m.Ticked()

	m, cmd := update(m, tea.FocusMsg{})
	if cmd == nil {
		t.Fatal("the focus regained asked for nothing")
	}
	if _, again := update(m, tea.FocusMsg{}); again != nil {
		t.Error("a second focus report started a second survey")
	}
	if _, dropped := update(m, slow); dropped != nil {
		t.Error("the slow tick that was out surveyed again after the focus came back")
	}
	// The files first, as the assistant beside the surface writes them; the
	// survey after them.
	if m, cmd = deliver(m, cmd); cmd == nil {
		t.Fatal("the files read again at the focus regained started no survey")
	}
	if m, cmd = deliver(m, cmd); cmd == nil {
		t.Fatal("the survey of the focus regained scheduled no refresh")
	}
	if _, next := update(m, m.Ticked()); next == nil {
		t.Error("the tick the survey scheduled surveyed nothing")
	}
}

// A focus report while a survey is out starts no second one: the survey
// answers soon, and its answer schedules the refresh.
func TestTheFocusRegainedWithASurveyOutStartsNoOther(t *testing.T) {
	_, _, c, projects := world(t, 3)
	m := refreshed(t, c, projects, stateWith(t, nil), nil)
	m, _ = update(m, tea.BlurMsg{})
	m, survey := update(m, m.Ticked())
	if survey == nil {
		t.Fatal("the tick surveyed nothing")
	}
	m, cmd := update(m, tea.FocusMsg{})
	if got := m.View().MouseMode; got != tea.MouseModeAllMotion {
		t.Errorf("mouse mode after the focus regained = %v, want the pointer reports back", got)
	}
	each(cmd, func(msg tea.Msg) {
		if fmt.Sprintf("%T", msg) == "tui.surveyMsg" {
			t.Error("the focus regained surveyed beside the survey that was out")
		}
	})
	if m, cmd = deliver(m, survey); cmd == nil {
		t.Fatal("the survey that was out scheduled no refresh")
	}
	if got := m.Interval(); got != time.Second {
		t.Errorf("interval after the survey that was out = %v, want the refresh", got)
	}
}

// A launch still coming up is claimed by the survey (decisions.md D87), so a
// surface without the focus keeps the refresh, and a hidden popup keeps
// surveying, until the launch is claimed or expires.
func TestALaunchComingUpKeepsTheRefresh(t *testing.T) {
	_, wm, c, projects := world(t, 2)
	wm.Add("revier", core.PopupClass)
	root := stateWith(t, nil)
	m := tui.New(c, projects, root, &config.Config{}, time.Second, theme.Default(), "").WithPopup().StaticCursors()
	m = survey(m)
	st, _ := state.Load(root)
	st.Launch = &state.Launch{Project: "project-00", At: time.Now()}
	if err := st.Save(root); err != nil {
		t.Fatal(err)
	}
	m, _ = update(m, tea.BlurMsg{})
	m = survey(m)
	if got := m.Interval(); got != time.Second {
		t.Errorf("interval without the focus with a launch coming up = %v, want the refresh", got)
	}

	m, hide := press(m, "esc")
	m = run(m, hide)
	if _, cmd := update(m, m.Survey()()); cmd == nil {
		t.Error("a hidden popup with a launch coming up ended its survey chain")
	}
}

// A launch past its window keeps nothing: one for a project this surface
// does not hold is never settled, and would keep the refresh for good.
func TestALaunchPastItsWindowKeepsNoRefresh(t *testing.T) {
	_, _, c, projects := world(t, 2)
	root := stateWith(t, nil)
	m := tui.New(c, projects, root, &config.Config{}, time.Second, theme.Default(), "").StaticCursors()
	m = survey(m)
	st, _ := state.Load(root)
	st.Launch = &state.Launch{Project: "not-here", At: time.Now().Add(-time.Hour)}
	if err := st.Save(root); err != nil {
		t.Fatal(err)
	}
	m, _ = update(m, tea.BlurMsg{})
	m = survey(m)
	if got := m.Interval(); got != 10*time.Second {
		t.Errorf("interval without the focus with a launch an hour old = %v, want 10s", got)
	}
}

// The popup can hide while a tick set without the focus is out, ten seconds
// long. The raise drops it and surveys once the files are read again, as it
// does when the chain had ended.
func TestTheRaiseDropsTheSlowTickThatWasOut(t *testing.T) {
	t.Setenv("REVIER_CONFIG_HOME", t.TempDir())
	_, wm, c, projects := world(t, 3)
	wm.Add("revier", core.PopupClass)
	m := tui.New(c, projects, stateWith(t, nil), &config.Config{}, time.Second, theme.Default(), "").WithPopup().StaticCursors()
	m = survey(m)
	m, _ = update(m, tea.BlurMsg{})
	m, _ = update(m, m.Survey()())
	slow := m.Ticked()
	m, hide := press(m, "esc")
	m = run(m, hide)

	m, cmd := update(m, tea.FocusMsg{})
	m, cmd = deliver(m, cmd)
	surveyed := false
	each(cmd, func(msg tea.Msg) {
		if fmt.Sprintf("%T", msg) == "tui.surveyMsg" {
			surveyed = true
		}
	})
	if !surveyed {
		t.Error("the raise surveyed nothing; the rows waited for the slow tick")
	}
	if _, dropped := update(m, slow); dropped != nil {
		t.Error("the slow tick that was out surveyed again after the raise")
	}
}

// The working spinner redraws the surface eight times a second; without the
// focus it stands, and a working agent shows the theme's own glyph, until
// the focus is back.
func TestTheSpinnerStopsWithoutTheFocus(t *testing.T) {
	_, _, c, projects := world(t, 3)
	c.Probes[0].(*hosttest.FakeProbe).State.Status = revier.StatusRunning
	m := refreshed(t, c, projects, stateWith(t, nil), nil)
	if _, cmd := update(m, tui.Spin()); cmd == nil {
		t.Fatal("a working agent with the focus scheduled no spin")
	}
	m, _ = update(m, tea.BlurMsg{})
	if _, cmd := update(m, tui.Spin()); cmd != nil {
		t.Error("the spinner went on without the focus")
	}
	if view := drawn(m); !strings.Contains(view, theme.Default().Glyphs.Working) {
		t.Errorf("a working agent without the focus does not show the working glyph:\n%s", view)
	}
	m, _ = update(m, tea.FocusMsg{})
	if _, cmd := update(m, tui.Spin()); cmd == nil {
		t.Error("the spinner did not go on with the focus back")
	}
}

// The focus regained reads the mirrored panel at once, not at the end of the
// slow tick that was out.
func TestTheFocusRegainedReadsTheMirrorAtOnce(t *testing.T) {
	m, _, _ := listedWorld(t, 160, 30,
		listed{project: "alpha", status: revier.StatusRunning, on: "one", screen: "first screen"})
	m = m.MirrorTicked()
	m, _ = update(m, tea.BlurMsg{})
	asked := m.MirrorAsked()
	m, _ = update(m, tea.FocusMsg{})
	if got := m.MirrorAsked(); got != asked+1 {
		t.Errorf("reads asked for = %d after the focus came back, want %d", got, asked+1)
	}
}

// A blur report with other input behind it in the same read is the blur all
// the same, as a focus report is. The messages are the ones bubbletea itself
// makes of those bytes, so its next version cannot change them unseen.
func TestABlurReportWithAMouseReportBehindItIsTheBlur(t *testing.T) {
	var read []tea.Msg
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	program := tea.NewProgram(recorder{&read}, tea.WithContext(ctx), tea.WithoutSignalHandler(),
		tea.WithInput(strings.NewReader("\x1b[O\x1b[<35;10;10M")), tea.WithOutput(io.Discard))
	if _, err := program.Run(); err != nil {
		t.Fatalf("bubbletea read no mouse report behind the blur report: %v", err)
	}

	_, _, c, projects := world(t, 3)
	m := refreshed(t, c, projects, stateWith(t, nil), nil)
	for _, msg := range read {
		m, _ = update(m, msg)
	}
	if got := m.Interval(); got != 10*time.Second {
		t.Errorf("interval after the blur report = %v, want 10s; bubbletea handed over %#v", got, read)
	}
}

// A press that brings a target to the front is done with the popup, so the
// popup hides; a failed one stays to be read, and a surface that is not the
// popup has nothing to hide.
func TestAGoFromThePopupHidesIt(t *testing.T) {
	_, wm, c, projects := world(t, 2)
	popup := wm.Add("revier", core.PopupClass)
	m := tui.New(c, projects, stateWith(t, nil), &config.Config{}, time.Second, theme.Default(), "").WithPopup().StaticCursors()
	m = survey(m)
	m, _ = press(m, "alt+t")
	m, _ = press(m, "down") // editor
	m, cmd := press(m, "enter")
	if cmd == nil {
		t.Fatal("enter on a target returned no command")
	}
	m, hide := deliver(m, cmd)
	m = run(m, hide)
	if len(wm.Hidden) != 1 || wm.Hidden[0] != popup {
		t.Errorf("hidden = %v after a go, want the popup", wm.Hidden)
	}

	wm.Hidden = nil
	wm.OpenErr, wm.FocusErr = errors.New("no editor"), errors.New("no editor")
	m, _ = update(m, tea.FocusMsg{})
	m, _ = press(m, "alt+t")
	m, _ = press(m, "down")
	m, cmd = press(m, "enter")
	if cmd == nil {
		t.Fatal("enter on a target returned no command")
	}
	m = run(m, cmd)
	if len(wm.Hidden) != 0 {
		t.Errorf("hidden = %v after a failed go, want the popup on the screen", wm.Hidden)
	}
	_ = m
}

// A go that lands after the user hid the popup and raised it again leaves it
// on the screen: the user raised it for the next errand.
func TestALateGoDoesNotHideAPopupRaisedSinceThePress(t *testing.T) {
	_, wm, c, projects := world(t, 2)
	wm.Add("revier", core.PopupClass)
	m := tui.New(c, projects, stateWith(t, nil), &config.Config{}, time.Second, theme.Default(), "").WithPopup().StaticCursors()
	m = survey(m)
	m, _ = press(m, "alt+t")
	m, _ = press(m, "down") // editor
	m, slow := press(m, "enter")
	if slow == nil {
		t.Fatal("enter on a target returned no command")
	}
	m, _ = press(m, "esc") // out of the pane
	m, hide := press(m, "esc")
	m = run(m, hide)
	if len(wm.Hidden) != 1 {
		t.Fatalf("hidden = %v after esc, want the popup", wm.Hidden)
	}
	m, _ = update(m, tea.FocusMsg{})

	if _, cmd := deliver(m, slow); cmd != nil {
		t.Error("a go that landed past the raise answered with a command, want none: the popup stays")
	}
}

// A go that landed while the popup could not hide leaves the popup where it
// is, with its query and cursor: only Esc, which had nothing else to do,
// quits instead.
func TestAGoFromAPopupThatCannotHideStays(t *testing.T) {
	_, _, c, projects := world(t, 2) // no popup window: the hide fails
	m := tui.New(c, projects, stateWith(t, nil), &config.Config{}, time.Second, theme.Default(), "").WithPopup().StaticCursors()
	m = survey(m)
	m, _ = press(m, "alt+t")
	m, _ = press(m, "down")
	m, cmd := press(m, "enter")
	if cmd == nil {
		t.Fatal("enter on a target returned no command")
	}
	m, hide := deliver(m, cmd)
	if hide == nil {
		t.Fatal("the go asked for no hide")
	}
	quit := false
	each(hide, func(msg tea.Msg) {
		if _, after := update(m, msg); after != nil {
			if _, ok := after().(tea.QuitMsg); ok {
				quit = true
			}
		}
	})
	if quit {
		t.Error("a go from a popup that could not hide quit the popup")
	}
}

// Outside the popup a go hides nothing.
func TestAGoOutsideThePopupHidesNothing(t *testing.T) {
	_, wm, c, projects := world(t, 2)
	m := refreshed(t, c, projects, stateWith(t, nil), nil)
	m, _ = press(m, "alt+t")
	m, _ = press(m, "down")
	m, cmd := press(m, "enter")
	if cmd == nil {
		t.Fatal("enter on a target returned no command")
	}
	m = run(m, cmd)
	if len(wm.Hidden) != 0 {
		t.Errorf("hidden = %v after a go outside the popup, want nothing", wm.Hidden)
	}
	_ = m
}
