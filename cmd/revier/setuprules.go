package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/pkg/revier"
)

// printRuleReport is the popup rules' part of `revier setup status`: what
// the rules hold for the popup's window, and every rule for it.
func printRuleReport(w io.Writer, r core.PopupRuleReport) {
	_, _ = fmt.Fprintf(w, "  %s %s\n", ruleMark(r.State), ruleStateNote(r))
	printFoundRules(w, r.Found)
}

func ruleMark(s core.PopupRuleState) string {
	switch s {
	case core.PopupRulesCurrent:
		return "+"
	case core.PopupRulesHeld:
		return "!"
	}
	return "-"
}

func ruleStateNote(r core.PopupRuleReport) string {
	switch r.State {
	case core.PopupRulesCurrent:
		return fmt.Sprintf("ok: revier's %d rules come first", len(r.Want))
	case core.PopupRulesStale:
		return "stale: revier's rules are not the ones this revier writes; `revier setup install` rewrites them"
	case core.PopupRulesHeld:
		return fmt.Sprintf("a rule of yours is for %s; `revier setup install --force` replaces it", core.PopupClass)
	}
	return "not installed: no rule is for " + core.PopupClass
}

// printFoundRules lists the rules for the popup's window, by their index in
// the file, as `wctl rules list` numbers them.
func printFoundRules(w io.Writer, found []core.PopupRuleFound) {
	for _, f := range found {
		whose := "revier's"
		if !f.Own {
			whose = "yours"
		}
		_, _ = fmt.Fprintf(w, "    rule %d (%s): %s\n", f.Index, whose, describeRule(f.Rule))
	}
}

// describeRule is a rule in the words of the guide: the monitor it is for,
// and where it puts the window.
func describeRule(r revier.WindowRule) string {
	var parts []string
	if r.MinWidth > 0 {
		parts = append(parts, fmt.Sprintf("monitor at least %d px wide", r.MinWidth))
	} else {
		parts = append(parts, "any monitor")
	}
	if len(r.Place) > 0 {
		parts = append(parts, "place "+strings.Join(r.Place, " "))
	}
	if r.Overlay {
		parts = append(parts, "overlay")
	}
	if r.Other {
		parts = append(parts, "and more")
	}
	return strings.Join(parts, ", ")
}

// printRulePlan is the popup rules' part of install and uninstall, marked as
// the keys are: + already right, > changed by this run, - left alone, !
// failed or held back.
func printRulePlan(w io.Writer, plan core.PopupRulePlan, sub string, dryRun, force bool) {
	n := len(plan.Report.Want)
	switch {
	case plan.Err != "":
		_, _ = fmt.Fprintf(w, "  ! failed: %s\n", plan.Err)
	case plan.Action == core.PopupRuleOK:
		_, _ = fmt.Fprintf(w, "  + ok: revier's %d rules come first\n", n)
	case plan.Action == core.PopupRuleAbsent:
		_, _ = fmt.Fprintln(w, "  - not installed: revier has no rule in the file")
	case plan.Action.Blocked() && !force:
		_, _ = fmt.Fprintf(w, "  ! skipped: a rule of yours is for %s. --force replaces it.\n", core.PopupClass)
	case plan.Action == core.PopupRuleRemove:
		_, _ = fmt.Fprintf(w, "  > revier's rules %s\n", wouldBe("removed", dryRun))
	case plan.Action == core.PopupRuleTakeOver:
		_, _ = fmt.Fprintf(w, "  > %d rules %s first, the rules of yours for %s removed\n", n, wouldBe("written", dryRun), core.PopupClass)
	default:
		_, _ = fmt.Fprintf(w, "  > %d rules %s first\n", n, wouldBe("written", dryRun))
	}
	if sub == "install" || dryRun || plan.Err != "" {
		printFoundRules(w, plan.Report.Found)
	}
	printRemovedRules(w, plan.Removed)
	if plan.Done && sub == "install" {
		_, _ = fmt.Fprintln(w, "\n  The extension reads the change by itself: the next popup gets it.")
	}
}

// printRemovedRules prints each rule the run removed that revier does not
// write, as the file held it: revier keeps no copy, and this is what the
// user puts back, with an editor or wctl.
func printRemovedRules(w io.Writer, removed []revier.WindowRule) {
	if len(removed) == 0 {
		return
	}
	_, _ = fmt.Fprintln(w, "\n  Removed, as the file held them, for you to put back:")
	for _, r := range removed {
		_, _ = fmt.Fprintf(w, "    %s\n", r.Raw)
	}
}
