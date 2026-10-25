package core_test

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/pkg/revier"
)

// The keys config.toml declares by default, and one named agent's.
var termKeys = []core.TermKey{
	{Chord: "ctrl+shift+t", Shell: true},
	{Chord: "ctrl+shift+z"},
	{Chord: "ctrl+shift+d", Agent: "dtop"},
}

// written is what revier's key file holds after an install of termKeys.
var written = []revier.TermBinding{
	{Chord: "ctrl+shift+t", Args: []string{"shell", "new"}},
	{Chord: "ctrl+shift+z", Args: []string{"agent", "new"}},
	{Chord: "ctrl+shift+d", Args: []string{"agent", "new", "dtop"}},
}

func termRow(t *testing.T, r core.TermReport, chord core.Chord) core.TermRow {
	t.Helper()
	for _, row := range r.Rows {
		if row.Chord == chord {
			return row
		}
	}
	t.Fatalf("no row for %q in %+v", chord, r.Rows)
	return core.TermRow{}
}

func termReportOf(t *testing.T, state revier.TermKeyState, keys []core.TermKey, projects ...core.Project) core.TermReport {
	t.Helper()
	c := &core.Core{TermKeys: hosttest.NewTermKeys(state)}
	r, err := c.TermKeyReport(context.Background(), keys, projects, "alt+space")
	if err != nil {
		t.Fatalf("TermKeyReport: %v", err)
	}
	return r
}

// Each key runs revier with the argv of what it opens; the default agent's
// names none.
func TestATermKeyRunsTheCommandOfWhatItOpens(t *testing.T) {
	r := termReportOf(t, revier.TermKeyState{}, termKeys)
	for i, want := range written {
		if got := r.Rows[i].Args; !slices.Equal(got, want.Args) {
			t.Errorf("row %d args = %v, want %v", i, got, want.Args)
		}
	}
	if r.FileState != core.TermFileAbsent || r.Include != core.TermIncludeAbsent {
		t.Errorf("file %q, include %q; want both absent", r.FileState, r.Include)
	}
	for _, row := range r.Rows {
		if row.Status != core.TermMissing {
			t.Errorf("%s: status %q, want missing", row.Chord, row.Status)
		}
	}
}

// A terminal key on a chord a desktop key of revier's holds would never be
// pressed: the desktop takes it first. That one key is refused, the others
// stay.
func TestATermKeyADesktopKeyHoldsIsRefusedAlone(t *testing.T) {
	keys := append(slices.Clone(termKeys), core.TermKey{Chord: "alt+space", Agent: "pop"}, core.TermKey{Chord: "ctrl+shift+u", Agent: "home"})
	r := termReportOf(t, revier.TermKeyState{}, keys, keyProject(t, "revier"))
	for chord, want := range map[core.Chord]string{
		"alt+space":    "the desktop key that opens revier ([ui] trigger_key)",
		"ctrl+shift+u": `the desktop key of target "home" in revier`,
	} {
		row := termRow(t, r, chord)
		if row.Status != core.TermRefused || row.Refused != want {
			t.Errorf("%s: %q %q, want refused by %q", chord, row.Status, row.Refused, want)
		}
	}
	if row := termRow(t, r, "ctrl+shift+t"); row.Status != core.TermMissing {
		t.Errorf("shell key: %q, want missing", row.Status)
	}
}

// A refused target's key is no desktop key revier wants, so it refuses no
// terminal key.
func TestARefusedTargetsKeyRefusesNoTermKey(t *testing.T) {
	p := keyProject(t, "revier")
	p.Refuse(0, errors.New("broken"))
	r := termReportOf(t, revier.TermKeyState{}, []core.TermKey{{Chord: "ctrl+shift+u", Shell: true}}, p)
	if row := r.Rows[0]; row.Status == core.TermRefused {
		t.Errorf("row refused by %q, want it kept: the target that holds the chord is refused", row.Refused)
	}
}

// A map of the user's after the include wins over revier's; a conditional
// one only where it holds. Before an install there is no include, and every
// map of the key comes after where install puts it.
func TestAMapAfterTheIncludeShadowsRevierAndOneBeforeMakesItLate(t *testing.T) {
	maps := []revier.TermMap{
		{Chord: "ctrl+shift+t", Line: 37},
		{Chord: "ctrl+shift+t", Line: 57, Condition: "when var:in_tmux"},
		{Chord: "ctrl+c", Line: 16},
	}
	r := termReportOf(t, revier.TermKeyState{ConfigExists: true, Maps: maps}, termKeys)
	want := []core.TermShadow{{Line: 37}, {Line: 57, Condition: "when var:in_tmux"}}
	if got := termRow(t, r, "ctrl+shift+t").Shadowed; !reflect.DeepEqual(got, want) {
		t.Errorf("shadowed = %+v, want %+v", got, want)
	}
	if got := termRow(t, r, "ctrl+shift+z").Shadowed; len(got) != 0 {
		t.Errorf("agent key shadowed by %+v, want nothing", got)
	}

	late := termReportOf(t, revier.TermKeyState{ConfigExists: true, Include: 40, Maps: maps}, termKeys)
	if late.Include != core.TermIncludeLate {
		t.Errorf("include = %q, want late: the map on line 37 is of a key revier writes", late.Include)
	}
	if got := termRow(t, late, "ctrl+shift+t").Shadowed; !reflect.DeepEqual(got, want[1:]) {
		t.Errorf("shadowed = %+v, want only the map after the include", got)
	}
}

// The file is current only when it holds exactly the keys config.toml wants.
func TestRevierFileIsStaleWhenItHoldsOtherKeys(t *testing.T) {
	state := revier.TermKeyState{FileExists: true, Bindings: written}
	if r := termReportOf(t, state, termKeys); r.FileState != core.TermFileCurrent {
		t.Errorf("file = %q, want current", r.FileState)
	}
	r := termReportOf(t, state, termKeys[:2])
	if r.FileState != core.TermFileStale {
		t.Errorf("file = %q, want stale: it holds dtop's key, which config.toml no longer declares", r.FileState)
	}
	for _, row := range r.Rows {
		if row.Status != core.TermWritten {
			t.Errorf("%s: %q, want written", row.Chord, row.Status)
		}
	}
}

// An install writes the keys the desktop does not take, adds the include,
// and reads the terminal again; a second install writes nothing.
func TestInstallWritesOnceAndASecondInstallWritesNothing(t *testing.T) {
	w := hosttest.NewTermWriter(revier.TermKeyState{
		ConfigExists: true,
		Maps:         []revier.TermMap{{Chord: "ctrl+shift+t", Line: 37}},
	})
	c := &core.Core{TermKeys: w}
	keys := append(slices.Clone(termKeys), core.TermKey{Chord: "alt+space", Agent: "pop"})
	plan, err := c.PlanInstallTermKeys(context.Background(), keys, nil, "alt+space")
	if err != nil {
		t.Fatal(err)
	}
	if plan.File != core.TermCreate || plan.Include != core.TermAdd {
		t.Errorf("plan: file %q, include %q; want create and add", plan.File, plan.Include)
	}
	plan = c.ApplyTermKeys(context.Background(), plan)
	if !plan.Done || plan.Err != "" {
		t.Fatalf("apply: done %v, err %q", plan.Done, plan.Err)
	}
	if !reflect.DeepEqual(w.State.Bindings, written) {
		t.Errorf("written %+v, want %+v: the refused key left out", w.State.Bindings, written)
	}
	if plan.Report.FileState != core.TermFileCurrent || plan.Report.Include != core.TermIncludeTop {
		t.Errorf("after: file %q, include %q; want current, top", plan.Report.FileState, plan.Report.Include)
	}
	if got := termRow(t, plan.Report, "ctrl+shift+t").Shadowed; len(got) != 1 || got[0].Line != 39 {
		t.Errorf("shadowed = %+v, want line 39, where the map is now", got)
	}

	again, err := c.PlanInstallTermKeys(context.Background(), keys, nil, "alt+space")
	if err != nil {
		t.Fatal(err)
	}
	if again.Writes() {
		t.Errorf("second plan writes: file %q, include %q", again.File, again.Include)
	}
	c.ApplyTermKeys(context.Background(), again)
	if len(w.Calls) != 1 {
		t.Errorf("calls = %v, want one write", w.Calls)
	}
}

// A missing main configuration is created, which the command has to say:
// the terminal watches only a file that was there when it started.
func TestInstallWithNoMainConfigurationCreatesIt(t *testing.T) {
	c := &core.Core{TermKeys: hosttest.NewTermWriter(revier.TermKeyState{})}
	plan, err := c.PlanInstallTermKeys(context.Background(), termKeys, nil, "alt+space")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Include != core.TermCreate {
		t.Errorf("include = %q, want create", plan.Include)
	}
}

// Uninstall removes what revier wrote; an include the user wrote stays.
func TestUninstallKeepsAUsersInclude(t *testing.T) {
	for _, tc := range []struct {
		owned bool
		want  core.TermAction
	}{{true, core.TermRemove}, {false, core.TermKept}} {
		w := hosttest.NewTermWriter(revier.TermKeyState{FileExists: true, Bindings: written, ConfigExists: true, Include: 3, IncludeOwned: tc.owned})
		c := &core.Core{TermKeys: w}
		plan, err := c.PlanUninstallTermKeys(context.Background(), termKeys, nil, "alt+space")
		if err != nil {
			t.Fatal(err)
		}
		if plan.File != core.TermRemove || plan.Include != tc.want {
			t.Errorf("owned %v: file %q, include %q; want remove, %q", tc.owned, plan.File, plan.Include, tc.want)
		}
		plan = c.ApplyTermKeys(context.Background(), plan)
		if !plan.Done || !slices.Equal(w.Calls, []string{"remove"}) {
			t.Errorf("owned %v: done %v, calls %v", tc.owned, plan.Done, w.Calls)
		}
	}
}

// Nothing of revier's there: uninstall writes nothing.
func TestUninstallOfNothingWritesNothing(t *testing.T) {
	w := hosttest.NewTermWriter(revier.TermKeyState{ConfigExists: true})
	c := &core.Core{TermKeys: w}
	plan, err := c.PlanUninstallTermKeys(context.Background(), termKeys, nil, "alt+space")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Writes() {
		t.Errorf("plan writes: file %q, include %q", plan.File, plan.Include)
	}
	c.ApplyTermKeys(context.Background(), plan)
	if len(w.Calls) != 0 {
		t.Errorf("calls = %v, want none", w.Calls)
	}
}

// A failed write is the plan's error, and neither step reads as done.
func TestAFailedWriteIsReported(t *testing.T) {
	w := hosttest.NewTermWriter(revier.TermKeyState{})
	w.WriteErr = errors.New("read-only file system")
	c := &core.Core{TermKeys: w}
	plan, _ := c.PlanInstallTermKeys(context.Background(), termKeys, nil, "alt+space")
	plan = c.ApplyTermKeys(context.Background(), plan)
	if plan.Done || plan.FileDone || plan.IncludeDone || plan.Err != "read-only file system" {
		t.Errorf("done %v, file done %v, include done %v, err %q", plan.Done, plan.FileDone, plan.IncludeDone, plan.Err)
	}
}

// A write that fails half way reads the terminal again, so the step that
// ran is done and the one that did not is not.
func TestAWriteThatFailsHalfWaySaysWhichStepRan(t *testing.T) {
	w := hosttest.NewTermWriter(revier.TermKeyState{ConfigExists: true})
	w.IncludeErr = errors.New("permission denied")
	c := &core.Core{TermKeys: w}
	plan, _ := c.PlanInstallTermKeys(context.Background(), termKeys, nil, "alt+space")
	plan = c.ApplyTermKeys(context.Background(), plan)
	if plan.Done || !plan.FileDone || plan.IncludeDone || plan.Err != "permission denied" {
		t.Errorf("done %v, file done %v, include done %v, err %q; want the key file alone done",
			plan.Done, plan.FileDone, plan.IncludeDone, plan.Err)
	}
}

// A link to nothing on the way to either file refuses the whole install and
// uninstall: the plan names it, and nothing is written, the other file
// included.
func TestALinkToNothingRefusesTheRunAndWritesNeitherFile(t *testing.T) {
	for _, tc := range []struct {
		state revier.TermKeyState
		want  string
	}{
		{revier.TermKeyState{ConfigDangling: "/k/kitty.conf"}, "/k/kitty.conf is a link to nothing"},
		{revier.TermKeyState{FileDangling: "/k/revier.conf", ConfigExists: true}, "/k/revier.conf is a link to nothing"},
		{revier.TermKeyState{FileDangling: "/k", ConfigDangling: "/k"}, "/k is a link to nothing"},
	} {
		state := tc.state
		w := hosttest.NewTermWriter(state)
		c := &core.Core{TermKeys: w}
		for _, planFor := range []func(context.Context, []core.TermKey, []core.Project, core.Chord) (core.TermPlan, error){
			c.PlanInstallTermKeys, c.PlanUninstallTermKeys,
		} {
			plan, err := planFor(context.Background(), termKeys, nil, "alt+space")
			if err != nil {
				t.Fatal(err)
			}
			if plan.Refused != tc.want {
				t.Errorf("%+v: refused %q, want %q", state, plan.Refused, tc.want)
			}
			if plan = c.ApplyTermKeys(context.Background(), plan); plan.Done || plan.Err != plan.Refused {
				t.Errorf("%+v: done %v, err %q; want the refusal", state, plan.Done, plan.Err)
			}
		}
		if len(w.Calls) != 0 {
			t.Errorf("%+v: calls = %v, want none", state, w.Calls)
		}
	}
}

// A reader alone cannot be written, and no terminal is a normal outcome.
func TestTermKeysNeedATerminalAndAWriter(t *testing.T) {
	c := &core.Core{TermKeys: hosttest.NewTermKeys(revier.TermKeyState{})}
	plan, _ := c.PlanInstallTermKeys(context.Background(), termKeys, nil, "alt+space")
	if plan = c.ApplyTermKeys(context.Background(), plan); plan.Err != core.ErrNoTermKeyWriter.Error() {
		t.Errorf("err = %q, want %q", plan.Err, core.ErrNoTermKeyWriter)
	}
	if _, err := (&core.Core{}).TermKeyReport(context.Background(), termKeys, nil, "alt+space"); !errors.Is(err, core.ErrNoTermKeys) {
		t.Errorf("err = %v, want ErrNoTermKeys", err)
	}
}
