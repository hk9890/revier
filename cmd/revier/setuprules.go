package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/pkg/revier"
)

// printRuleReport is the popup rules' part of `revier setup status`: what
// revier's file holds, an older revier's rules in the user's file, and every
// rule that the host tries before revier's. unread is why the extension
// would not apply revier's file, empty when it would.
func printRuleReport(w io.Writer, r core.PopupRuleReport, unread string) {
	_, _ = fmt.Fprintf(w, "  %s %s\n", ruleMark(r.State), ruleStateNote(r))
	if r.Broken != "" {
		_, _ = fmt.Fprintf(w, "    %s\n", r.Broken)
	}
	printUserBroken(w, r)
	printLegacyRules(w, r.Legacy)
	printAheadRules(w, r.Ahead)
	if unread != "" {
		_, _ = fmt.Fprintf(w, "  ! not applied: %s\n", unread)
	}
}

func ruleMark(s core.PopupRuleState) string {
	if s == core.PopupRulesCurrent {
		return "+"
	}
	return "-"
}

func ruleStateNote(r core.PopupRuleReport) string {
	switch {
	case r.State == core.PopupRulesCurrent:
		return fmt.Sprintf("ok: %s holds revier's %d rules", r.File, len(r.Want))
	case len(r.Legacy) > 0:
		return "stale: an older revier's rules are in your rules file, where they hide revier's; `revier setup install` takes them out"
	case r.State == core.PopupRulesStale:
		return fmt.Sprintf("stale: %s does not hold the rules this revier writes; `revier setup install` rewrites it", r.File)
	}
	return "not installed: no " + r.File
}

// printUserBroken warns that the user's file does not load, so that none
// of its rules is read here, nor applied by the extension.
func printUserBroken(w io.Writer, r core.PopupRuleReport) {
	if r.UserBroken != "" {
		_, _ = fmt.Fprintf(w, "  ! your rules file does not load, so none of its rules applies: %s\n", r.UserBroken)
	}
}

// printLegacyRules lists an older revier's rules in the user's file, by
// their index there, as `wctl rules list` numbers them.
func printLegacyRules(w io.Writer, legacy []core.PopupRuleFound) {
	for _, f := range legacy {
		_, _ = fmt.Fprintf(w, "    rule %d of %s (revier's): %s\n", f.Index, f.File, describeRule(f.Rule))
	}
}

// printAheadRules warns of each rule for the popup that the host tries
// before revier's file: the first that matches the popup wins over
// revier's, which is how a user overrides them.
func printAheadRules(w io.Writer, ahead []core.PopupRuleFound) {
	for _, f := range ahead {
		_, _ = fmt.Fprintf(w, "  ! rule %d of %s comes first and wins over revier's: %s\n", f.Index, f.File, describeRule(f.Rule))
	}
	if len(ahead) > 0 {
		_, _ = fmt.Fprintf(w, "    Remove it to have revier's rules size %s.\n", core.PopupClass)
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
// failed.
func printRulePlan(w io.Writer, plan core.PopupRulePlan, sub string, dryRun bool) {
	r := plan.Report
	switch {
	case plan.Err != "":
		_, _ = fmt.Fprintf(w, "  ! failed: %s\n", plan.Err)
	case plan.Action == core.PopupRuleOK:
		_, _ = fmt.Fprintf(w, "  + ok: %s holds revier's %d rules\n", r.File, len(r.Want))
	case plan.Action == core.PopupRuleAbsent:
		_, _ = fmt.Fprintln(w, "  - not installed: revier has no rules file and no rule in yours")
	case plan.Action == core.PopupRuleRemove && len(plan.Moved) > 0:
		_, _ = fmt.Fprintf(w, "  > %s %s from your rules file\n", movedRules(plan.Moved), wouldBe("removed", dryRun))
	case plan.Action == core.PopupRuleRemove:
		_, _ = fmt.Fprintf(w, "  > %s %s\n", r.File, wouldBe("removed", dryRun))
	default:
		if len(plan.Moved) > 0 {
			_, _ = fmt.Fprintf(w, "  > %s %s from your rules file\n", movedRules(plan.Moved), wouldBe("removed", dryRun))
		}
		_, _ = fmt.Fprintf(w, "  > %s %s with revier's %d rules\n", r.File, wouldBe("written", dryRun), len(r.Want))
	}
	switch {
	case dryRun:
		printLegacyRules(w, plan.Moved)
	case plan.Err != "":
		printLegacyRules(w, r.Legacy)
	}
	if sub == "install" {
		printAheadRules(w, r.Ahead)
	}
	printRemovedRules(w, plan.Removed)
	if plan.Done && sub == "install" {
		_, _ = fmt.Fprintln(w, "\n  The extension reads the change by itself: the next popup gets it.")
	}
}

func movedRules(moved []core.PopupRuleFound) string {
	return fmt.Sprintf("%d %s of an older revier", len(moved), plural(len(moved), "rule", "rules"))
}

// printRemovedRules prints each rule the run took out of the user's file
// that revier does not write now, as the file held it: revier keeps no
// copy, and this is what the user puts back, with an editor or wctl.
func printRemovedRules(w io.Writer, removed []revier.WindowRule) {
	if len(removed) == 0 {
		return
	}
	_, _ = fmt.Fprintln(w, "\n  Removed from your rules file, as it held them. One you put back there wins over revier's:")
	for _, r := range removed {
		_, _ = fmt.Fprintf(w, "    %s\n", r.Raw)
	}
}
