package core

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/hk9890/revier/pkg/revier"
)

// ErrKeyConflict means the projects disagree about a key, so no plan can be
// made. `revier keys status` reports a disagreement and carries on, because it
// is a diagnostic; installing one is a coin flip, so this refuses.
var ErrKeyConflict = errors.New("the projects disagree about a key")

// ErrNoKeyWriter means the desktop can be read but not written.
var ErrNoKeyWriter = errors.New("this desktop's shortcuts cannot be changed by revier")

// KeyAction is what installing or removing does to one chord.
type KeyAction string

const (
	// KeyOK: the shortcut is already what revier would write.
	KeyOK KeyAction = "ok"
	// KeyCreate: nobody holds the chord, so revier writes its own.
	KeyCreate KeyAction = "create"
	// KeyUpdate: revier's own shortcut is there and runs the wrong command.
	KeyUpdate KeyAction = "update"
	// KeyEnable: revier's own shortcut is there and switched off.
	KeyEnable KeyAction = "enable"
	// KeyTakeOver: somebody else's shortcut holds the chord. Needs force.
	KeyTakeOver KeyAction = "take over"
	// KeyClear: a desktop default holds the chord. Needs force, and changes a
	// setting of the desktop rather than another program's shortcut.
	KeyClear KeyAction = "clear"
	// KeyRemove: uninstalling, and revier's own shortcut is there.
	KeyRemove KeyAction = "remove"
	// KeyAbsent: uninstalling, and there is nothing of revier's to remove.
	KeyAbsent KeyAction = "absent"
)

// Blocked reports whether the action needs --force.
func (a KeyAction) Blocked() bool { return a == KeyTakeOver || a == KeyClear }

// Writes reports whether the action changes anything.
func (a KeyAction) Writes() bool { return a != KeyOK && a != KeyAbsent }

// KeyStep is one chord and what is to be done with it.
type KeyStep struct {
	// Chord and Target say which key this is, in revier's own vocabulary.
	Chord  Chord     `json:"chord"`
	Target string    `json:"target"`
	Action KeyAction `json:"action"`

	// Write is the shortcut revier would install. Zero when the step removes
	// rather than installs.
	Write revier.Binding `json:"-"`
	// Evict is what has to stop firing first: somebody else's shortcut on the
	// chord, or a desktop default on it. Empty when the chord is free.
	Evict []revier.Binding `json:"-"`
	// Drop is revier's own shortcut, to be deleted. Only an uninstall fills
	// it: revier removes what it wrote and never what it did not.
	Drop []revier.Binding `json:"-"`

	// HeldBy names what is in the way, for the line the command prints.
	HeldBy string `json:"held_by,omitempty"`
	// Undo is the command that puts a cleared desktop setting back. Empty
	// unless the action is KeyClear.
	Undo string `json:"undo,omitempty"`

	// Done and Err are filled by ApplyKeys. A step that was blocked by force,
	// or that had nothing to do, is neither.
	Done bool   `json:"done,omitempty"`
	Err  string `json:"error,omitempty"`
}

// KeyPlan is what a run of install or uninstall will do, before it does it.
// The same value is what the command prints afterwards, with Done and Err
// filled, so what was promised and what happened have one shape.
type KeyPlan struct {
	Desktop string    `json:"desktop"`
	Steps   []KeyStep `json:"steps"`
}

// Blocked counts the steps that need --force.
func (p KeyPlan) Blocked() int {
	n := 0
	for _, s := range p.Steps {
		if s.Action.Blocked() {
			n++
		}
	}
	return n
}

// Installed counts the chords revier holds, or would hold once the plan runs.
func (p KeyPlan) Installed() int {
	n := 0
	for _, s := range p.Steps {
		if s.Action == KeyOK || s.Done {
			n++
		}
	}
	return n
}

// bindingID is the name revier gives a shortcut it owns. It is stable across
// runs and unlike anything a desktop allocates for itself - GNOME numbers its
// own entries - so the two never claim the same storage.
func bindingID(target string) string { return "revier-" + target }

// bindingLabel is what the desktop's settings window shows.
func bindingLabel(target string) string { return "revier: " + target }

// PlanInstallKeys works out what claiming the desktop keys would do, and
// changes nothing.
//
// It refuses when the projects disagree about a key, because installing either
// spelling of a disagreement is a guess. `revier keys status` reports the same
// disagreement and carries on; that is the difference between a report and a
// change.
func (c *Core) PlanInstallKeys(ctx context.Context, projects []Project, trigger Chord) (KeyPlan, error) {
	report, bindings, err := c.keyState(ctx, projects, trigger)
	if err != nil {
		return KeyPlan{}, err
	}
	held := index(bindings)

	plan := KeyPlan{Desktop: report.Desktop}
	for _, row := range report.Rows {
		plan.Steps = append(plan.Steps, installStep(row, held[row.Chord]))
	}
	return plan, nil
}

// PlanUninstallKeys works out what releasing the desktop keys would do.
//
// It covers the chords revier wants and the ones it holds and no longer wants,
// so a renamed target does not leave a shortcut firing that nothing removes.
func (c *Core) PlanUninstallKeys(ctx context.Context, projects []Project, trigger Chord) (KeyPlan, error) {
	report, bindings, err := c.keyState(ctx, projects, trigger)
	if err != nil {
		return KeyPlan{}, err
	}
	held := index(bindings)

	plan := KeyPlan{Desktop: report.Desktop}
	for _, row := range report.Rows {
		plan.Steps = append(plan.Steps, removeStep(row.Chord, row.Target, held[row.Chord]))
	}
	for _, row := range report.Orphaned {
		plan.Steps = append(plan.Steps, removeStep(row.Chord, "", held[row.Chord]))
	}
	return plan, nil
}

// keyState is the one read both plans start from: the report `keys status`
// prints, plus the bindings behind it, because a plan acts on the shortcuts
// themselves and not on their labels.
func (c *Core) keyState(ctx context.Context, projects []Project, trigger Chord) (KeyReport, []revier.Binding, error) {
	if c.KeyBinder == nil {
		return KeyReport{}, nil, ErrNoKeyBinder
	}
	bindings, err := c.KeyBinder.List(ctx)
	if err != nil {
		return KeyReport{}, nil, err
	}
	report := c.report(bindings, projects, trigger)
	if err := conflicts(report); err != nil {
		return KeyReport{}, nil, err
	}
	return report, bindings, nil
}

// conflicts turns the report's own marks into a refusal, naming every chord in
// one so the file can be fixed in a single pass.
func conflicts(report KeyReport) error {
	var named []string
	for _, r := range report.Rows {
		if r.Conflict {
			named = append(named, fmt.Sprintf("%s wants %s", r.Target, r.Chord))
		}
	}
	if len(named) == 0 {
		return nil
	}
	sort.Strings(named)
	return fmt.Errorf("%w: %s. `revier keys status` shows which projects ask for what",
		ErrKeyConflict, joinAnd(named))
}

func joinAnd(in []string) string {
	switch len(in) {
	case 0:
		return ""
	case 1:
		return in[0]
	}
	out := in[0]
	for _, s := range in[1 : len(in)-1] {
		out += ", " + s
	}
	return out + " and " + in[len(in)-1]
}

// installStep decides one chord, from what the report already worked out about
// it. The statuses and the actions are the same six cases seen from the two
// sides: what is true now, and what to do about it.
func installStep(row KeyRow, holders []revier.Binding) KeyStep {
	step := KeyStep{
		Chord:  row.Chord,
		Target: row.Target,
		HeldBy: row.HeldBy,
		Write: revier.Binding{
			ID:      bindingID(row.Target),
			Chord:   row.Chord.GNOME(),
			Command: row.Command,
			Label:   bindingLabel(row.Target),
			Enabled: true,
			Source:  revier.BindingCustom,
		},
	}

	switch row.Status {
	case KeyActive:
		step.Action = KeyOK
		return step
	case KeyStale:
		step.Action = KeyUpdate
	case KeyInert:
		step.Action = KeyEnable
	case KeyFree:
		step.Action = KeyCreate
	case KeyTaken:
		step.Action = KeyTakeOver
		step.Evict = otherHolders(holders)
	case KeyBuiltin:
		step.Action = KeyClear
		step.Evict = otherHolders(holders)
		if len(step.Evict) == 1 {
			step.Undo = fmt.Sprintf("gsettings reset %s %s", step.Evict[0].Where, step.Evict[0].Label)
		}
	}

	// An update or an enable acts on revier's own entry where the desktop
	// already keeps it, so the write does not move it to a new place.
	if own, ok := ownHolder(holders); ok {
		step.Write.ID = own.ID
		step.Write.Where = own.Where
	}
	return step
}

// removeStep decides one chord for an uninstall. Only revier's own shortcuts
// are removed: everything else on the chord is somebody's configuration.
func removeStep(chord Chord, target string, holders []revier.Binding) KeyStep {
	step := KeyStep{Chord: chord, Target: target, Action: KeyAbsent}
	for _, b := range holders {
		if b.Source == revier.BindingCustom && ownedCommand(b.Command) {
			step.Drop = append(step.Drop, b)
		}
	}
	if len(step.Drop) > 0 {
		step.Action = KeyRemove
		step.HeldBy = step.Drop[0].Label
	}
	return step
}

// otherHolders is everything on the chord that is not revier's own: the
// shortcuts that have to stop firing before revier's can be the one that runs.
func otherHolders(holders []revier.Binding) []revier.Binding {
	var out []revier.Binding
	for _, b := range holders {
		if b.Source == revier.BindingCustom && ownedCommand(b.Command) {
			continue
		}
		if b.Source == revier.BindingCustom && !b.Enabled {
			// Switched off already: it fires on no press, so taking the chord
			// does not need it touched.
			continue
		}
		out = append(out, b)
	}
	return out
}

// ownHolder is revier's own shortcut on the chord, if the desktop holds one.
func ownHolder(holders []revier.Binding) (revier.Binding, bool) {
	for _, b := range holders {
		if b.Source == revier.BindingCustom && ownedCommand(b.Command) {
			return b, true
		}
	}
	return revier.Binding{}, false
}

// ApplyKeys carries out a plan and returns it with each step marked.
//
// Without force, a step that would take a chord from something else is left
// undone rather than failing the run: a machine where three keys are free and
// one is a desktop default should get three keys. The caller reports what was
// skipped, and `revier keys status` shows the half state afterwards.
//
// A step that fails does not stop the ones after it. Each chord is independent,
// and stopping would leave the reason for the stop hidden behind the first
// failure.
func (c *Core) ApplyKeys(ctx context.Context, plan KeyPlan, force bool) KeyPlan {
	w, ok := c.KeyBinder.(revier.KeyWriter)
	if !ok {
		for i := range plan.Steps {
			plan.Steps[i].Err = ErrNoKeyWriter.Error()
		}
		return plan
	}

	for i := range plan.Steps {
		s := &plan.Steps[i]
		if !s.Action.Writes() || (s.Action.Blocked() && !force) {
			continue
		}
		if err := applyStep(ctx, w, *s); err != nil {
			s.Err = err.Error()
			continue
		}
		s.Done = true
	}
	return plan
}

// applyStep does one chord: what is in the way stops firing, then revier's own
// shortcut is written. The order matters - a desktop that already refuses two
// shortcuts on one chord would refuse the write otherwise.
func applyStep(ctx context.Context, w revier.KeyWriter, s KeyStep) error {
	for _, b := range s.Evict {
		if err := w.Disable(ctx, b); err != nil {
			return fmt.Errorf("switch off %s: %w", b.Label, err)
		}
	}
	for _, b := range s.Drop {
		if err := w.Remove(ctx, b); err != nil {
			return fmt.Errorf("remove %s: %w", b.Label, err)
		}
	}
	if s.Action == KeyRemove || s.Action == KeyAbsent {
		return nil
	}
	return w.Bind(ctx, s.Write)
}
