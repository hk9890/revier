package core

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/hk9890/revier/pkg/revier"
)

// ErrNoKeyBinder means this machine has no desktop revier can read shortcuts
// from. It is a normal outcome on a headless box, not a failure.
var ErrNoKeyBinder = errors.New("no desktop keybinding support on this machine")

// PickerTarget is the row for the chord that opens revier itself. It is not a
// target - nothing is run-or-raised, the surface comes up - so it has a name
// of its own rather than borrowing a target's, and ValidateKeyTarget keeps a
// target with a key from taking it.
const PickerTarget = "picker"

// KeyStatus is what a chord revier wants is doing right now.
type KeyStatus string

const (
	// KeyActive: revier holds the chord and it runs what revier would install.
	KeyActive KeyStatus = "active"
	// KeyStale: revier holds it, but runs something else. An older install.
	KeyStale KeyStatus = "stale"
	// KeyInert: revier's entry is in the desktop's store but switched off, so
	// nothing happens on a press. Invisible in the settings UI, which is why
	// it is reported.
	KeyInert KeyStatus = "inert"
	// KeyTaken: some other shortcut holds it.
	KeyTaken KeyStatus = "taken"
	// KeyFree: nobody holds it.
	KeyFree KeyStatus = "free"
	// KeyBuiltin: the desktop itself holds it, outside the custom shortcuts.
	// revier cannot claim it without changing a desktop default.
	KeyBuiltin KeyStatus = "builtin"
)

// KeyRow is one chord and who holds it.
type KeyRow struct {
	Chord  Chord     `json:"chord"`
	Target string    `json:"target"`
	Status KeyStatus `json:"status"`
	// HeldBy names whatever holds the chord: a shortcut's label, or the
	// desktop setting for a built-in. Empty when the chord is free.
	HeldBy string `json:"held_by,omitempty"`
	// Command is what revier would install on this chord, and for an orphan
	// what is installed there now.
	Command string `json:"command"`
	// Projects are the projects that ask for this chord, in configuration
	// order. Empty for the picker, which no project declares.
	Projects []revier.ProjectName `json:"projects,omitempty"`
	// Conflict is set when the projects do not agree: the same target is
	// bound to more than one chord, so more than one row carries the target.
	Conflict bool `json:"conflict,omitempty"`
}

// KeyReport is the answer to "which chords does revier hold".
type KeyReport struct {
	// Desktop is the keybinder that was read, for the message a user needs
	// when the answer is surprising.
	Desktop string `json:"desktop"`
	// Rows is one entry per chord revier wants, whether or not it holds it.
	Rows []KeyRow `json:"rows"`
	// Orphaned is every chord revier holds and no longer wants: what an
	// earlier install left behind after a target was renamed or removed.
	Orphaned []KeyRow `json:"orphaned,omitempty"`
}

// The commands revier binds to a desktop key. A login shell, so the desktop
// finds revier on the same PATH a terminal has.
const (
	pickerCommand   = `sh -lc "revier popup"`
	goCommandPrefix = `sh -lc "revier go `
	goCommandSuffix = ` --picker"`
)

func targetCommand(name revier.TargetName) string {
	return goCommandPrefix + string(name) + goCommandSuffix
}

// keyTargetName is the shape of every target name. The name is written into
// the shell command a desktop key runs, and read back out of it to tell
// revier's shortcuts from anybody else's, so it is one plain word that no
// shell reads as syntax and revier go does not read as a flag. It holds for a
// target with no key too: a key added later must not find its name refused.
var keyTargetName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// ValidateTargetName refuses a name no target can have. config runs it at
// load, so a key that could not be installed, or would be taken for somebody
// else's once it was, fails before anything is written.
func ValidateTargetName(name revier.TargetName) error {
	if !keyTargetName.MatchString(string(name)) {
		return fmt.Errorf("target name %q: it goes into the command the target's key runs; use letters, digits, '.', '_' and '-', starting with a letter or digit",
			name)
	}
	return nil
}

// ValidateKeyTarget refuses a name a target with a key cannot have on top of
// that: the picker's row, which the key opening revier already holds.
func ValidateKeyTarget(name revier.TargetName) error {
	if name == PickerTarget {
		return fmt.Errorf("target %q has a key, and %q names the key that opens revier ([ui] trigger_key); rename the target",
			name, PickerTarget)
	}
	return nil
}

// owned reports whether a shortcut is revier's: uninstall deletes it, and
// install rewrites it without --force. It is revier's when it sits in an entry
// revier names (bindingID), whatever it runs now, so an install from an older
// revier - one that ran another command - is rewritten in place and not left
// beside the new one (decisions.md D77). It is revier's too when it runs
// exactly a command revier writes, wherever the desktop filed it, because a
// user who bound `sh -lc "revier go editor --picker"` by hand owns the same key
// revier does.
func owned(b revier.Binding) bool {
	if b.Source != revier.BindingCustom {
		return false
	}
	return strings.HasPrefix(b.ID, bindingIDPrefix) || ownedCommand(b.Command)
}

// ownedCommand reports whether a command is exactly one revier writes: the
// picker's, or one target's. A user's own shortcut that merely runs revier -
// `revier go editor && notify-send done` in an entry of their own - is theirs,
// and is treated as anybody else's.
func ownedCommand(cmd string) bool {
	if cmd == pickerCommand {
		return true
	}
	name, ok := strings.CutPrefix(cmd, goCommandPrefix)
	if !ok {
		return false
	}
	name, ok = strings.CutSuffix(name, goCommandSuffix)
	return ok && keyTargetName.MatchString(name)
}

// Keys reports which desktop chords revier wants and who holds each one.
//
// It reads. Nothing here changes a binding: claiming a chord is a separate
// operation, and a status command that could write is one an agent cannot be
// allowed to run on the user's live desktop.
func (c *Core) Keys(ctx context.Context, projects []Project, trigger Chord) (KeyReport, error) {
	if c.KeyBinder == nil {
		return KeyReport{}, ErrNoKeyBinder
	}
	bindings, err := c.KeyBinder.List(ctx)
	if err != nil {
		return KeyReport{}, err
	}
	return c.report(bindings, projects, trigger), nil
}

// report is the whole of the classification, over bindings already read. It is
// split from Keys because installing starts from the same answer: what is true
// now decides what to do about it, and two commands working from one report
// cannot disagree about the desktop.
func (c *Core) report(bindings []revier.Binding, projects []Project, trigger Chord) KeyReport {
	report := KeyReport{Desktop: c.KeyBinder.Name()}
	held := index(bindings)
	wanted := wantedChords(projects, trigger)
	for _, w := range wanted {
		report.Rows = append(report.Rows, classify(w, held[w.Chord]))
	}
	report.Orphaned = orphans(bindings, wanted)
	return report
}

// index groups the desktop's shortcuts by canonical chord. A chord revier
// cannot parse is dropped: it belongs to no chord revier wants, so it can
// neither hold one nor be an orphan.
func index(bindings []revier.Binding) map[Chord][]revier.Binding {
	out := map[Chord][]revier.Binding{}
	for _, b := range bindings {
		ch, err := ParseChord(b.Chord)
		if err != nil {
			continue
		}
		out[ch] = append(out[ch], b)
	}
	return out
}

// wantedChords is every chord revier would bind: the picker, plus the union of
// the key each target declares. A chord one project declares is wanted, so a
// target that exists in a single project still gets a row.
func wantedChords(projects []Project, trigger Chord) []KeyRow {
	rows := []KeyRow{{Chord: trigger, Target: PickerTarget, Command: pickerCommand}}

	// Keyed by target and chord, so a target bound to two different chords
	// produces two rows rather than one that hides the disagreement.
	type key struct {
		target string
		chord  Chord
	}
	at := map[key]int{}
	chordsPerTarget := map[string]map[Chord]bool{}
	targetsPerChord := map[Chord]map[string]bool{}

	// The picker holds a chord like any target does. Left out of these maps, a
	// target that declares the trigger key produces two rows on one chord -
	// the picker active and the target stale - with neither marked, which is
	// the disagreement this marking exists to name.
	chordsPerTarget[PickerTarget] = map[Chord]bool{trigger: true}
	targetsPerChord[trigger] = map[string]bool{PickerTarget: true}

	for _, p := range projects {
		for _, t := range p.Targets {
			if t.Key == "" {
				continue
			}
			ch, err := ParseChord(t.Key)
			if err != nil {
				// A key that does not parse refuses its target at load
				// (decisions.md D85), and the target is still here carrying
				// it. There is no chord to want, and `revier doctor` is
				// where that refusal is read.
				continue
			}
			k := key{string(t.Name), ch}
			if i, ok := at[k]; ok {
				rows[i].Projects = append(rows[i].Projects, p.Name)
				continue
			}
			at[k] = len(rows)
			rows = append(rows, KeyRow{
				Chord:    ch,
				Target:   string(t.Name),
				Command:  targetCommand(t.Name),
				Projects: []revier.ProjectName{p.Name},
			})
			if chordsPerTarget[string(t.Name)] == nil {
				chordsPerTarget[string(t.Name)] = map[Chord]bool{}
			}
			chordsPerTarget[string(t.Name)][ch] = true
			if targetsPerChord[ch] == nil {
				targetsPerChord[ch] = map[string]bool{}
			}
			targetsPerChord[ch][string(t.Name)] = true
		}
	}

	// Both directions are a disagreement, and both mislead unmarked. One
	// target on two chords prints two rows that look like two targets; two
	// targets on one chord prints one row active and one stale, where fixing
	// the stale one breaks the working one.
	for i, r := range rows {
		if len(chordsPerTarget[r.Target]) > 1 || len(targetsPerChord[r.Chord]) > 1 {
			rows[i].Conflict = true
		}
	}

	// The picker first, then targets by name, then by chord: the picker is
	// the key a user checks first, and the rest has to be stable across runs.
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if (a.Target == PickerTarget) != (b.Target == PickerTarget) {
			return a.Target == PickerTarget
		}
		if a.Target != b.Target {
			return a.Target < b.Target
		}
		return a.Chord < b.Chord
	})
	return rows
}

// classify decides what a wanted chord is doing, from everything the desktop
// holds on it.
//
// A switched-on custom shortcut wins, because that is what fires on a press.
// Only when none exists do the states a user cannot see from the settings UI
// get reported: revier's own shortcut left switched off, or a desktop default
// sitting on the chord. The one exception is a desktop default beside
// revier's own shortcut when that is right: both fire, so the chord is not
// revier's yet.
func classify(want KeyRow, holders []revier.Binding) KeyRow {
	// Every switched-on shortcut on the chord fires, not just the first one
	// the desktop happens to list - which is why README's step 3 asks a user
	// to switch the old bindings off. Reporting one holder and dropping the
	// rest would say "active" for a chord that also runs somebody else's
	// command, and that is the one thing this command exists to catch.
	var live []revier.Binding
	for _, b := range holders {
		if b.Source == revier.BindingCustom && b.Enabled {
			live = append(live, b)
		}
	}
	if len(live) > 0 {
		// The holder that decides the status is the one a user has to act on:
		// somebody else's beats revier's own, because revier's is the half
		// that is already right.
		decides := live[0]
		labels := make([]string, 0, len(live))
		for _, b := range live {
			if !owned(b) && owned(decides) {
				decides = b
			}
			labels = append(labels, b.Label)
		}
		builtins := builtinNames(holders)
		switch {
		case !owned(decides):
			want.Status = KeyTaken
		case decides.Command != want.Command:
			want.Status = KeyStale
		case len(builtins) > 0:
			// revier's shortcut is right, and a desktop default fires on the
			// same press. Active would promise a key install still has to
			// clear with --force.
			want.Status = KeyBuiltin
			labels = append(labels, builtins...)
		default:
			want.Status = KeyActive
		}
		want.HeldBy = strings.Join(labels, ", ")
		return want
	}
	for _, b := range holders {
		if owned(b) {
			want.Status, want.HeldBy = KeyInert, b.Label
			return want
		}
	}
	for _, b := range holders {
		if b.Source == revier.BindingBuiltin {
			want.Status, want.HeldBy = KeyBuiltin, b.Where+" "+b.Label
			return want
		}
	}
	want.Status = KeyFree
	return want
}

// builtinNames names the desktop defaults on a chord, each by its setting.
func builtinNames(holders []revier.Binding) []string {
	var out []string
	for _, b := range holders {
		if b.Source == revier.BindingBuiltin {
			out = append(out, b.Where+" "+b.Label)
		}
	}
	return out
}

// orphans are revier's own shortcuts on chords revier no longer wants: what a
// rename or a deleted target leaves behind. Without this block they are
// invisible, and they still fire.
func orphans(bindings []revier.Binding, wanted []KeyRow) []KeyRow {
	want := map[Chord]bool{}
	for _, w := range wanted {
		want[w.Chord] = true
	}
	var out []KeyRow
	for _, b := range bindings {
		if !owned(b) {
			continue
		}
		ch, err := ParseChord(b.Chord)
		if err != nil || want[ch] {
			continue
		}
		status := KeyActive
		if !b.Enabled {
			status = KeyInert
		}
		out = append(out, KeyRow{
			Chord: ch, Status: status, HeldBy: b.Label, Command: b.Command,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Chord < out[j].Chord })
	return out
}
