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

	keys, err := r.applyKeys(ctx)
	if err != nil {
		out.fail("keys", err, core.ErrNoKeyBinder, "")
	} else {
		out.Keys = &keys
	}
	termBefore, term, err := r.applyTerm(ctx)
	if err != nil {
		out.fail("term", err, core.ErrNoTermKeys, "")
	} else {
		out.Term = &term
	}
	rules, err := r.applyRules(ctx)
	if err != nil {
		out.fail("rules", err, core.ErrNoWindowRules, r.skipped(err))
	} else {
		out.Rules = &rules
	}

	if asJSON {
		if err := a.printJSON(out); err != nil {
			return err
		}
	} else {
		r.print(a.out, out, termBefore)
	}

	if len(out.Failed) > 0 ||
		(out.Keys != nil && keysIncomplete(*out.Keys, r.force)) ||
		(out.Term != nil && termIncomplete(*out.Term, r.sub)) ||
		(out.Rules != nil && rulesIncomplete(*out.Rules, r.force)) {
		return errSetupIncomplete
	}
	return nil
}

func (r setupRun) print(w io.Writer, out setupPlan, termBefore core.TermKeyPlan) {
	_, _ = fmt.Fprintf(w, "=== revier setup %s ===\n\n", r.sub)
	if r.dryRun {
		_, _ = fmt.Fprint(w, "  dry run: nothing is changed\n\n")
	}
	_, _ = fmt.Fprintln(w, "desktop keys:")
	if out.Keys != nil {
		printPlan(w, *out.Keys, r.sub, r.dryRun, r.force)
	} else {
		printPartNot(w, out, "keys")
	}
	_, _ = fmt.Fprintln(w, "\nterminal keys:")
	if out.Term != nil {
		printTermPlan(w, termBefore, *out.Term, r.sub, r.dryRun)
	} else {
		printPartNot(w, out, "term")
	}
	_, _ = fmt.Fprintln(w, "\npopup rules:")
	if out.Rules != nil {
		printRulePlan(w, *out.Rules, r.sub, r.dryRun, r.force)
	} else {
		printPartNot(w, out, "rules")
	}
}

// fail records a part that could not be planned: skipped when the machine
// does not have it, which is the sentinel, and failed otherwise. why, when
// set, says why it is skipped in place of the sentinel.
func (p *setupPlan) fail(part string, err, sentinel error, why string) {
	switch {
	case !errors.Is(err, sentinel):
		p.Failed[part] = err.Error()
	case why != "":
		p.Skipped[part] = why
	default:
		p.Skipped[part] = err.Error()
	}
}

func printPartNot(w io.Writer, p setupPlan, part string) {
	if why, ok := p.Skipped[part]; ok {
		_, _ = fmt.Fprintf(w, "  - skipped: %s\n", why)
		return
	}
	_, _ = fmt.Fprintf(w, "  ! failed: %s\n", p.Failed[part])
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
	plan, err := planFor(ctx)
	if err != nil || r.dryRun {
		return plan, err
	}
	plan = a.core.ApplyPopupRules(ctx, plan, r.force)
	switch {
	case plan.Err != "":
		slog.Error("setup "+r.sub+": rules", "action", plan.Action, "err", plan.Err)
	case plan.Done:
		slog.Info("setup "+r.sub+": rules", "action", plan.Action)
	}
	return plan, nil
}

// keysIncomplete, termIncomplete and rulesIncomplete report whether a part
// is not what was asked for: a step that failed, or one held back for want
// of --force. A caller reading JSON is the one most likely to be a script,
// so the exit status says it too, and the script need not parse the plan.
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

func rulesIncomplete(plan core.PopupRulePlan, force bool) bool {
	return plan.Err != "" || (plan.Action.Blocked() && !force)
}
