package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"

	"github.com/hk9890/revier/internal/core"
)

// apply is install and uninstall. They differ in the plans they build and in
// nothing else, so they share the run: build each part's plan, apply it
// unless this is a dry run, print what happened.
func (r setupRun) apply(ctx context.Context, asJSON bool) error {
	a := r.a
	out := setupPlan{Skipped: map[string]string{}, Failed: map[string]string{}}
	parts := partResults{skipped: out.Skipped, failed: out.Failed}

	keys, err := r.applyKeys(ctx)
	if parts.ok("keys", err, core.ErrNoKeyBinder, "") {
		out.Keys = &keys
	}
	termBefore, term, err := r.applyTerm(ctx)
	if parts.ok("term", err, core.ErrNoTermKeys, "") {
		out.Term = &term
	}
	rules, err := r.applyRules(ctx)
	if parts.ok("rules", err, core.ErrNoWindowRules, r.skipped(err)) {
		out.Rules = &rules
	}

	if asJSON {
		if err := a.printJSON(out); err != nil {
			return err
		}
	} else {
		r.print(a.out, out, termBefore, parts)
	}

	if err := setupFailed(r.sub, out.Failed); err != nil {
		return err
	}
	// An install that skipped a part did not set up the machine: a script
	// that installs and goes on must hear it, as it hears a held key.
	if (r.sub == "install" && len(out.Skipped) > 0) ||
		(out.Keys != nil && keysIncomplete(*out.Keys, r.force)) ||
		(out.Term != nil && termIncomplete(*out.Term, r.sub)) ||
		(out.Rules != nil && out.Rules.Err != "") {
		return errSetupIncomplete
	}
	return nil
}

func (r setupRun) print(w io.Writer, out setupPlan, termBefore core.TermKeyPlan, parts partResults) {
	_, _ = fmt.Fprintf(w, "=== revier setup %s ===\n\n", r.sub)
	if r.dryRun {
		_, _ = fmt.Fprint(w, "  dry run: nothing is changed\n\n")
	}
	_, _ = fmt.Fprintln(w, "desktop keys:")
	if out.Keys != nil {
		printPlan(w, *out.Keys, r.sub, r.dryRun, r.force)
	} else {
		parts.print(w, "keys")
	}
	_, _ = fmt.Fprintln(w, "\nterminal keys:")
	if out.Term != nil {
		printTermPlan(w, termBefore, *out.Term, r.sub, r.dryRun)
	} else {
		parts.print(w, "term")
	}
	_, _ = fmt.Fprintln(w, "\npopup rules:")
	if out.Rules != nil {
		printRulePlan(w, *out.Rules, r.sub, r.dryRun)
	} else {
		parts.print(w, "rules")
	}
}

// partResults records, for every run, the parts that did not come out: a
// part is skipped when the machine does not have it, which is its sentinel,
// and failed on any other error.
type partResults struct{ skipped, failed map[string]string }

// ok records err for part and reports whether there was none. why, when
// set, says why the part is skipped in place of the sentinel.
func (p partResults) ok(part string, err, sentinel error, why string) bool {
	switch {
	case err == nil:
		return true
	case !errors.Is(err, sentinel):
		p.failed[part] = err.Error()
	case why != "":
		p.skipped[part] = why
	default:
		p.skipped[part] = err.Error()
	}
	return false
}

func (p partResults) print(w io.Writer, part string) {
	if why, ok := p.skipped[part]; ok {
		_, _ = fmt.Fprintf(w, "  - skipped: %s\n", why)
		return
	}
	_, _ = fmt.Fprintf(w, "  ! failed: %s\n", p.failed[part])
}

// applyKeys builds the desktop keys' plan and, unless this is a dry run,
// carries it out.
func (r setupRun) applyKeys(ctx context.Context) (core.KeyPlan, error) {
	a := r.a
	planFor := a.core.PlanInstallKeys
	if r.sub == "uninstall" {
		planFor = a.core.PlanUninstallKeys
	}
	plan, err := planFor(ctx, a.projects, r.trigger)
	if err != nil || r.dryRun {
		return plan, err
	}
	// Each step that changed something logs "setup install: keys" or
	// "setup uninstall: keys" with step. A step that failed logs it at
	// Error; its error is inside step.
	plan = a.core.ApplyKeys(ctx, plan, r.force)
	for _, s := range plan.Steps {
		if s.Err != "" {
			slog.Error("setup "+r.sub+": keys", "step", s)
		} else if s.Done {
			slog.Info("setup "+r.sub+": keys", "step", s)
		}
	}
	return plan, nil
}

// applyTerm is applyKeys for the terminal's keys. It returns the plan as it
// was built beside what became of it: the rows of the first say which keys
// the run had to write.
func (r setupRun) applyTerm(ctx context.Context) (before, after core.TermKeyPlan, err error) {
	a := r.a
	planFor := a.core.PlanInstallTermKeys
	if r.sub == "uninstall" {
		planFor = a.core.PlanUninstallTermKeys
	}
	plan, err := planFor(ctx, a.cfg.TermKeys(), a.projects, r.trigger)
	if err != nil || r.dryRun {
		return plan, plan, err
	}
	// A run that changed something logs "setup install: term" or
	// "setup uninstall: term" with file and include, the actions on revier's
	// key file and on the include line, and path, the key file. A run that
	// failed logs it at Error with err in place of path.
	after = a.core.ApplyTermKeys(ctx, plan)
	switch {
	case after.Err != "":
		slog.Error("setup "+r.sub+": term", "file", after.File, "include", after.Include, "err", after.Err)
	case after.Done:
		slog.Info("setup "+r.sub+": term", "file", after.File, "include", after.Include, "path", after.Report.File)
	}
	return plan, after, nil
}

// applyRules is applyKeys for the popup's rules.
func (r setupRun) applyRules(ctx context.Context) (core.PopupRulePlan, error) {
	a := r.a
	planFor := a.core.PlanInstallPopupRules
	if r.sub == "uninstall" {
		planFor = a.core.PlanUninstallPopupRules
	}
	if r.sub == "install" && r.addWhy != nil {
		return core.PopupRulePlan{}, core.ErrNoWindowRules
	}
	plan, err := planFor(ctx)
	if err != nil || r.dryRun {
		return plan, err
	}
	// A run that changed something logs "setup install: rules" or
	// "setup uninstall: rules" with action; file, revier's own rules file; and
	// moved, the number of an older revier's rules taken out of the user's
	// file. A run that failed logs it at Error with err
	// as well.
	plan = a.core.ApplyPopupRules(ctx, plan)
	switch {
	case plan.Err != "":
		slog.Error("setup "+r.sub+": rules", "action", plan.Action, "file", plan.Report.File, "moved", len(plan.Moved), "err", plan.Err)
	case plan.Done:
		slog.Info("setup "+r.sub+": rules", "action", plan.Action, "file", plan.Report.File, "moved", len(plan.Moved))
	}
	return plan, nil
}

// keysIncomplete and termIncomplete report whether a part is not what was
// asked for: a step that failed, or one held back for want of --force; the
// rules are not when their plan has an error. A caller reading JSON is the
// one most likely to be a script, so the exit status says it too, and the
// script need not parse the plan.
func keysIncomplete(plan core.KeyPlan, force bool) bool {
	for _, s := range plan.Steps {
		if s.Err != "" || (s.Action.Blocked() && !force) {
			return true
		}
	}
	return false
}

func termIncomplete(plan core.TermKeyPlan, sub string) bool {
	if plan.Err != "" || plan.Refused != "" {
		return true
	}
	if sub == "install" {
		for _, row := range plan.Report.Rows {
			if row.Status == core.TermKeyRefused {
				return true
			}
		}
	}
	return false
}
