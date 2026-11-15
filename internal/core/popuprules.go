package core

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/hk9890/revier/pkg/revier"
)

// The popup's rules are revier's third store of the user's configuration,
// beside the desktop's keys and the terminal's (decisions.md D189). The
// window host places a window its rules match before the window's first
// frame, so a popup that a rule places does not jump into place. revier owns
// the class the popup is launched with, so a rule for the class in the shape
// revier writes is revier's; any other rule for it is the user's, and only
// --force replaces it, as a key that another shortcut holds.

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

// popupRuleOwned reports whether a rule is in the shape revier writes for
// the popup: its class alone, with or without a width, an overlay, and a
// place or none. A rule of an older revier, with other sizes, is one, and so
// is the overlay rule the guide had users write by hand.
func popupRuleOwned(r revier.WindowRule) bool {
	return r.Class == PopupClass && !r.Other && r.Overlay
}

// PopupRuleState is what the host's rules hold of the popup's.
type PopupRuleState string

const (
	// PopupRulesCurrent: revier's rules come first, as revier writes them,
	// and no other rule is for the popup's class.
	PopupRulesCurrent PopupRuleState = "current"
	// PopupRulesStale: rules of revier's are there, not as it writes them now.
	// `revier setup install` rewrites them.
	PopupRulesStale PopupRuleState = "stale"
	// PopupRulesHeld: a rule of the user's is for the popup's class.
	// `revier setup install --force` replaces it.
	PopupRulesHeld PopupRuleState = "held"
	// PopupRulesAbsent: no rule is for the popup's class.
	PopupRulesAbsent PopupRuleState = "absent"
)

// PopupRuleFound is a rule for the popup's class, where it is in the file.
type PopupRuleFound struct {
	Index int               `json:"index"`
	Rule  revier.WindowRule `json:"rule"`
	// Own says the rule is revier's.
	Own bool `json:"own"`
}

// PopupRuleReport is what `revier setup status` prints of the popup's rules.
type PopupRuleReport struct {
	Host  string              `json:"host"`
	State PopupRuleState      `json:"state"`
	Want  []revier.WindowRule `json:"want"`
	Found []PopupRuleFound    `json:"found"`
}

// PopupRuleAction is what install or uninstall does to the rules.
type PopupRuleAction string

const (
	PopupRuleOK PopupRuleAction = "ok"
	// PopupRuleWrite removes revier's rules for the class and adds the
	// wanted ones first.
	PopupRuleWrite PopupRuleAction = "write"
	// PopupRuleTakeOver is a write that also removes the user's rules for the
	// class. Needs force.
	PopupRuleTakeOver PopupRuleAction = "take-over"
	// PopupRuleRemove removes revier's rules for the class.
	PopupRuleRemove PopupRuleAction = "remove"
	// PopupRuleAbsent: uninstall finds no rule of revier's.
	PopupRuleAbsent PopupRuleAction = "absent"
)

// Writes reports whether the action changes the rules.
func (a PopupRuleAction) Writes() bool {
	return a == PopupRuleWrite || a == PopupRuleTakeOver || a == PopupRuleRemove
}

// Blocked reports whether the action needs --force.
func (a PopupRuleAction) Blocked() bool { return a == PopupRuleTakeOver }

// PopupRulePlan is what install or uninstall does to the popup's rules, and,
// once applied, what became of it.
type PopupRulePlan struct {
	Action PopupRuleAction `json:"action"`
	Report PopupRuleReport `json:"report"`
	// Done and Err are filled by ApplyPopupRules. Report is then the rules
	// as they are read after the run.
	Done bool   `json:"done"`
	Err  string `json:"error,omitempty"`
	// Removed is each rule of the user's that a take-over removed, as the
	// host stored it. revier keeps no copy, so this is the user's one way to
	// put it back: no tool writes a rule by hand again (decisions.md D189).
	Removed []revier.WindowRule `json:"removed,omitempty"`
}

// PopupRuleReport reads the host's rules.
func (c *Core) PopupRuleReport(ctx context.Context) (PopupRuleReport, error) {
	if c.WindowRules == nil {
		return PopupRuleReport{}, ErrNoWindowRules
	}
	rules, err := c.WindowRules.Read(ctx)
	if err != nil {
		return PopupRuleReport{}, err
	}
	return popupRuleReport(c.WindowRules.Name(), rules), nil
}

func popupRuleReport(host string, rules []revier.WindowRule) PopupRuleReport {
	r := PopupRuleReport{Host: host, Want: PopupRules(), Found: []PopupRuleFound{}}
	foreign := false
	for i, rule := range rules {
		if rule.Class != PopupClass {
			continue
		}
		own := popupRuleOwned(rule)
		foreign = foreign || !own
		r.Found = append(r.Found, PopupRuleFound{Index: i, Rule: rule, Own: own})
	}
	switch {
	case len(r.Found) == 0:
		r.State = PopupRulesAbsent
	case foreign:
		r.State = PopupRulesHeld
	case len(rules) >= len(r.Want) && len(r.Found) == len(r.Want) && slices.EqualFunc(rules[:len(r.Want)], r.Want, sameRule):
		r.State = PopupRulesCurrent
	default:
		r.State = PopupRulesStale
	}
	return r
}

func sameRule(a, b revier.WindowRule) bool {
	return a.Class == b.Class && a.MinWidth == b.MinWidth && a.Overlay == b.Overlay && !a.Other && !b.Other && slices.Equal(a.Place, b.Place)
}

// PlanInstallPopupRules is what install does: nothing when the rules are
// current, and otherwise a write, which a rule of the user's for the class
// makes one that needs force.
func (c *Core) PlanInstallPopupRules(ctx context.Context) (PopupRulePlan, error) {
	report, err := c.PopupRuleReport(ctx)
	if err != nil {
		return PopupRulePlan{}, err
	}
	plan := PopupRulePlan{Report: report}
	switch report.State {
	case PopupRulesCurrent:
		plan.Action = PopupRuleOK
	case PopupRulesHeld:
		plan.Action = PopupRuleTakeOver
	default:
		plan.Action = PopupRuleWrite
	}
	return plan, nil
}

// PlanUninstallPopupRules is what uninstall does: it removes revier's rules,
// and never one of the user's.
func (c *Core) PlanUninstallPopupRules(ctx context.Context) (PopupRulePlan, error) {
	report, err := c.PopupRuleReport(ctx)
	if err != nil {
		return PopupRulePlan{}, err
	}
	plan := PopupRulePlan{Report: report, Action: PopupRuleAbsent}
	for _, f := range report.Found {
		if f.Own {
			plan.Action = PopupRuleRemove
		}
	}
	return plan, nil
}

// ApplyPopupRules carries out a plan and returns it with its report read
// again. A write adds the wanted rules at the top first, then removes the
// rules they replace from the last one up, each the wanted rules' number
// further down than the report found it: a step that fails leaves rules that
// place the popup. A take-over without force changes nothing.
func (c *Core) ApplyPopupRules(ctx context.Context, plan PopupRulePlan, force bool) PopupRulePlan {
	if !plan.Action.Writes() || (plan.Action.Blocked() && !force) {
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
	rules, readErr := w.Read(ctx)
	if readErr != nil {
		if plan.Err == "" {
			plan.Err = readErr.Error()
		}
		return plan
	}
	plan.Report = popupRuleReport(w.Name(), rules)
	settled := plan.Report.State == PopupRulesCurrent
	if plan.Action == PopupRuleRemove {
		settled = !slices.ContainsFunc(plan.Report.Found, func(f PopupRuleFound) bool { return f.Own })
	}
	switch {
	case plan.Err != "":
	case !settled:
		plan.Err = "the rules read back are not what was written: another program changed the file during the run"
	default:
		plan.Done = true
	}
	return plan
}

// applyPopupRules returns the rules of the user's it removed, also when a
// later step fails: they are gone either way.
func applyPopupRules(ctx context.Context, w revier.WindowRuleWriter, plan PopupRulePlan) ([]revier.WindowRule, error) {
	shift := 0
	if plan.Action != PopupRuleRemove {
		for i, rule := range plan.Report.Want {
			if err := w.Add(ctx, i, rule); err != nil {
				return nil, err
			}
		}
		shift = len(plan.Report.Want)
	}
	var removed []revier.WindowRule
	found := plan.Report.Found
	for i := len(found) - 1; i >= 0; i-- {
		if !found[i].Own && plan.Action != PopupRuleTakeOver {
			continue
		}
		if err := w.Remove(ctx, found[i].Index+shift); err != nil {
			return removed, err
		}
		if !found[i].Own {
			removed = slices.Insert(removed, 0, found[i].Rule)
		}
	}
	return removed, nil
}
