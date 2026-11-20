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

// guideRule is the overlay rule the guide of revier 0.27 had users copy
// into their own file.
var guideRule = revier.WindowRule{
	Class: core.PopupClass, Overlay: true,
	Raw: `{"match":{"class":"revier-popup"},"overlay":true}`,
}

// handRule is a rule of the user's in revier's shape with a size of the
// user's own: an override, or a rule written by hand from an older guide.
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

// An install takes an older revier's rules out of the user's file, known by
// their exact shape: a tier revier 0.28 wrote, and the guide's overlay
// rule. The host would try them before revier's file and so hide it. It
// writes revier's file first, keeps every other rule, and carries each it
// took that revier does not write now, for the user.
func TestInstallTakesAnOlderReviersRulesOutOfTheUsersFile(t *testing.T) {
	w := hosttest.NewWindowRuleWriter(guideRule, kittyRule, core.PopupRules()[2])
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
	if want := []revier.WindowRule{guideRule}; !reflect.DeepEqual(plan.Removed, want) {
		t.Errorf("removed =\n%+v\nwant\n%+v", plan.Removed, want)
	}
	// The file first, then the removes from the last one up.
	if want := []string{"write own", "remove user 2", "remove user 0"}; !slices.Equal(w.Calls, want) {
		t.Errorf("calls = %v, want %v", w.Calls, want)
	}
}

// An older revier's rule is one wherever it is, also after revier wrote its
// file: another machine that shares the user's file can put it back. The
// install then takes it out and leaves revier's current file alone.
func TestAnOlderReviersRuleIsTakenOutBesideACurrentFile(t *testing.T) {
	w := hosttest.NewWindowRuleWriter(core.PopupRules()...)
	w.Own = core.PopupRules()
	plan := install(t, &core.Core{WindowRules: w})
	if !plan.Done || len(w.User) != 0 || len(plan.Removed) != 0 {
		t.Errorf("done %v, user %+v, removed %+v", plan.Done, w.User, plan.Removed)
	}
	if want := []string{"remove user 2", "remove user 1", "remove user 0"}; !slices.Equal(w.Calls, want) {
		t.Errorf("calls = %v, want %v", w.Calls, want)
	}
}

// A rule for the popup in any other shape is the user's, also one in
// revier's shape with a size of the user's own: it wins over revier's, so
// install leaves it, with revier's file there or not, and the report names
// it.
func TestARuleOfTheUsersWinsAndStays(t *testing.T) {
	mine := revier.WindowRule{Class: core.PopupClass, Place: []string{"center", "top", "50%", "100%"}, Other: true}
	other := revier.WindowRuleFile{Path: "rules.d/a.json", Rules: []revier.WindowRule{kittyRule, popupRule(0)}}
	for _, own := range [][]revier.WindowRule{nil, core.PopupRules()} {
		w := hosttest.NewWindowRuleWriter(handRule, mine)
		w.Own, w.Earlier = own, []revier.WindowRuleFile{other}
		plan := install(t, &core.Core{WindowRules: w})
		if (plan.Action != core.PopupRuleOK && !plan.Done) || !reflect.DeepEqual(w.User, []revier.WindowRule{handRule, mine}) {
			t.Fatalf("own %v: done %v, err %q, user %+v", own != nil, plan.Done, plan.Err, w.User)
		}
		want := []core.PopupRuleFound{
			{File: hosttest.FakeUserRules, Index: 0, Rule: handRule},
			{File: hosttest.FakeUserRules, Index: 1, Rule: mine},
			{File: "rules.d/a.json", Index: 1, Rule: popupRule(0)},
		}
		if !reflect.DeepEqual(plan.Report.Ahead, want) || len(plan.Report.Legacy) != 0 {
			t.Errorf("own %v: ahead =\n%+v\nwant\n%+v\nlegacy %+v", own != nil, plan.Report.Ahead, want, plan.Report.Legacy)
		}
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

// A rule of the user's that places the popup on any monitor decides, also
// where revier's file is not current: the host applies it first. One for
// wide monitors alone leaves the rest to the rules after it.
func TestARuleOfTheUsersOnAnyMonitorPlacesThePopupWhateverReviersFileHolds(t *testing.T) {
	mine := revier.WindowRule{Class: core.PopupClass, Place: []string{"center", "top", "50%", "100%"}}
	wide := revier.WindowRule{Class: core.PopupClass, MinWidth: 3000, Place: []string{"center", "top", "50%", "100%"}}
	for _, tc := range []struct {
		name   string
		user   []revier.WindowRule
		places bool
	}{
		{"any monitor", []revier.WindowRule{mine}, true},
		{"wide monitors alone", []revier.WindowRule{wide}, false},
		{"wide, then any", []revier.WindowRule{wide, mine}, true},
	} {
		report, err := (&core.Core{WindowRules: hosttest.NewWindowRules(tc.user...)}).PopupRuleReport(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if report.State != core.PopupRulesMissing || report.PlacesPopup() != tc.places {
			t.Errorf("%s: state %s, places = %v, want missing and %v", tc.name, report.State, report.PlacesPopup(), tc.places)
		}
	}
}

// A user's file the host would not load does not stop revier's file, which
// the host loads without it. An older revier's rules in it wait for the
// install after it is fixed.
func TestABrokenUsersFileDoesNotStopReviersFile(t *testing.T) {
	w := hosttest.NewWindowRuleWriter(guideRule)
	w.UserBroken = "rules.json: not valid JSON"
	c := &core.Core{WindowRules: w}
	plan := install(t, c)
	if !plan.Done || !reflect.DeepEqual(w.Own, core.PopupRules()) || plan.Report.UserBroken == "" || len(plan.Moved) != 0 {
		t.Errorf("install: done %v, err %q, own %+v, moved %+v", plan.Done, plan.Err, w.Own, plan.Moved)
	}
	if !plan.Report.PlacesPopup() {
		t.Error("a current file of revier's does not place the popup beside a broken user's file")
	}
	w.UserBroken = ""
	if plan = install(t, c); !plan.Done || len(w.User) != 0 {
		t.Errorf("install after the fix: done %v, user %+v", plan.Done, w.User)
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

// A run that stops after it wrote revier's file leaves the older revier's
// rules where they were, and the next install takes them out.
func TestARunThatStopsAfterTheFileIsFinishedByTheNext(t *testing.T) {
	w := hosttest.NewWindowRuleWriter(guideRule)
	w.FailAt = 2
	c := &core.Core{WindowRules: w}
	plan := install(t, c)
	if plan.Done || plan.Err == "" || plan.Report.State != core.PopupRulesStale || len(plan.Report.Legacy) != 1 {
		t.Fatalf("done %v, err %q, state %s, legacy %d; want a failure that left the old rule", plan.Done, plan.Err, plan.Report.State, len(plan.Report.Legacy))
	}
	w.FailAt = 0
	if plan = install(t, c); !plan.Done || len(w.User) != 0 || !reflect.DeepEqual(w.Own, core.PopupRules()) {
		t.Errorf("second install: done %v, user %+v, own %+v", plan.Done, w.User, w.Own)
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
