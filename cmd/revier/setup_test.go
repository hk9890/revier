package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/pkg/revier"
)

// rulesApp is termApp with a fake rules file that holds rules, so `revier
// setup` runs all of its parts but the desktop keys.
func rulesApp(t *testing.T, rules ...revier.WindowRule) (*app, *hosttest.FakeWindowRuleWriter) {
	t.Helper()
	a, _ := termApp(t, "", "")
	w := hosttest.NewWindowRuleWriter(rules...)
	stores := a.stores
	a.stores = func(ctx context.Context, write bool) setupStores {
		s := stores(ctx, write)
		if write {
			s.rules = w
		} else {
			s.rules = w.FakeWindowRules
		}
		return s
	}
	return a, w
}

func runSetup(t *testing.T, a *app, args ...string) (string, error) {
	t.Helper()
	var b bytes.Buffer
	a.out = &b
	err := cmdSetup(t.Context(), a, args)
	return b.String(), err
}

var userPopupRule = revier.WindowRule{
	Class: core.PopupClass, Place: []string{"center", "top", "50%", "100%"},
	Raw: `{"match":{"class":"revier-popup"},"place":["center","top","50%","100%"]}`,
}

// A dry run says so once, and writes no rule.
func TestSetupDryRunWritesNoRule(t *testing.T) {
	a, w := rulesApp(t)
	out, err := runSetup(t, a, "install", "-n")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"dry run: nothing is changed", "desktop keys:", "popup rules:", hosttest.FakeOwnRules + " would be written with revier's 3 rules"} {
		if !strings.Contains(out, want) {
			t.Errorf("dry run does not say %q:\n%s", want, out)
		}
	}
	if len(w.Calls) != 0 {
		t.Errorf("a dry run wrote: %v", w.Calls)
	}
}

// A rule of the user's for the popup's window wins over revier's: install
// writes revier's file, leaves the user's rule, says it comes first, and is
// complete.
func TestSetupInstallLeavesARuleOfTheUsersAndSaysItWins(t *testing.T) {
	a, w := rulesApp(t, userPopupRule)
	out, err := runSetup(t, a, "install")
	if err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}
	if !strings.Contains(out, "! rule 0 of "+hosttest.FakeUserRules+" comes first and wins over revier's") {
		t.Errorf("install does not name the rule that wins:\n%s", out)
	}
	if !reflect.DeepEqual(w.User, []revier.WindowRule{userPopupRule}) || !reflect.DeepEqual(w.Own, core.PopupRules()) {
		t.Errorf("user = %+v, own = %+v", w.User, w.Own)
	}
	out, err = runSetup(t, a, "status")
	if err != nil || !strings.Contains(out, "+ ok: "+hosttest.FakeOwnRules) || !strings.Contains(out, "comes first and wins over revier's") {
		t.Errorf("status: err %v:\n%s", err, out)
	}
}

// An install takes the overlay rule the guide had users copy out of their
// file, where it would hide revier's, and prints it as the file held it. A
// rule of the user's in revier's shape with a size of their own stays, and
// status and install say it wins.
func TestSetupInstallTakesTheGuidesRuleOutAndLeavesTheUsers(t *testing.T) {
	guide := revier.WindowRule{Class: core.PopupClass, Overlay: true, Raw: `{"match":{"class":"revier-popup"},"overlay":true}`}
	hand := revier.WindowRule{
		Class: core.PopupClass, Place: []string{"center", "top", "1800", "100%"}, Overlay: true,
		Raw: `{"match":{"class":"revier-popup"},"place":["center","top",1800,"100%"],"overlay":true}`,
	}
	a, w := rulesApp(t, hand, guide)
	out, err := runSetup(t, a, "status")
	if err != nil || !strings.Contains(out, "stale: an older revier's rules are in your rules file") || !strings.Contains(out, "! rule 0 of "+hosttest.FakeUserRules+" comes first and wins over revier's") {
		t.Errorf("status before: err %v:\n%s", err, out)
	}
	out, err = runSetup(t, a, "install")
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if !strings.Contains(out, "1 rule of an older revier removed from your rules file") || !strings.Contains(out, "wins over revier's:\n    "+guide.Raw+"\n") {
		t.Errorf("install does not print the rule it removed:\n%s", out)
	}
	if !reflect.DeepEqual(w.User, []revier.WindowRule{hand}) || !reflect.DeepEqual(w.Own, core.PopupRules()) {
		t.Errorf("user = %+v, own = %+v", w.User, w.Own)
	}
}

// status --json carries each part, a part the machine does not have as null
// with the reason under skipped; uninstall then removes revier's file.
func TestSetupStatusThenUninstall(t *testing.T) {
	a, w := rulesApp(t)
	w.Own = core.PopupRules()
	stores := a.stores
	a.stores = func(ctx context.Context, write bool) setupStores {
		s := stores(ctx, write)
		s.keys = nil
		return s
	}
	out, err := runSetup(t, a, "status", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var report struct {
		Keys    *core.KeyReport
		Rules   *core.PopupRuleReport
		Skipped map[string]string
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("%v:\n%s", err, out)
	}
	if report.Keys != nil || report.Skipped["keys"] == "" {
		t.Errorf("keys = %+v, skipped %q; want null and a reason", report.Keys, report.Skipped["keys"])
	}
	if report.Rules == nil || report.Rules.State != core.PopupRulesCurrent {
		t.Errorf("rules = %+v, want current", report.Rules)
	}

	if _, err := runSetup(t, a, "uninstall"); err != nil {
		t.Fatal(err)
	}
	if w.Own != nil {
		t.Errorf("revier's file after uninstall = %+v", w.Own)
	}
}

// A machine whose window host revier cannot write rules for says why, and
// the rest of the setup still runs.
func TestSetupSaysWhyTheRulesAreSkipped(t *testing.T) {
	a, _ := termApp(t, "", "")
	stores := a.stores
	a.stores = func(ctx context.Context, write bool) setupStores {
		s := stores(ctx, write)
		s.rules = nil
		s.rulesWhy = errors.New("extension 21 does not read \"rules-min-width\" in a rule: upgrade it")
		return s
	}
	out, err := runSetup(t, a, "status")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "- skipped: extension 21 does not read") || !strings.Contains(out, "terminal: kitty") {
		t.Errorf("status:\n%s", out)
	}
}

// A part that cannot be read fails alone: status still reports the others,
// and the run's error names the part, so it is no exit for want of --force.
func TestSetupStatusReportsThePartsBesideOneThatFails(t *testing.T) {
	a, w := rulesApp(t)
	w.ReadErr = errors.New("rules.json is not valid JSON")
	out, err := runSetup(t, a, "status")
	if err == nil || errors.Is(err, errSetupIncomplete) || !strings.Contains(err.Error(), "rules: rules.json is not valid JSON") {
		t.Errorf("err = %v, want the rules part named", err)
	}
	if !strings.Contains(out, "terminal: kitty") || !strings.Contains(out, "! failed: rules.json is not valid JSON") {
		t.Errorf("status:\n%s", out)
	}
}

// An extension that would not apply revier's file keeps an install from
// writing it, and the install exits as incomplete, so a script hears that
// it set up less than the machine; status says why the file is not
// applied, and an uninstall still removes it.
func TestSetupUninstallNeedsNoExtensionThatReadsTheRules(t *testing.T) {
	a, w := rulesApp(t)
	stores := a.stores
	why := errors.New(`extension 23 does not read "rules-dropin", so it would not apply revier's rules: upgrade it`)
	a.stores = func(ctx context.Context, write bool) setupStores {
		s := stores(ctx, write)
		s.addWhy = why
		return s
	}
	out, err := runSetup(t, a, "install")
	if !errors.Is(err, errSetupIncomplete) || !strings.Contains(out, "- skipped: "+why.Error()) || len(w.Calls) != 0 {
		t.Errorf("install: err %v, calls %v:\n%s", err, w.Calls, out)
	}
	w.Own = core.PopupRules()
	if out, err := runSetup(t, a, "status"); err != nil || !strings.Contains(out, "! not applied: "+why.Error()) {
		t.Errorf("status: err %v:\n%s", err, out)
	}
	if _, err := runSetup(t, a, "uninstall"); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if w.Own != nil {
		t.Errorf("uninstall left %v", w.Own)
	}
}
