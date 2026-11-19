package core_test

import (
	"reflect"
	"slices"
	"testing"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/pkg/revier"
)

// popupRule is a rule for the popup's class in the shape revier writes.
func popupRule(minWidth int, place ...string) revier.WindowRule {
	return revier.WindowRule{Class: core.PopupClass, MinWidth: minWidth, Place: place, Overlay: true}
}

var kittyRule = revier.WindowRule{Class: "kitty", Place: []string{"center", "top", "50%", "100%"}}

// handRule is the overlay rule the guide had users write into their own
// file by hand, with no width.
var handRule = revier.WindowRule{
	Class: core.PopupClass, Place: []string{"center", "top", "1800", "100%"}, Overlay: true,
	Raw: `{"match":{"class":"revier-popup"},"place":["center","top",1800,"100%"],"overlay":true}`,
}

// The rules are the popup's tiers, widest first, each kept on top, and the
// last one for any screen, so a popup always matches one.
func TestPopupRulesAreTheTiersWidestFirst(t *testing.T) {
	want := []revier.WindowRule{
		popupRule(3400, "center", "top", "1800", "100%"),
		popupRule(2200, "center", "top", "75%", "100%"),
		popupRule(0, "left", "top", "100%", "100%"),
	}
	if got := core.PopupRules(); !reflect.DeepEqual(got, want) {
		t.Errorf("PopupRules =\n%+v\nwant\n%+v", got, want)
	}
}

func install(t *testing.T, c *core.Core) core.PopupRulePlan {
	t.Helper()
	plan, err := c.PlanInstallPopupRules(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return c.ApplyPopupRules(t.Context(), plan)
}

// An install writes revier's own file and leaves the user's file as it was.
func TestInstallWritesRevierFileAndLeavesTheUsers(t *testing.T) {
	w := hosttest.NewWindowRuleWriter(kittyRule)
	c := &core.Core{WindowRules: w}
	plan, err := c.PlanInstallPopupRules(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if plan.Action != core.PopupRuleWrite || plan.Report.State != core.PopupRulesMissing {
		t.Fatalf("plan = %s on %s, want a write on missing", plan.Action, plan.Report.State)
	}
	plan = c.ApplyPopupRules(t.Context(), plan)
	if !plan.Done || plan.Err != "" {
		t.Fatalf("applied = done %v, err %q", plan.Done, plan.Err)
	}
	if !reflect.DeepEqual(w.Own, core.PopupRules()) || !reflect.DeepEqual(w.User, []revier.WindowRule{kittyRule}) {
		t.Errorf("own =\n%+v\nuser =\n%+v", w.Own, w.User)
	}
	if want := []string{"write own"}; !slices.Equal(w.Calls, want) {
		t.Errorf("calls = %v, want %v", w.Calls, want)
	}
	if plan.Report.State != core.PopupRulesCurrent || plan.Report.File != hosttest.FakeOwnRules {
		t.Errorf("after = %s of %s, want current of revier's file", plan.Report.State, plan.Report.File)
	}
}

// A second install finds revier's file current and writes nothing.
func TestInstallOnACurrentFileWritesNothing(t *testing.T) {
	w := hosttest.NewWindowRuleWriter(kittyRule)
	w.Own = core.PopupRules()
	plan := install(t, &core.Core{WindowRules: w})
	if plan.Action != core.PopupRuleOK || len(w.Calls) != 0 {
		t.Errorf("action = %s, calls = %v; want ok and none", plan.Action, w.Calls)
	}
}

// A file of revier's with other rules, or one the host would not load, is
// stale, and install writes it again.
func TestInstallRewritesAStaleFile(t *testing.T) {
	for name, broken := range map[string]string{"other sizes": "", "refused": "rules[0].bogus: unknown key"} {
		w := hosttest.NewWindowRuleWriter()
		w.Own, w.OwnBroken = core.PopupRules()[1:], broken
		c := &core.Core{WindowRules: w}
		plan, err := c.PlanInstallPopupRules(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if plan.Report.State != core.PopupRulesStale || plan.Report.Broken != broken {
			t.Errorf("%s: state = %s, broken %q", name, plan.Report.State, plan.Report.Broken)
		}
		if plan = c.ApplyPopupRules(t.Context(), plan); !plan.Done || !reflect.DeepEqual(w.Own, core.PopupRules()) {
			t.Errorf("%s: done %v, own = %+v", name, plan.Done, w.Own)
		}
	}
}

// The first install takes an older revier's rules out of the user's file:
// its sizes, and the hand-written overlay rule, which the host would try
// before revier's file and so hide it. It keeps every other rule, and
// carries each it took that revier does not write now, for the user.
func TestTheFirstInstallMovesAnOlderReviersRulesOutOfTheUsersFile(t *testing.T) {
	w := hosttest.NewWindowRuleWriter(handRule, kittyRule, core.PopupRules()[2])
	c := &core.Core{WindowRules: w}
	plan, err := c.PlanInstallPopupRules(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if plan.Action != core.PopupRuleWrite || plan.Report.State != core.PopupRulesStale || len(plan.Report.Legacy) != 2 || len(plan.Report.Ahead) != 0 {
		t.Fatalf("plan = %s on %s, legacy %d, ahead %d; want a write on stale with 2 legacy rules", plan.Action, plan.Report.State, len(plan.Report.Legacy), len(plan.Report.Ahead))
	}
	plan = c.ApplyPopupRules(t.Context(), plan)
	if !plan.Done {
		t.Fatalf("not done: %q", plan.Err)
	}
	if !reflect.DeepEqual(w.User, []revier.WindowRule{kittyRule}) || !reflect.DeepEqual(w.Own, core.PopupRules()) {
		t.Errorf("user =\n%+v\nown =\n%+v", w.User, w.Own)
	}
	if want := []revier.WindowRule{handRule}; !reflect.DeepEqual(plan.Removed, want) {
		t.Errorf("removed =\n%+v\nwant\n%+v", plan.Removed, want)
	}
	// The removes from the last one up, then the file.
	if want := []string{"remove user 2", "remove user 0", "write own"}; !slices.Equal(w.Calls, want) {
		t.Errorf("calls = %v, want %v", w.Calls, want)
	}
}

// Once revier's file exists, a rule for the popup in the user's file is the
// user's, also in revier's shape: it wins over revier's, so install leaves
// it and the report names it.
func TestOnceRevierHasAFileARuleOfTheUsersWinsAndStays(t *testing.T) {
	mine := revier.WindowRule{Class: core.PopupClass, Place: []string{"center", "top", "50%", "100%"}, Other: true}
	other := revier.WindowRuleFile{Path: "rules.d/a.json", Rules: []revier.WindowRule{kittyRule, popupRule(0)}}
	w := hosttest.NewWindowRuleWriter(handRule, mine)
	w.Own, w.Earlier = core.PopupRules(), []revier.WindowRuleFile{other}
	plan := install(t, &core.Core{WindowRules: w})
	if plan.Action != core.PopupRuleOK || len(w.Calls) != 0 {
		t.Fatalf("action = %s, calls %v; want ok and none", plan.Action, w.Calls)
	}
	want := []core.PopupRuleFound{
		{File: hosttest.FakeUserRules, Index: 0, Rule: handRule},
		{File: hosttest.FakeUserRules, Index: 1, Rule: mine},
		{File: "rules.d/a.json", Index: 1, Rule: popupRule(0)},
	}
	if !reflect.DeepEqual(plan.Report.Ahead, want) || len(plan.Report.Legacy) != 0 {
		t.Errorf("ahead =\n%+v\nwant\n%+v\nlegacy %+v", plan.Report.Ahead, want, plan.Report.Legacy)
	}
}

// Rules ahead of revier's file place the popup when each places it, or
// holds what revier cannot read; a rule that places nothing leaves the
// place to revier.
func TestRulesAheadPlaceThePopupWhenEachPlacesIt(t *testing.T) {
	for _, tc := range []struct {
		name   string
		ahead  revier.WindowRule
		places bool
	}{
		{"a place", handRule, true},
		{"a tile", revier.WindowRule{Class: core.PopupClass, Other: true}, true},
		{"an overlay alone", popupRule(0), false},
	} {
		w := hosttest.NewWindowRules(tc.ahead)
		w.Own = core.PopupRules()
		report, err := (&core.Core{WindowRules: w}).PopupRuleReport(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if report.PlacesPopup() != tc.places {
			t.Errorf("%s: places = %v, want %v", tc.name, report.PlacesPopup(), tc.places)
		}
	}
}

// Uninstall removes revier's file and leaves the user's.
func TestUninstallRemovesRevierFile(t *testing.T) {
	mine := revier.WindowRule{Class: core.PopupClass, Place: []string{"center", "top", "50%", "100%"}, Other: true}
	w := hosttest.NewWindowRuleWriter(kittyRule, mine)
	w.Own = core.PopupRules()
	c := &core.Core{WindowRules: w}
	plan, err := c.PlanUninstallPopupRules(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if plan.Action != core.PopupRuleRemove {
		t.Fatalf("action = %s, want remove", plan.Action)
	}
	if plan = c.ApplyPopupRules(t.Context(), plan); !plan.Done {
		t.Fatalf("not done: %q", plan.Err)
	}
	if w.Own != nil || !reflect.DeepEqual(w.User, []revier.WindowRule{kittyRule, mine}) {
		t.Errorf("own = %+v, user = %+v", w.Own, w.User)
	}
	plan, err = c.PlanUninstallPopupRules(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if plan.Action != core.PopupRuleAbsent {
		t.Errorf("second uninstall = %s, want absent", plan.Action)
	}
}

// Before revier has a file, uninstall takes an older revier's rules out of
// the user's file, as that revier's uninstall did.
func TestUninstallBeforeAnInstallRemovesAnOlderReviersRules(t *testing.T) {
	w := hosttest.NewWindowRuleWriter(append([]revier.WindowRule{kittyRule}, core.PopupRules()...)...)
	c := &core.Core{WindowRules: w}
	plan, err := c.PlanUninstallPopupRules(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if plan = c.ApplyPopupRules(t.Context(), plan); !plan.Done {
		t.Fatalf("not done: %q", plan.Err)
	}
	if !reflect.DeepEqual(w.User, []revier.WindowRule{kittyRule}) || len(plan.Removed) != 0 {
		t.Errorf("user = %+v, removed %+v", w.User, plan.Removed)
	}
}

// A run that stops after it took the old rules out leaves no file of
// revier's, and the next install writes it: the old rules are gone, so
// nothing is left to hide it.
func TestARunThatStopsAfterTheMoveIsFinishedByTheNext(t *testing.T) {
	w := hosttest.NewWindowRuleWriter(handRule)
	w.FailAt = 2
	c := &core.Core{WindowRules: w}
	plan := install(t, c)
	if plan.Done || plan.Err == "" || plan.Report.State != core.PopupRulesMissing {
		t.Fatalf("done %v, err %q, state %s; want a failure that left no file", plan.Done, plan.Err, plan.Report.State)
	}
	if want := []revier.WindowRule{handRule}; !reflect.DeepEqual(plan.Removed, want) {
		t.Errorf("removed = %+v, want the rule the failed run took out", plan.Removed)
	}
	w.FailAt = 0
	if plan = install(t, c); !plan.Done || !reflect.DeepEqual(w.Own, core.PopupRules()) {
		t.Errorf("second install: done %v, own %+v", plan.Done, w.Own)
	}
}

// A machine with no window host whose rules revier writes is a normal
// outcome.
func TestNoWindowRulesIsTheSentinel(t *testing.T) {
	_, err := (&core.Core{}).PopupRuleReport(t.Context())
	if err != core.ErrNoWindowRules {
		t.Errorf("err = %v, want ErrNoWindowRules", err)
	}
}
