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

// An install on a file with rules of the user's puts revier's first and
// keeps every other rule, in its order.
func TestInstallPutsThePopupRulesFirstAndKeepsTheRest(t *testing.T) {
	w := hosttest.NewWindowRuleWriter(kittyRule)
	c := &core.Core{WindowRules: w}
	plan, err := c.PlanInstallPopupRules(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if plan.Action != core.PopupRuleWrite || plan.Report.State != core.PopupRulesAbsent {
		t.Fatalf("plan = %s on %s, want a write on absent", plan.Action, plan.Report.State)
	}
	plan = c.ApplyPopupRules(t.Context(), plan, false)
	if !plan.Done || plan.Err != "" {
		t.Fatalf("applied = done %v, err %q", plan.Done, plan.Err)
	}
	if want := append(core.PopupRules(), kittyRule); !reflect.DeepEqual(w.Rules, want) {
		t.Errorf("rules =\n%+v\nwant\n%+v", w.Rules, want)
	}
	if plan.Report.State != core.PopupRulesCurrent {
		t.Errorf("state after = %s, want current", plan.Report.State)
	}
}

// A second install finds the rules current and writes nothing.
func TestInstallOnCurrentRulesWritesNothing(t *testing.T) {
	w := hosttest.NewWindowRuleWriter(append(core.PopupRules(), kittyRule)...)
	c := &core.Core{WindowRules: w}
	plan, err := c.PlanInstallPopupRules(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if plan.Action != core.PopupRuleOK {
		t.Fatalf("action = %s, want ok", plan.Action)
	}
	c.ApplyPopupRules(t.Context(), plan, false)
	if len(w.Calls) != 0 {
		t.Errorf("calls = %v, want none", w.Calls)
	}
}

// Rules in revier's shape are revier's wherever they are: an older revier's
// sizes, and the overlay rule the guide had users write by hand. Install
// replaces them with no --force.
func TestInstallRewritesRevierRulesWithoutForce(t *testing.T) {
	w := hosttest.NewWindowRuleWriter(
		kittyRule,
		popupRule(0, "center", "top", "1800", "100%"),
		popupRule(0),
	)
	c := &core.Core{WindowRules: w}
	plan, err := c.PlanInstallPopupRules(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if plan.Action != core.PopupRuleWrite || plan.Report.State != core.PopupRulesStale {
		t.Fatalf("plan = %s on %s, want a write on stale", plan.Action, plan.Report.State)
	}
	plan = c.ApplyPopupRules(t.Context(), plan, false)
	if !plan.Done {
		t.Fatalf("not done: %q", plan.Err)
	}
	if want := append(core.PopupRules(), kittyRule); !reflect.DeepEqual(w.Rules, want) {
		t.Errorf("rules =\n%+v\nwant\n%+v", w.Rules, want)
	}
	// The adds first, at the top; then the removes from the last one up,
	// each three further down than the report found it.
	if want := []string{"add 0", "add 1", "add 2", "remove 5", "remove 4"}; !slices.Equal(w.Calls, want) {
		t.Errorf("calls = %v, want %v", w.Calls, want)
	}
}

// A rule for the popup's class in any other shape is the user's. Install
// without --force changes nothing; with it, the user's rule goes, and the
// plan carries it as the host stored it.
func TestARuleOfTheUsersForThePopupNeedsForce(t *testing.T) {
	mine := revier.WindowRule{Class: core.PopupClass, Place: []string{"center", "top", "50%", "100%"}, Raw: `{"mine":true}`}
	for _, force := range []bool{false, true} {
		w := hosttest.NewWindowRuleWriter(mine, kittyRule)
		c := &core.Core{WindowRules: w}
		plan, err := c.PlanInstallPopupRules(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if plan.Action != core.PopupRuleTakeOver || !plan.Action.Blocked() {
			t.Fatalf("action = %s, want a take-over that needs force", plan.Action)
		}
		plan = c.ApplyPopupRules(t.Context(), plan, force)
		want := []revier.WindowRule{mine, kittyRule}
		if force {
			want = append(core.PopupRules(), kittyRule)
		}
		if !reflect.DeepEqual(w.Rules, want) {
			t.Errorf("force %v: rules =\n%+v\nwant\n%+v", force, w.Rules, want)
		}
		if plan.Done != force {
			t.Errorf("force %v: done = %v", force, plan.Done)
		}
		var removed []revier.WindowRule
		if force {
			removed = []revier.WindowRule{mine}
		}
		if !reflect.DeepEqual(plan.Removed, removed) {
			t.Errorf("force %v: removed = %+v, want %+v", force, plan.Removed, removed)
		}
	}
}

// Uninstall removes revier's rules and leaves the user's, the user's rule for
// the popup's class among them.
func TestUninstallRemovesOnlyRevierRules(t *testing.T) {
	mine := revier.WindowRule{Class: core.PopupClass, Place: []string{"center", "top", "50%", "100%"}}
	w := hosttest.NewWindowRuleWriter(append(core.PopupRules(), kittyRule, mine)...)
	c := &core.Core{WindowRules: w}
	plan, err := c.PlanUninstallPopupRules(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if plan.Action != core.PopupRuleRemove {
		t.Fatalf("action = %s, want remove", plan.Action)
	}
	plan = c.ApplyPopupRules(t.Context(), plan, false)
	if !plan.Done {
		t.Fatalf("not done: %q", plan.Err)
	}
	if want := []revier.WindowRule{kittyRule, mine}; !reflect.DeepEqual(w.Rules, want) {
		t.Errorf("rules =\n%+v\nwant\n%+v", w.Rules, want)
	}

	plan, err = c.PlanUninstallPopupRules(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if plan.Action != core.PopupRuleAbsent {
		t.Errorf("second uninstall = %s, want absent", plan.Action)
	}
}

// A run that stops half way says so, and its report is the file as it was
// left.
func TestARunThatFailsHalfWayReportsTheFileAsLeft(t *testing.T) {
	w := hosttest.NewWindowRuleWriter(kittyRule)
	w.FailAt = 2
	c := &core.Core{WindowRules: w}
	plan, err := c.PlanInstallPopupRules(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	plan = c.ApplyPopupRules(t.Context(), plan, false)
	if plan.Done || plan.Err == "" {
		t.Fatalf("done %v, err %q; want a failure", plan.Done, plan.Err)
	}
	if plan.Report.State != core.PopupRulesStale {
		t.Errorf("state = %s, want stale: one rule of three was written", plan.Report.State)
	}
}

// The adds go before the removes, so a run that stops after them leaves
// revier's rules first, where they place the popup, and the old ones below
// them, where the host never reaches them.
func TestARunThatFailsAfterTheAddsLeavesRulesThatPlaceThePopup(t *testing.T) {
	w := hosttest.NewWindowRuleWriter(kittyRule, popupRule(0, "center", "top", "1800", "100%"))
	w.FailAt = 4
	c := &core.Core{WindowRules: w}
	plan, err := c.PlanInstallPopupRules(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	plan = c.ApplyPopupRules(t.Context(), plan, false)
	if plan.Done || plan.Err == "" {
		t.Fatalf("done %v, err %q; want a failure", plan.Done, plan.Err)
	}
	if want := core.PopupRules(); !reflect.DeepEqual(w.Rules[:len(want)], want) {
		t.Errorf("rules =\n%+v\nwant revier's first", w.Rules)
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
