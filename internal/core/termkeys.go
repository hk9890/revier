package core

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/hk9890/revier/pkg/revier"
)

// The terminal's keys are revier's second key store, beside the desktop's
// (decisions.md D147). revier owns its key file in the terminal's
// configuration whole, so there is nothing to take over and no --force: a key
// config.toml declares is written. What this file decides is which keys those
// are, which one the desktop would take first, and what is in the way.

// ErrNoTermKeys means this machine has no terminal revier writes keys for.
var ErrNoTermKeys = errors.New("no terminal revier writes keys for on this machine")

// ErrNoTermKeyWriter means the terminal's keys can be read but not written.
var ErrNoTermKeyWriter = errors.New("this terminal's keys cannot be changed by revier")

// TermKey is one terminal key config.toml declares, its chord parsed: the
// shell's, the default agent's, or a named agent's.
type TermKey struct {
	Chord Chord
	// Shell is the key that opens a shell. Agent names the named agent the
	// key opens; empty, with Shell false, it is the default agent.
	Shell bool
	Agent string
	// Label is what config.toml calls the key, as a problem names it:
	// terminal.agent_key, or agent "dtop" key.
	Label string
}

// Args is the revier command the key runs, after "revier".
func (k TermKey) Args() []string {
	switch {
	case k.Shell:
		return []string{"shell", "new"}
	case k.Agent != "":
		return []string{"agent", "new", k.Agent}
	}
	return []string{"agent", "new"}
}

// Opens names what the key opens, for the report.
func (k TermKey) Opens() string {
	switch {
	case k.Shell:
		return "shell"
	case k.Agent != "":
		return "agent " + k.Agent
	}
	return "agent"
}

// TermKeyStatus is what the terminal does with one key revier wants.
type TermKeyStatus string

const (
	// TermKeyWritten: revier's key file holds the key as config.toml wants it.
	TermKeyWritten TermKeyStatus = "written"
	// TermKeyMissing: revier's key file does not hold it, or runs another
	// command on it. `revier term install` writes it.
	TermKeyMissing TermKeyStatus = "missing"
	// TermKeyRefused: a desktop key revier wants is on the same chord, and the
	// desktop takes a press before the terminal does (decisions.md D110), so
	// revier writes no map of it.
	TermKeyRefused TermKeyStatus = "refused"
)

// TermKeyFileState is what revier's key file is, against what config.toml
// wants.
type TermKeyFileState string

const (
	TermKeyFileCurrent TermKeyFileState = "current"
	TermKeyFileStale   TermKeyFileState = "stale"
	TermKeyFileAbsent  TermKeyFileState = "absent"
)

// TermKeyInclude is where the terminal's main configuration includes revier's
// key file.
type TermKeyInclude string

const (
	// TermKeyIncludeAbsent: no line includes it, so the terminal reads none of
	// revier's keys.
	TermKeyIncludeAbsent TermKeyInclude = "absent"
	// TermKeyIncludeTop: it is included before every map of a key revier
	// writes, so the user's own maps of those keys win.
	TermKeyIncludeTop TermKeyInclude = "top"
	// TermKeyIncludeLate: it is included after a map of a key revier writes, so
	// revier's map wins over the user's, a conditional one too.
	TermKeyIncludeLate TermKeyInclude = "late"
)

// TermKeyShadow is a map in the terminal's main configuration that wins over
// revier's on the same key: it comes after the include.
type TermKeyShadow struct {
	Line int `json:"line"`
	// Condition limits the map to some windows; empty, it holds everywhere
	// and revier's map never runs.
	Condition string `json:"condition,omitempty"`
}

// TermKeyRow is one key revier wants in the terminal.
type TermKeyRow struct {
	Chord  Chord         `json:"chord"`
	Opens  string        `json:"opens"`
	Args   []string      `json:"args"`
	Status TermKeyStatus `json:"status"`
	// Refused names the desktop key on the chord, when Status is refused.
	Refused string `json:"refused,omitempty"`
	// Shadowed is every map of the main configuration that wins over
	// revier's on this chord, with the include where it is or, while there
	// is none, where install puts it.
	Shadowed []TermKeyShadow `json:"shadowed,omitempty"`
}

// TermKeyReport is the answer to "which keys does the terminal hold for
// revier".
type TermKeyReport struct {
	Terminal string `json:"terminal"`
	// File is revier's key file, and FileState what it holds.
	File      string           `json:"file"`
	FileState TermKeyFileState `json:"file_state"`
	// Config is the terminal's main configuration.
	Config       string `json:"config"`
	ConfigExists bool   `json:"config_exists"`
	// FileDangling and ConfigDangling name the link on the way to File and
	// to Config that points at nothing, when there is one: no install or
	// uninstall runs while either is set.
	FileDangling   string `json:"file_dangling,omitempty"`
	ConfigDangling string `json:"config_dangling,omitempty"`
	// Include is where Config includes File, IncludeLine the line, and
	// IncludeOwned whether the line is revier's or the user's.
	Include      TermKeyInclude `json:"include"`
	IncludeLine  int            `json:"include_line,omitempty"`
	IncludeOwned bool           `json:"include_owned,omitempty"`
	Rows         []TermKeyRow   `json:"rows"`
}

// TermKeyReport reads the terminal and reports every key in keys: the ones
// config.toml declares. projects and trigger are the desktop keys revier
// wants, which a terminal key on the same chord loses to.
//
// It reads. Nothing here changes the terminal's configuration.
func (c *Core) TermKeyReport(ctx context.Context, keys []TermKey, projects []Project, trigger Chord) (TermKeyReport, error) {
	if c.TermKeys == nil {
		return TermKeyReport{}, ErrNoTermKeys
	}
	state, err := c.TermKeys.Read(ctx)
	if err != nil {
		return TermKeyReport{}, err
	}
	return termReport(c.TermKeys.Name(), state, wantedTermRows(keys, projects, trigger)), nil
}

// wantedTermRows is one row per key, in config.toml's order, with the ones a
// desktop key is on refused. The rest have no status yet: that is what the
// terminal holds.
func wantedTermRows(keys []TermKey, projects []Project, trigger Chord) []TermKeyRow {
	desktop := map[Chord]string{}
	for _, r := range wantedChords(projects, trigger) {
		if _, ok := desktop[r.Chord]; ok {
			continue
		}
		if r.Target == PopupTarget {
			desktop[r.Chord] = "the desktop key that opens revier ([ui] trigger_key)"
			continue
		}
		names := make([]string, len(r.Projects))
		for i, p := range r.Projects {
			names[i] = string(p)
		}
		desktop[r.Chord] = fmt.Sprintf("the desktop key of target %q in %s", r.Target, strings.Join(names, ", "))
	}
	rows := make([]TermKeyRow, 0, len(keys))
	for _, k := range keys {
		row := TermKeyRow{Chord: k.Chord, Opens: k.Opens(), Args: k.Args()}
		if held, ok := desktop[k.Chord]; ok {
			row.Status, row.Refused = TermKeyRefused, held
		}
		rows = append(rows, row)
	}
	return rows
}

// InstalledTermKeys is the keys of keys that `revier term install` writes:
// every one no desktop key is on.
func InstalledTermKeys(keys []TermKey, projects []Project, trigger Chord) []TermKey {
	rows := wantedTermRows(keys, projects, trigger)
	var out []TermKey
	for i, k := range keys {
		if rows[i].Status != TermKeyRefused {
			out = append(out, k)
		}
	}
	return out
}

// termReport is the whole of the classification, over a state already read
// and the rows wantedTermRows made.
func termReport(name string, state revier.TermKeyState, wanted []TermKeyRow) TermKeyReport {
	report := TermKeyReport{
		Terminal:       name,
		File:           state.File,
		Config:         state.Config,
		ConfigExists:   state.ConfigExists,
		FileDangling:   state.FileDangling,
		ConfigDangling: state.ConfigDangling,
		Include:        TermKeyIncludeAbsent,
		IncludeLine:    state.Include,
		IncludeOwned:   state.IncludeOwned,
	}

	held := map[Chord][]string{}
	var have []revier.TermBinding
	for _, b := range state.Bindings {
		ch, err := ParseChord(b.Chord)
		if err != nil {
			// Not a key revier wrote: the file is not what install writes.
			have = append(have, b)
			continue
		}
		held[ch] = b.Args
		have = append(have, revier.TermBinding{Chord: string(ch), Args: b.Args})
	}

	maps := map[Chord][]revier.TermMap{}
	for _, m := range state.Maps {
		if ch, err := ParseChord(m.Chord); err == nil {
			maps[ch] = append(maps[ch], m)
		}
	}

	write := termBindings(wanted)
	report.FileState = TermKeyFileStale
	switch {
	case !state.FileExists:
		report.FileState = TermKeyFileAbsent
	case slices.EqualFunc(have, write, func(a, b revier.TermBinding) bool {
		return a.Chord == b.Chord && slices.Equal(a.Args, b.Args)
	}):
		report.FileState = TermKeyFileCurrent
	}

	if state.Include > 0 {
		report.Include = TermKeyIncludeTop
	}
	for _, row := range wanted {
		if row.Status != TermKeyRefused {
			row.Status = TermKeyMissing
			if args, ok := held[row.Chord]; ok && slices.Equal(args, row.Args) {
				row.Status = TermKeyWritten
			}
			for _, m := range maps[row.Chord] {
				if m.Line > state.Include {
					row.Shadowed = append(row.Shadowed, TermKeyShadow{Line: m.Line, Condition: m.Condition})
				} else if state.Include > 0 {
					report.Include = TermKeyIncludeLate
				}
			}
		}
		report.Rows = append(report.Rows, row)
	}
	return report
}

// termBindings is what revier's key file holds for the rows: every key the
// desktop does not take, in order.
func termBindings(rows []TermKeyRow) []revier.TermBinding {
	out := []revier.TermBinding{}
	for _, r := range rows {
		if r.Status != TermKeyRefused {
			out = append(out, revier.TermBinding{Chord: string(r.Chord), Args: r.Args})
		}
	}
	return out
}

// TermKeyAction is what installing or removing does to one of the two files.
type TermKeyAction string

const (
	// TermKeyOK: the file is what install would make it.
	TermKeyOK TermKeyAction = "ok"
	// TermKeyCreate: the file is not there, and install writes it. For the
	// main configuration, the terminal must be restarted to watch it.
	TermKeyCreate TermKeyAction = "create"
	// TermKeyUpdate: revier's key file is there and holds other keys.
	TermKeyUpdate TermKeyAction = "update"
	// TermKeyAdd: the main configuration is there, and install adds the
	// include at its top.
	TermKeyAdd TermKeyAction = "add"
	// TermKeyRemove: uninstalling, and revier's file or include is there.
	TermKeyRemove TermKeyAction = "remove"
	// TermKeyAbsent: uninstalling, and there is nothing of revier's to remove.
	TermKeyAbsent TermKeyAction = "absent"
	// TermKeyKept: uninstalling, and the include is the user's: it stays.
	TermKeyKept TermKeyAction = "kept"
)

// Writes reports whether the action changes the file.
func (a TermKeyAction) Writes() bool { return a != TermKeyOK && a != TermKeyAbsent && a != TermKeyKept }

// TermKeyPlan is what a run of `revier term install` or `uninstall` does,
// before it does it. The same value is what the command prints afterwards,
// with Done or Err filled and the report read again.
type TermKeyPlan struct {
	Report TermKeyReport `json:"report"`
	// File is the action on revier's key file, Include on the include line
	// in the main configuration.
	File    TermKeyAction `json:"file"`
	Include TermKeyAction `json:"include"`
	// Refused is why the plan writes nothing at all: a link to nothing on
	// the way to one of the files. Writing the other one alone would leave
	// half an install.
	Refused string `json:"refused,omitempty"`
	// FileDone and IncludeDone say which of the two actions ran, and Done
	// that the whole plan did. A failed run can have done one of them.
	FileDone    bool   `json:"file_done,omitempty"`
	IncludeDone bool   `json:"include_done,omitempty"`
	Done        bool   `json:"done,omitempty"`
	Err         string `json:"error,omitempty"`

	wanted    []TermKeyRow
	uninstall bool
}

// Writes reports whether the plan changes a file.
func (p TermKeyPlan) Writes() bool { return p.File.Writes() || p.Include.Writes() }

// refusal is why no install or uninstall runs: every link to nothing on the
// way to the two files. Empty when there is none.
func refusal(r TermKeyReport) string {
	var links []string
	for _, l := range []string{r.FileDangling, r.ConfigDangling} {
		if l != "" && !slices.Contains(links, l) {
			links = append(links, l)
		}
	}
	switch len(links) {
	case 0:
		return ""
	case 1:
		return links[0] + " is a link to nothing"
	}
	return strings.Join(links, " and ") + " are links to nothing"
}

// PlanInstallTermKeys works out what writing the terminal keys would do, and
// changes nothing. A key the desktop takes is left out of the file; the rest
// are written.
func (c *Core) PlanInstallTermKeys(ctx context.Context, keys []TermKey, projects []Project, trigger Chord) (TermKeyPlan, error) {
	report, wanted, err := c.termState(ctx, keys, projects, trigger)
	if err != nil {
		return TermKeyPlan{}, err
	}
	plan := TermKeyPlan{Report: report, wanted: wanted, Refused: refusal(report)}
	switch report.FileState {
	case TermKeyFileCurrent:
		plan.File = TermKeyOK
	case TermKeyFileStale:
		plan.File = TermKeyUpdate
	default:
		plan.File = TermKeyCreate
	}
	switch {
	case report.Include != TermKeyIncludeAbsent:
		plan.Include = TermKeyOK
	case report.ConfigExists:
		plan.Include = TermKeyAdd
	default:
		plan.Include = TermKeyCreate
	}
	return plan, nil
}

// PlanUninstallTermKeys works out what removing the terminal keys would do.
// Only what revier wrote goes: an include the user wrote stays.
func (c *Core) PlanUninstallTermKeys(ctx context.Context, keys []TermKey, projects []Project, trigger Chord) (TermKeyPlan, error) {
	report, wanted, err := c.termState(ctx, keys, projects, trigger)
	if err != nil {
		return TermKeyPlan{}, err
	}
	plan := TermKeyPlan{Report: report, wanted: wanted, uninstall: true, File: TermKeyAbsent, Include: TermKeyAbsent, Refused: refusal(report)}
	if report.FileState != TermKeyFileAbsent {
		plan.File = TermKeyRemove
	}
	switch {
	case report.Include == TermKeyIncludeAbsent:
	case report.IncludeOwned:
		plan.Include = TermKeyRemove
	default:
		plan.Include = TermKeyKept
	}
	return plan, nil
}

func (c *Core) termState(ctx context.Context, keys []TermKey, projects []Project, trigger Chord) (TermKeyReport, []TermKeyRow, error) {
	if c.TermKeys == nil {
		return TermKeyReport{}, nil, ErrNoTermKeys
	}
	state, err := c.TermKeys.Read(ctx)
	if err != nil {
		return TermKeyReport{}, nil, err
	}
	wanted := wantedTermRows(keys, projects, trigger)
	return termReport(c.TermKeys.Name(), state, wanted), wanted, nil
}

// ApplyTermKeys carries out a plan and returns it marked, its report read
// again: what the files hold now, and the shadowing maps at the lines they
// are on now. A refused plan fails with its refusal and writes nothing; a
// plan that writes nothing is returned as it is.
func (c *Core) ApplyTermKeys(ctx context.Context, plan TermKeyPlan) TermKeyPlan {
	if plan.Refused != "" {
		plan.Err = plan.Refused
		return plan
	}
	if !plan.Writes() {
		return plan
	}
	w, ok := c.TermKeys.(revier.TermKeyWriter)
	if !ok {
		plan.Err = ErrNoTermKeyWriter.Error()
		return plan
	}
	var err error
	if plan.uninstall {
		err = w.Remove(ctx)
	} else {
		err = w.Write(ctx, termBindings(plan.wanted))
	}
	if err != nil {
		plan.Err = err.Error()
	}
	state, readErr := w.Read(ctx)
	if readErr == nil {
		plan.Report = termReport(w.Name(), state, plan.wanted)
	}
	if err == nil {
		plan.Done, plan.FileDone, plan.IncludeDone = true, plan.File.Writes(), plan.Include.Writes()
		return plan
	}
	if readErr != nil {
		return plan
	}
	// The run failed part way: the files read again say which step ran.
	if plan.uninstall {
		plan.FileDone = plan.File.Writes() && plan.Report.FileState == TermKeyFileAbsent
		plan.IncludeDone = plan.Include.Writes() && plan.Report.Include == TermKeyIncludeAbsent
	} else {
		plan.FileDone = plan.File.Writes() && plan.Report.FileState == TermKeyFileCurrent
		plan.IncludeDone = plan.Include.Writes() && plan.Report.Include != TermKeyIncludeAbsent
	}
	return plan
}
