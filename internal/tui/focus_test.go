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
	m := tui.NewWithoutWaits(c, projects, root, &config.Config{}, time.Second, theme.Default(), "").WithPopup().StaticCursors()
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
	m := tui.NewWithoutWaits(c, projects, root, &config.Config{}, time.Second, theme.Default(), "").StaticCursors()
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
	m := tui.NewWithoutWaits(c, projects, stateWith(t, nil), &config.Config{}, time.Second, theme.Default(), "").WithPopup().StaticCursors()
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
	m := tui.NewWithoutWaits(c, projects, stateWith(t, nil), &config.Config{}, time.Second, theme.Default(), "").WithPopup().StaticCursors()
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

// A go that lands after the popup hid and was raised again leaves it on the
// screen, with no press since its own: the user raised it for the next
// errand.
func TestALateGoDoesNotHideAPopupRaisedSinceThePress(t *testing.T) {
	_, wm, c, projects := world(t, 2)
	wm.Add("revier", core.PopupClass)
	m := tui.NewWithoutWaits(c, projects, stateWith(t, nil), &config.Config{}, time.Second, theme.Default(), "").WithPopup().StaticCursors()
	m = survey(m)
	m, _ = press(m, "alt+t")
	m, _ = press(m, "down") // editor
	m, slow := press(m, "enter")
	if slow == nil {
		t.Fatal("enter on a target returned no command")
	}
	m = m.Raised()

	if _, cmd := deliver(m, slow); cmd != nil {
		t.Error("a go that landed past the raise answered with a command, want none: the popup stays")
	}
}

// A go that lands after the next press leaves the popup on the screen: the
// press began the user's next errand, such as a delete still to be answered.
func TestALateGoDoesNotHideAPopupPressedSinceThePress(t *testing.T) {
	c := &core.Core{Runtime: hosttest.NewRuntime("rt"), Window: hosttest.New("wm")}
	c.Window.(*hosttest.Fake).Add("revier", core.PopupClass)
	projects, _ := onDisk(t, []string{"alpha", "beta"}, nil, nil)
	m := tui.NewWithoutWaits(c, projects, stateWith(t, nil), &config.Config{}, time.Second, theme.Default(), "").WithPopup().StaticCursors()
	m = survey(m)
	m, slow := press(m, "enter")
	if slow == nil {
		t.Fatal("enter on a project returned no command")
	}
	m, _ = press(m, "alt+d")

	m, cmd := deliver(m, slow)
	if cmd != nil {
		t.Error("a go that landed past a press answered with a command, want none: the popup stays")
	}
	if f := footer(m); !strings.Contains(f, "delete alpha?") {
		t.Errorf("footer = %q, want the delete question still asked", f)
	}
}

// A go that lands after a wheel turn, or after the trigger key brought the
// popup still on the screen back to the front, leaves the popup there.
func TestALateGoDoesNotHideAPopupScrolledOrRefocusedSinceThePress(t *testing.T) {
	for name, since := range map[string]tea.Msg{
		"wheel": tea.MouseWheelMsg{X: 2, Y: 4, Button: tea.MouseWheelDown},
		"focus": tea.FocusMsg{},
	} {
		t.Run(name, func(t *testing.T) {
			_, wm, c, projects := world(t, 2)
			wm.Add("revier", core.PopupClass)
			m := tui.NewWithoutWaits(c, projects, stateWith(t, nil), &config.Config{}, time.Second, theme.Default(), "").WithPopup().StaticCursors()
			m = survey(m)
			m, slow := press(m, "enter")
			if slow == nil {
				t.Fatal("enter on a project returned no command")
			}
			m, _ = update(m, tea.BlurMsg{})
			m, _ = update(m, since)
			if _, cmd := deliver(m, slow); cmd != nil {
				t.Error("a go that landed past it answered with a command, want none: the popup stays")
			}
		})
	}
}

// A go that landed while the popup could not hide leaves the popup where it
// is, with its query and cursor: only Esc, which had nothing else to do,
// quits instead.
func TestAGoFromAPopupThatCannotHideStays(t *testing.T) {
	_, _, c, projects := world(t, 2) // no popup window: the hide fails
	m := tui.NewWithoutWaits(c, projects, stateWith(t, nil), &config.Config{}, time.Second, theme.Default(), "").WithPopup().StaticCursors()
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

// agentsPopup is the popup with the agent list in view and the focus, the
// window host it is listed by, and its window: the raise after a hide opens
// on the projects, and a focus regained leaves the list where it was, so the
// list in view says which of the two ran.
func agentsPopup(t *testing.T, minimized bool) (tui.Model, *hosttest.Fake, revier.TargetRef) {
	t.Helper()
	_, wm, c, projects := world(t, 3)
	popup := wm.AddInstance(revier.Instance{Title: "revier", Class: core.PopupClass, Minimized: minimized})
	m, _ := clocked(tui.NewWithoutWaits(c, projects, stateWith(t, nil), &config.Config{}, time.Second, theme.Default(), "").
		WithPopup().StaticCursors())
	m = switched(survey(m))
	if q := query(m); !strings.Contains(q, "filter agents") {
		t.Fatalf("query line = %q, want the agent list", q)
	}
	return m, wm, popup
}

// onAgents reports whether the agent list is in view: the popup was not taken
// as hidden and raised.
func onAgents(m tui.Model) bool { return strings.Contains(query(m), "filter agents") }

// surveys reports whether the next tick of m surveys: a popup taken as
// hidden does not (decisions.md D86).
func surveys(m tui.Model) bool {
	_, cmd := update(m, m.Ticked())
	return cmd != nil
}

// A popup the desktop hid while another window had the focus is found hidden
// by a look after the blur, surveys no more, and is raised as Esc's hide is:
// on the projects, exactly as after Esc.
func TestAHideByTheDesktopIsTakenAsEscs(t *testing.T) {
	escaped, _, _ := agentsPopup(t, false)
	escaped, hide := press(escaped, "esc")
	escaped = run(escaped, hide)
	escaped, _ = update(escaped, tea.BlurMsg{})
	escaped, _ = update(escaped, tea.FocusMsg{})

	overlaid, _, _ := agentsPopup(t, true)
	overlaid, looking := update(overlaid, tea.BlurMsg{})
	if looking == nil {
		t.Fatal("a blurred popup set no look")
	}
	overlaid, look := update(overlaid, overlaid.Looked())
	overlaid = run(overlaid, look)
	if _, cmd := update(overlaid, overlaid.Ticked()); cmd != nil {
		t.Error("a popup the desktop hid still surveys")
	}
	overlaid, _ = update(overlaid, tea.FocusMsg{})

	if onAgents(overlaid) {
		t.Errorf("query line = %q after the raise, want the projects", query(overlaid))
	}
	if got, want := screen(overlaid), screen(escaped); got != want {
		t.Errorf("raised after the desktop's hide:\n%s\nwant as after Esc:\n%s", got, want)
	}
}

// A desktop hide over a query and the targets leaves what Esc leaves: no
// query, and the cursor on the project list.
func TestADesktopHideClearsTheQueryAsEscDoes(t *testing.T) {
	popup := func(minimized bool) tui.Model {
		_, wm, c, projects := world(t, 3)
		wm.AddInstance(revier.Instance{Title: "revier", Class: core.PopupClass, Minimized: minimized})
		m, _ := clocked(tui.NewWithoutWaits(c, projects, stateWith(t, nil), &config.Config{}, time.Second, theme.Default(), "").
			WithPopup().StaticCursors())
		m = survey(m)
		m, _ = press(m, "1")
		m, _ = press(m, "alt+t")
		return m
	}
	escaped := popup(false)
	for range 2 {
		escaped, _ = press(escaped, "esc")
	}
	escaped, hide := press(escaped, "esc")
	escaped = run(escaped, hide)
	escaped, _ = update(escaped, tea.BlurMsg{})
	escaped, _ = update(escaped, tea.FocusMsg{})

	overlaid := popup(true)
	overlaid, _ = update(overlaid, tea.BlurMsg{})
	overlaid, look := update(overlaid, overlaid.Looked())
	overlaid = run(overlaid, look)
	overlaid, _ = update(overlaid, tea.FocusMsg{})

	if q := query(overlaid); !strings.Contains(q, "filter projects") {
		t.Errorf("query line = %q after the raise, want the projects with no query", q)
	}
	if got, want := screen(overlaid), screen(escaped); got != want {
		t.Errorf("raised after the desktop's hide:\n%s\nwant as after Esc:\n%s", got, want)
	}
}

// A look that answers once the focus is back, or a look's time set before
// it, is dropped: the popup is on the screen, whatever the look found.
func TestALookAfterTheFocusIsBackIsDropped(t *testing.T) {
	m, _, _ := agentsPopup(t, true)
	m, _ = update(m, tea.BlurMsg{})
	stale := m.Looked()
	_, look := update(m, stale)
	found := look()
	m, _ = update(m, tea.FocusMsg{})

	if _, cmd := update(m, stale); cmd != nil {
		t.Error("a look's time set before the focus came back looked")
	}
	m, _ = update(m, found)
	if !onAgents(m) {
		t.Errorf("query line = %q, want the popup still on the agent list", query(m))
	}
	m, _ = update(m, tea.BlurMsg{})
	if _, cmd := update(m, m.Ticked()); cmd == nil {
		t.Error("a look that answered late hid the popup")
	}
}

// A popup still on the screen without the focus stays as it is, and looks
// again at each slow refresh: the desktop may hide it later.
func TestAPopupNotHiddenLooksAgainAtEachRefresh(t *testing.T) {
	m, wm, popup := agentsPopup(t, false)
	m, _ = update(m, tea.BlurMsg{})
	m, look := update(m, m.Looked())
	m = run(m, look)
	if !onAgents(m) {
		t.Fatalf("query line = %q, want the popup on the agent list", query(m))
	}

	wm.Remove(popup)
	wm.AddInstance(revier.Instance{Title: "revier", Class: core.PopupClass, Minimized: true})
	calls := wm.InstancesCalls
	m, tick := update(m, m.Ticked())
	m = run(m, tick)
	if got := wm.InstancesCalls - calls; got != 1 {
		t.Errorf("the slow refresh listed the windows %d times, want once: its survey is the look", got)
	}
	if surveys(m) {
		t.Error("the refresh's survey did not find the hide: the popup still surveys")
	}
}

// A look that fails changes nothing: the popup stays on the screen, and the
// next look asks again.
func TestAFailedLookChangesNothing(t *testing.T) {
	m, wm, _ := agentsPopup(t, true)
	m, _ = update(m, tea.BlurMsg{})
	wm.SetInstancesErr(errors.New("wctl: no answer"))
	for range 2 {
		var look tea.Cmd
		m, look = update(m, m.Looked())
		m = run(m, look)
	}
	wm.SetInstancesErr(nil)
	if !onAgents(m) {
		t.Errorf("query line = %q, want the popup on the agent list", query(m))
	}
	m, look := update(m, m.Looked())
	m = run(m, look)
	if _, cmd := update(m, m.Ticked()); cmd != nil {
		t.Error("the look after the failures did not find the hide")
	}
}

// Only a popup without the focus looks: one with the focus is on the screen,
// one that hid itself knows, and no other surface hides.
func TestOnlyABlurredPopupLooks(t *testing.T) {
	focused, _, _ := agentsPopup(t, true)
	if _, cmd := update(focused, focused.Looked()); cmd != nil {
		t.Error("a popup with the focus looked")
	}
	focused = survey(focused)
	if !surveys(focused) || !onAgents(focused) {
		t.Error("the refresh of a popup with the focus took it as hidden")
	}

	escaped, _, _ := agentsPopup(t, true)
	escaped, hide := press(escaped, "esc")
	escaped = run(escaped, hide)
	escaped, looking := update(escaped, tea.BlurMsg{})
	if looking != nil {
		t.Error("a popup that hid itself set a look at its blur")
	}
	if _, cmd := update(escaped, escaped.Looked()); cmd != nil {
		t.Error("a popup that hid itself looked")
	}

	_, wm, c, projects := world(t, 3)
	wm.AddInstance(revier.Instance{Title: "revier", Class: core.PopupClass, Minimized: true})
	own := refreshed(t, c, projects, stateWith(t, nil), nil)
	own, looking = update(own, tea.BlurMsg{})
	if looking != nil {
		t.Error("a surface in a terminal of its own set a look at its blur")
	}
	if _, cmd := update(own, own.Looked()); cmd != nil {
		t.Error("a surface in a terminal of its own looked")
	}
	own = survey(own)
	if !surveys(own) {
		t.Error("the slow refresh of a surface in a terminal of its own took it as hidden")
	}
}

// On GNOME the hide after Esc takes the focus before wctl answers, so the
// blur comes first: the looks it sets are dropped once the hide is in.
func TestTheLooksOfEscsOwnBlurAreDropped(t *testing.T) {
	m, _, _ := agentsPopup(t, true)
	m, hide := press(m, "esc")
	m, looking := update(m, tea.BlurMsg{})
	if looking == nil {
		t.Fatal("a blur before the hide answered set no look")
	}
	stale := m.Looked()
	m = run(m, hide)
	if _, cmd := update(m, stale); cmd != nil {
		t.Error("a look the blur before Esc's hide set still looked")
	}
}

// What a look or a survey found during an earlier blur is dropped in the
// next: the press between raised the popup.
func TestALookFromAnEarlierBlurIsDropped(t *testing.T) {
	m, _, _ := agentsPopup(t, true)
	m, _ = update(m, tea.BlurMsg{})
	_, look := update(m, m.Looked())
	found := look()
	_, tick := update(m, m.Ticked())
	surveyed := tick()
	m, _ = update(m, tea.FocusMsg{})
	m, _ = update(m, tea.BlurMsg{})

	m, _ = update(m, found)
	m, _ = update(m, surveyed)
	if !onAgents(m) || !surveys(m) {
		t.Errorf("query line = %q, want the popup still on the agent list and surveying", query(m))
	}
}

// A popup that asks a question, here a delete, is not taken as hidden: the
// raise after a hide would drop the question, and it waits for its answer.
func TestAPopupThatAsksIsNotTakenAsHidden(t *testing.T) {
	wm := hosttest.New("wm")
	wm.AddInstance(revier.Instance{Title: "revier", Class: core.PopupClass, Minimized: true})
	c := &core.Core{Runtime: hosttest.NewRuntime("rt"), Window: wm}
	projects, _ := onDisk(t, []string{"alpha", "beta"}, nil, nil)
	m, _ := clocked(tui.NewWithoutWaits(c, projects, stateWith(t, nil), &config.Config{}, time.Second, theme.Default(), "").
		WithPopup().StaticCursors())
	m = survey(m)
	m, _ = press(m, "alt+d")
	m, looking := update(m, tea.BlurMsg{})
	if looking != nil {
		t.Error("a popup that asks set a look at its blur")
	}
	m, tick := update(m, m.Ticked())
	m = run(m, tick)
	m, _ = update(m, tea.FocusMsg{})
	if f := footer(m); !strings.Contains(f, "delete alpha?") {
		t.Errorf("footer = %q, want the delete question still asked", f)
	}
}
