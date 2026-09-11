package core

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

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
	// Own is what happens to revier's own shortcut on the chord: created,
	// rewritten, switched on, or already right. Action says it too, except
	// on a take over or a clear, where Action is about what is displaced and
	// revier's own shortcut may exist already.
	Own KeyAction `json:"own,omitempty"`

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
	if err := conflicts(report); err != nil {
		return KeyPlan{}, err
	}
	held := index(bindings)

	plan := KeyPlan{Desktop: report.Desktop}
	for _, row := range report.Rows {
		plan.Steps = append(plan.Steps, installStep(row, held[row.Chord]))
	}
	entryPerStep(plan.Steps, bindings)
	return plan, nil
}

// entryPerStep gives every step that creates a shortcut an entry no other step
// writes and nobody else owns.
//
// A new shortcut is named after its target, and that name can be in use. When
// a key moves between targets - home leaves ctrl+shift+u, web takes it - web
// rewrites the entry revier-home in place, and home's new key would be written
// to the same entry: one of the two keys is lost, and both steps report done.
// An entry that is somebody else's is never overwritten either. revier's own
// entry on a chord nothing wants any more may be reused: moving it is what
// renaming the key means.
func entryPerStep(steps []KeyStep, bindings []revier.Binding) {
	taken := map[string]bool{}
	for _, b := range bindings {
		if b.Source == revier.BindingCustom && !ownedCommand(b.Command) {
			taken[b.ID] = true
		}
	}
	for _, s := range steps {
		if s.Own != KeyCreate {
			taken[s.Write.ID] = true
		}
	}
	for i := range steps {
		s := &steps[i]
		if s.Own != KeyCreate {
			continue
		}
		id := s.Write.ID
		for n := 2; taken[id]; n++ {
			id = fmt.Sprintf("%s-%d", s.Write.ID, n)
		}
		s.Write.ID = id
		taken[id] = true
	}
}

// PlanUninstallKeys works out what releasing the desktop keys would do.
//
// It covers the chords revier wants and the ones it holds and no longer wants,
// so a renamed target does not leave a shortcut firing that nothing removes.
//
// One step per chord, not one per row: a step removes everything of revier's
// on its chord, so two rows sharing one - two orphans on the same key, or a
// configuration disagreement - would otherwise remove each entry twice and
// print the same line twice.
func (c *Core) PlanUninstallKeys(ctx context.Context, projects []Project, trigger Chord) (KeyPlan, error) {
	report, bindings, err := c.keyState(ctx, projects, trigger)
	if err != nil {
		return KeyPlan{}, err
	}
	held := index(bindings)

	plan := KeyPlan{Desktop: report.Desktop}
	seen := map[Chord]bool{}
	add := func(chord Chord, target string) {
		if seen[chord] {
			return
		}
		seen[chord] = true
		plan.Steps = append(plan.Steps, removeStep(chord, target, held[chord]))
	}
	for _, row := range report.Rows {
		add(row.Chord, row.Target)
	}
	for _, row := range report.Orphaned {
		add(row.Chord, "")
	}
	return plan, nil
}

// keyState is the one read both plans start from: the report `keys status`
// prints, plus the bindings behind it, because a plan acts on the shortcuts
// themselves and not on their labels.
//
// It does not refuse a configuration disagreement. Only installing has to
// choose between two spellings of one; removing what revier wrote is the same
// answer either way, and refusing there would leave a user unable to give the
// keys back until the file that disagrees is fixed.
func (c *Core) keyState(ctx context.Context, projects []Project, trigger Chord) (KeyReport, []revier.Binding, error) {
	if c.KeyBinder == nil {
		return KeyReport{}, nil, ErrNoKeyBinder
	}
	bindings, err := c.KeyBinder.List(ctx)
	if err != nil {
		return KeyReport{}, nil, err
	}
	return c.report(bindings, projects, trigger), bindings, nil
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
		// Not returned yet. revier's shortcut being right does not make the
		// key revier's while a desktop default sits on the same chord, and
		// the check below is what notices.
		step.Action = KeyOK
	case KeyStale:
		step.Action = KeyUpdate
	case KeyInert:
		step.Action = KeyEnable
	case KeyFree:
		step.Action = KeyCreate
	case KeyTaken:
		step.Action = KeyTakeOver
	case KeyBuiltin:
		step.Action = KeyClear
	}

	// What has to stop firing is read off the chord and not off the status.
	// A status names the holder a user has to act on first, and a desktop
	// default sitting beside revier's own stale entry is not that holder -
	// but it still fires, so rewriting the command without clearing it would
	// leave the key doing what it did before.
	step.Evict = otherHolders(holders)
	if len(step.Evict) > 0 {
		// Displacing something that is not revier's needs --force, whatever
		// the status was named after.
		if !step.Action.Blocked() {
			step.Action = KeyTakeOver
			if allBuiltin(step.Evict) {
				step.Action = KeyClear
			}
		}
		// What is in the way is what is displaced. The status names every
		// live shortcut on the chord, revier's own among them, and revier's
		// own is never switched off.
		step.HeldBy = holderNames(step.Evict)
	}
	step.Undo = undoFor(step.Evict)
	step.Own = KeyCreate

	// An update or an enable acts on revier's own entry where the desktop
	// already keeps it, so the write does not move it to a new place.
	if own, ok := ownHolder(holders); ok {
		step.Write.ID = own.ID
		step.Write.Where = own.Where
		step.Own = ownAction(own, row.Command)
	}
	return step
}

// ownAction is what an install does to revier's own shortcut: rewrite a
// command that is out of date, switch on one that is off, or leave it.
func ownAction(own revier.Binding, want string) KeyAction {
	switch {
	case own.Command != want:
		return KeyUpdate
	case !own.Enabled:
		return KeyEnable
	default:
		return KeyOK
	}
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

// allBuiltin reports whether everything to be evicted is a desktop default,
// which is the difference between taking a key from a program and changing a
// setting of the desktop.
func allBuiltin(evict []revier.Binding) bool {
	for _, b := range evict {
		if b.Source != revier.BindingBuiltin {
			return false
		}
	}
	return len(evict) > 0
}

// undoFor is the command that puts back every desktop default the step clears.
// A default is a setting rather than an entry, so nothing revier keeps and
// nothing the displaced program does will return it: this line is the only way
// back, and it is printed beside the key.
func undoFor(evict []revier.Binding) string {
	var out []string
	for _, b := range evict {
		if b.Source == revier.BindingBuiltin {
			out = append(out, fmt.Sprintf("gsettings reset %s %s", b.Where, b.Label))
		}
	}
	return strings.Join(out, "; ")
}

// holderNames names what is being evicted, spelled the way the report spells
// it: a shortcut by its label, a desktop default by its setting.
func holderNames(evict []revier.Binding) string {
	names := make([]string, 0, len(evict))
	for _, b := range evict {
		if b.Source == revier.BindingBuiltin {
			names = append(names, b.Where+" "+b.Label)
			continue
		}
		names = append(names, b.Label)
	}
	return strings.Join(names, ", ")
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
	var off []revier.Binding
	for _, b := range s.Evict {
		if err := w.Disable(ctx, b); err != nil {
			return stranded(fmt.Errorf("switch off %s: %w", b.Label, err), off)
		}
		off = append(off, b)
	}
	for _, b := range s.Drop {
		if err := w.Remove(ctx, b); err != nil {
			return fmt.Errorf("remove %s: %w", b.Label, err)
		}
	}
	if s.Action == KeyRemove || s.Action == KeyAbsent {
		return nil
	}
	if err := w.Bind(ctx, s.Write); err != nil {
		return stranded(err, off)
	}
	return nil
}

// stranded is a failed step's error, naming what the step had already switched
// off. Those stay off - revier has no verb that switches somebody else's
// shortcut back on, and a desktop that just refused one write is not one to
// trust with another - so the key may now run nothing, and the line printed
// against it has to say why.
func stranded(err error, off []revier.Binding) error {
	if len(off) == 0 {
		return err
	}
	return fmt.Errorf("%w; %s switched off already and stays off, so the key may run nothing",
		err, holderNames(off))
}
