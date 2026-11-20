package core

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/hk9890/revier/pkg/revier"
)

// The popup's rules are revier's third store of the user's configuration,
// beside the desktop's keys and the terminal's (decisions.md D191). The
// window host places a window its rules match before the window's first
// frame, so a popup that a rule places does not jump into place. revier
// writes them as a file of its own, whole, and writes the user's file only
// to take out a rule an older revier wrote there, which it knows by its
// exact shape. The host tries the user's file first, so any other rule of
// the user's for the popup's class wins over revier's, and revier reports it
// and leaves it.

// ErrNoWindowRules means this machine has no window host whose rules revier
// writes.
var ErrNoWindowRules = errors.New("no window host whose rules revier writes on this machine")

// ErrNoWindowRuleWriter means the window host's rules can be read but not
// written.
var ErrNoWindowRuleWriter = errors.New("this window host's rules cannot be changed by revier")

// PopupRules are the rules revier writes for the popup, in the order the
// host tries them: one for each of popupTiers.
func PopupRules() []revier.WindowRule {
	out := make([]revier.WindowRule, len(popupTiers))
	for i, t := range popupTiers {
		out[i] = revier.WindowRule{Class: PopupClass, MinWidth: t.minWidth, Place: strings.Fields(t.place), Overlay: true}
	}
	return out
}

// olderPopupRule reports whether a rule in the user's file is one an older
// revier put there: a tier revier 0.28 wrote, which are PopupRules, or the
// overlay rule its guide had users copy, the class and an overlay alone. A
// rule in any other shape is the user's, even one revier's could have been:
// revier cannot tell an override of the user's from it.
func olderPopupRule(r revier.WindowRule) bool {
	guide := revier.WindowRule{Class: PopupClass, Overlay: true}
	return sameRule(r, guide) || slices.ContainsFunc(PopupRules(), func(w revier.WindowRule) bool { return sameRule(r, w) })
}

// PopupRuleState is what revier's own rules file holds.
type PopupRuleState string

const (
	// PopupRulesCurrent: revier's file holds the rules this revier writes,
	// and no older revier's rule is in the user's file.
	PopupRulesCurrent PopupRuleState = "current"
	// PopupRulesStale: revier's file holds other rules, or the host would
	// not load it, or an older revier's rules are in the user's file.
	// `revier setup install` writes the file and takes them out.
	PopupRulesStale PopupRuleState = "stale"
	// PopupRulesMissing: no file, and no rule of an older revier's.
	PopupRulesMissing PopupRuleState = "missing"
)

// PopupRuleFound is a rule for the popup's class, where it is.
type PopupRuleFound struct {
	File  string            `json:"file"`
	Index int               `json:"index"`
	Rule  revier.WindowRule `json:"rule"`
}

// PopupRuleReport is what `revier setup status` prints of the popup's rules.
type PopupRuleReport struct {
	Host  string         `json:"host"`
	State PopupRuleState `json:"state"`
	// File is revier's own rules file, Exists whether it is there, and Own
	// the rules it holds. Broken is why the host would not load it.
	File   string `json:"file"`
	Exists bool   `json:"exists"`
	// UserBroken is why the host would not load the user's file. Its rules
	// are then not read: the host loads revier's file without them, and an
	// older revier's rules in it wait for the next run after it is fixed.
	UserBroken string              `json:"user_broken,omitempty"`
	Own        []revier.WindowRule `json:"own"`
	Broken     string              `json:"broken,omitempty"`
	Want       []revier.WindowRule `json:"want"`
	// Legacy is each rule an older revier put into the user's file.
	Legacy []PopupRuleFound `json:"legacy"`
	// Ahead is each rule of another file for the popup's class that the
	// host tries before revier's file. The first one that matches wins.
	Ahead []PopupRuleFound `json:"ahead"`
}

// PlacesPopup reports whether the host's rules place the popup, so that
// revier need not. The host applies the first rule that matches, so the
// rules ahead of revier's file decide in their order: one that places
// nothing leaves the popup unplaced on the monitors it matches, and one
// that places it on any monitor, with nothing revier cannot read, ends the
// question. A rule that holds something revier cannot read is the user's
// choice of what the popup gets, and is taken to place it. Past them,
// revier's file places it where it is current.
func (r PopupRuleReport) PlacesPopup() bool {
	for _, f := range r.Ahead {
		if len(f.Rule.Place) == 0 && !f.Rule.Other {
			return false
		}
		if f.Rule.MinWidth == 0 && !f.Rule.Other {
			return true
		}
	}
	return r.State == PopupRulesCurrent
}

// PopupRuleAction is what install or uninstall does to the rules.
type PopupRuleAction string

const (
	PopupRuleOK PopupRuleAction = "ok"
	// PopupRuleWrite writes revier's file where it is not current, and takes
	// an older revier's rules out of the user's file.
	PopupRuleWrite PopupRuleAction = "write"
	// PopupRuleRemove deletes revier's file, and takes an older revier's
	// rules out of the user's file.
	PopupRuleRemove PopupRuleAction = "remove"
	// PopupRuleAbsent: uninstall finds no rule of revier's.
	PopupRuleAbsent PopupRuleAction = "absent"
)

// Writes reports whether the action changes the rules.
func (a PopupRuleAction) Writes() bool {
	return a == PopupRuleWrite || a == PopupRuleRemove
}

// PopupRulePlan is what install or uninstall does to the popup's rules, and,
// once applied, what became of it.
type PopupRulePlan struct {
	Action PopupRuleAction `json:"action"`
	Report PopupRuleReport `json:"report"`
	// Moved is each rule of an older revier's that the plan takes out of
	// the user's file: the report's Legacy as the plan found it.
	Moved []PopupRuleFound `json:"moved"`
	// Done and Err are filled by ApplyPopupRules. Report is then the rules
	// as they are read after the run.
	Done bool   `json:"done"`
	Err  string `json:"error,omitempty"`
	// Removed is each rule the run took out of the user's file that revier
	// does not write now, as the host stored it: the overlay rule the guide
	// had users copy. revier prints each rule it takes out (decisions.md
	// D191) and keeps no copy, so this is the user's one way to put it back.
	Removed []revier.WindowRule `json:"removed,omitempty"`
}

// PopupRuleReport reads the host's rules.
func (c *Core) PopupRuleReport(ctx context.Context) (PopupRuleReport, error) {
	if c.WindowRules == nil {
		return PopupRuleReport{}, ErrNoWindowRules
	}
	set, err := c.WindowRules.Read(ctx)
	if err != nil {
		return PopupRuleReport{}, err
	}
	return popupRuleReport(c.WindowRules.Name(), set), nil
}

func popupRuleReport(host string, set revier.WindowRuleSet) PopupRuleReport {
	r := PopupRuleReport{
		Host: host, File: set.Own.Path, Exists: set.OwnExists, UserBroken: set.UserBroken, Own: slices.Clone(set.Own.Rules), Broken: set.OwnBroken,
		Want: PopupRules(), Legacy: []PopupRuleFound{}, Ahead: []PopupRuleFound{},
	}
	if r.Own == nil {
		r.Own = []revier.WindowRule{}
	}
	for i, rule := range set.User.Rules {
		if rule.Class != PopupClass {
			continue
		}
		found := PopupRuleFound{File: set.User.Path, Index: i, Rule: rule}
		if olderPopupRule(rule) {
			r.Legacy = append(r.Legacy, found)
		} else {
			r.Ahead = append(r.Ahead, found)
		}
	}
	for _, f := range set.Earlier {
		for i, rule := range f.Rules {
			if rule.Class == PopupClass {
				r.Ahead = append(r.Ahead, PopupRuleFound{File: f.Path, Index: i, Rule: rule})
			}
		}
	}
	switch {
	case r.ownCurrent() && len(r.Legacy) == 0:
		r.State = PopupRulesCurrent
	case set.OwnExists || len(r.Legacy) > 0:
		r.State = PopupRulesStale
	default:
		r.State = PopupRulesMissing
	}
	return r
}

// ownCurrent reports whether revier's file holds the rules this revier
// writes.
func (r PopupRuleReport) ownCurrent() bool {
	return r.Exists && r.Broken == "" && slices.EqualFunc(r.Own, r.Want, sameRule)
}

func sameRule(a, b revier.WindowRule) bool {
	return a.Class == b.Class && a.MinWidth == b.MinWidth && a.Overlay == b.Overlay && !a.Other && !b.Other && slices.Equal(a.Place, b.Place)
}

// PlanInstallPopupRules is what install does: nothing when revier's file is
// current, and otherwise a write. A rule of the user's ahead of revier's
// file changes neither: it is the user's to keep.
func (c *Core) PlanInstallPopupRules(ctx context.Context) (PopupRulePlan, error) {
	report, err := c.PopupRuleReport(ctx)
	if err != nil {
		return PopupRulePlan{}, err
	}
	plan := PopupRulePlan{Report: report, Moved: report.Legacy, Action: PopupRuleWrite}
	if report.State == PopupRulesCurrent {
		plan.Action = PopupRuleOK
	}
	return plan, nil
}

// PlanUninstallPopupRules is what uninstall does: it removes revier's file
// and an older revier's rules in the user's file, and never a rule of the
// user's.
func (c *Core) PlanUninstallPopupRules(ctx context.Context) (PopupRulePlan, error) {
	report, err := c.PopupRuleReport(ctx)
	if err != nil {
		return PopupRulePlan{}, err
	}
	plan := PopupRulePlan{Report: report, Moved: report.Legacy, Action: PopupRuleAbsent}
	if report.State != PopupRulesMissing {
		plan.Action = PopupRuleRemove
	}
	return plan, nil
}

// ApplyPopupRules carries out a plan and returns it with its report read
// again. revier's file is written before an older revier's rules leave the
// user's file: a run that stops between the two leaves rules that place the
// popup, and the next install takes the old ones out.
func (c *Core) ApplyPopupRules(ctx context.Context, plan PopupRulePlan) PopupRulePlan {
	if !plan.Action.Writes() {
		return plan
	}
	w, ok := c.WindowRules.(revier.WindowRuleWriter)
	if !ok {
		plan.Err = ErrNoWindowRuleWriter.Error()
		return plan
	}
	removed, err := applyPopupRules(ctx, w, plan)
	plan.Removed = removed
	if err != nil {
		plan.Err = err.Error()
	}
	set, readErr := w.Read(ctx)
	if readErr != nil {
		if plan.Err == "" {
			plan.Err = readErr.Error()
		}
		return plan
	}
	plan.Report = popupRuleReport(w.Name(), set)
	settled := plan.Report.State == PopupRulesCurrent
	if plan.Action == PopupRuleRemove {
		settled = plan.Report.State == PopupRulesMissing
	}
	switch {
	case plan.Err != "":
	case !settled:
		plan.Err = "the rules read back are not what was written: another program changed them during the run"
	default:
		plan.Done = true
	}
	return plan
}

// applyPopupRules returns the rules it took out of the user's file that
// revier does not write now, also when a later step fails: they are gone
// either way.
func applyPopupRules(ctx context.Context, w revier.WindowRuleWriter, plan PopupRulePlan) ([]revier.WindowRule, error) {
	var err error
	switch {
	case plan.Action == PopupRuleRemove:
		err = w.RemoveOwn(ctx)
	case !plan.Report.ownCurrent():
		err = w.WriteOwn(ctx, plan.Report.Want)
	}
	if err != nil {
		return nil, err
	}
	var removed []revier.WindowRule
	legacy := plan.Moved
	for i := len(legacy) - 1; i >= 0; i-- {
		if err := w.RemoveUser(ctx, legacy[i].Index); err != nil {
			return removed, err
		}
		if !slices.ContainsFunc(plan.Report.Want, func(w revier.WindowRule) bool { return sameRule(legacy[i].Rule, w) }) {
			removed = slices.Insert(removed, 0, legacy[i].Rule)
		}
	}
	return removed, nil
}
