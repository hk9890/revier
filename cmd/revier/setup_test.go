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
	a.stores = func(ctx context.Context, write bool) setupStores {
		s := termOnly(ctx, write)
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
	for _, want := range []string{"dry run: nothing is changed", "desktop keys:", "- skipped: " + core.ErrNoKeyBinder.Error(), "popup rules:", "3 rules would be written first"} {
		if !strings.Contains(out, want) {
			t.Errorf("dry run does not say %q:\n%s", want, out)
		}
	}
	if len(w.Calls) != 0 {
		t.Errorf("a dry run wrote: %v", w.Calls)
	}
}

// A rule of the user's for the popup's window holds the install back and
// makes it incomplete; --force replaces it and prints it as the file held
// it, for the user to put back.
func TestSetupInstallNeedsForceForARuleOfTheUsers(t *testing.T) {
	a, w := rulesApp(t, userPopupRule)
	out, err := runSetup(t, a, "install")
	if !errors.Is(err, errSetupIncomplete) {
		t.Errorf("err = %v, want errSetupIncomplete", err)
	}
	if !strings.Contains(out, "--force replaces it") || !strings.Contains(out, "rule 0 (yours)") {
		t.Errorf("install does not name the rule and the way past it:\n%s", out)
	}
	if len(w.Calls) != 0 {
		t.Fatalf("install without --force wrote: %v", w.Calls)
	}

	out, err = runSetup(t, a, "install", "--force")
	if err != nil {
		t.Fatalf("install --force: %v", err)
	}
	if !strings.Contains(out, "for you to put back:\n    "+userPopupRule.Raw+"\n") {
		t.Errorf("install --force does not print the rule it removed:\n%s", out)
	}
	if !reflect.DeepEqual(w.Rules, core.PopupRules()) {
		t.Errorf("rules = %+v, want revier's", w.Rules)
	}
}

// status --json carries each part, a part the machine does not have as null
// with the reason under skipped; uninstall then removes revier's rules.
func TestSetupStatusThenUninstall(t *testing.T) {
	a, w := rulesApp(t, core.PopupRules()...)
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
	if len(w.Rules) != 0 {
		t.Errorf("rules after uninstall = %+v", w.Rules)
	}
}

// A machine whose window host revier cannot write rules for says why, and
// the rest of the setup still runs.
func TestSetupSaysWhyTheRulesAreSkipped(t *testing.T) {
	a, _ := termApp(t, "", "")
	a.stores = func(ctx context.Context, write bool) setupStores {
		s := termOnly(ctx, write)
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

// An extension that cannot read revier's rules keeps an install from adding
// them, and an uninstall still removes the ones revier wrote.
func TestSetupUninstallNeedsNoExtensionThatReadsTheRules(t *testing.T) {
	a, w := rulesApp(t)
	stores := a.stores
	a.stores = func(ctx context.Context, write bool) setupStores {
		s := stores(ctx, write)
		s.addWhy = errors.New("extension 21 does not read \"rules-min-width\" in a rule: upgrade it")
		return s
	}
	out, err := runSetup(t, a, "install")
	if err != nil || !strings.Contains(out, "- skipped: extension 21 does not read") || len(w.Calls) != 0 {
		t.Errorf("install: err %v, calls %v:\n%s", err, w.Calls, out)
	}
	w.Rules = core.PopupRules()
	if _, err := runSetup(t, a, "uninstall"); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if len(w.Rules) != 0 {
		t.Errorf("uninstall left %v", w.Rules)
	}
}
