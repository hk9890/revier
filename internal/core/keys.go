package core

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/hk9890/revier/pkg/revier"
)

// ErrNoKeyBinder means this machine has no desktop revier can read shortcuts
// from. It is a normal outcome on a headless box, not a failure.
var ErrNoKeyBinder = errors.New("no desktop keybinding support on this machine")

// PickerTarget is the row for the chord that opens revier itself. It is not a
// target - nothing is run-or-raised, the surface comes up - so it has a name
// of its own rather than borrowing a target's.
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

// The commands revier binds. They match contrib/gnome/revier-keybindings.dconf:
// a login shell, so the desktop finds revier on the same PATH a terminal has.
const pickerCommand = `sh -lc "revier-popup"`

func targetCommand(name revier.TargetName) string {
	return fmt.Sprintf(`sh -lc "revier-go %s"`, name)
}

// ownedCommand reports whether a shortcut runs revier. Ownership is read from
// the command and not from where the desktop filed the shortcut, because a
// user who bound `revier-go editor` by hand owns the same key revier does, and
// reporting that as somebody else's would be a lie.
func ownedCommand(cmd string) bool {
	return strings.Contains(cmd, "revier-popup") || strings.Contains(cmd, "revier-go")
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
	report := KeyReport{Desktop: c.KeyBinder.Name()}

	held := index(bindings)
	wanted := wantedChords(projects, trigger)
	for _, w := range wanted {
		report.Rows = append(report.Rows, classify(w, held[w.Chord]))
	}
	report.Orphaned = orphans(bindings, wanted)
	return report, nil
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

	for _, p := range projects {
		for _, t := range p.Targets {
			if t.Key == "" {
				continue
			}
			ch, err := ParseChord(t.Key)
			if err != nil {
				// config.Load rejects a key that does not parse, so this is
				// unreachable through the CLI. Skipping keeps a hand-built
				// project from breaking the whole report.
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
		}
	}

	for i, r := range rows {
		if len(chordsPerTarget[r.Target]) > 1 {
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
// sitting on the chord.
func classify(want KeyRow, holders []revier.Binding) KeyRow {
	for _, b := range holders {
		if b.Source == revier.BindingCustom && b.Enabled {
			switch {
			case !ownedCommand(b.Command):
				want.Status, want.HeldBy = KeyTaken, b.Label
			case b.Command == want.Command:
				want.Status, want.HeldBy = KeyActive, b.Label
			default:
				want.Status, want.HeldBy = KeyStale, b.Label
			}
			return want
		}
	}
	for _, b := range holders {
		if b.Source == revier.BindingCustom && ownedCommand(b.Command) {
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
		if b.Source != revier.BindingCustom || !ownedCommand(b.Command) {
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
