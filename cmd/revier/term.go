package main

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/hk9890/revier/internal/core"
)

func printTermReport(w io.Writer, r core.TermKeyReport) {
	_, _ = fmt.Fprintf(w, "terminal: %s\n", r.Terminal)
	_, _ = fmt.Fprintf(w, "%s: %s\n", r.File, termFileNote(r))
	_, _ = fmt.Fprintf(w, "%s: %s\n\n", r.Config, termIncludeNote(r))

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "KEY\tOPENS\tSTATUS\tNOTE")
	for _, row := range r.Rows {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", row.Chord, row.Opens, row.Status, dash(termRowNote(r, row)))
	}
	_ = tw.Flush() // stdout; a write failure has nowhere left to be reported
	printShadowHint(w, r)
}

// printShadowHint says what to do about a map of the user's that wins over
// revier's in every window: the hand-written map of an older setup is the
// one to expect. A conditional one is left to the user: it wins only where
// they asked it to.
func printShadowHint(w io.Writer, r core.TermKeyReport) {
	for _, row := range r.Rows {
		for _, s := range row.Shadowed {
			if s.Condition == "" {
				_, _ = fmt.Fprintf(w, "\n  A map of yours in %s wins over revier's in every window: delete it to use revier's key.\n", filepath.Base(r.Config))
				return
			}
		}
	}
}

func termFileNote(r core.TermKeyReport) string {
	if r.FileDangling != "" {
		return linkToNothing(r.File, r.FileDangling)
	}
	switch r.FileState {
	case core.TermKeyFileCurrent:
		return "current"
	case core.TermKeyFileStale:
		return "stale: it holds other keys than config.toml declares; `revier setup install` rewrites it"
	}
	return "absent"
}

func termIncludeNote(r core.TermKeyReport) string {
	switch {
	case r.ConfigDangling != "":
		return linkToNothing(r.Config, r.ConfigDangling)
	case !r.ConfigExists:
		return "absent"
	case r.Include == core.TermKeyIncludeAbsent:
		return "does not include revier.conf, so the terminal reads none of these keys"
	case r.Include == core.TermKeyIncludeLate:
		return fmt.Sprintf("includes revier.conf at line %d, after a map of yours on one of these keys: revier's map wins over it, a conditional one too. Move the include to the top", r.IncludeLine)
	case r.IncludeOwned:
		return fmt.Sprintf("includes revier.conf at line %d", r.IncludeLine)
	}
	return fmt.Sprintf("includes revier.conf at line %d, a line of yours", r.IncludeLine)
}

// termRowNote is why a key is not what it looks like: a desktop key on it,
// or a map of the user's that wins over revier's.
func termRowNote(r core.TermKeyReport, row core.TermKeyRow) string {
	if row.Status == core.TermKeyRefused {
		return "refused: " + row.Refused + " is on it, and the desktop takes the press first"
	}
	if len(row.Shadowed) == 0 {
		return ""
	}
	conf := filepath.Base(r.Config)
	at := make([]string, len(row.Shadowed))
	for i, s := range row.Shadowed {
		at[i] = fmt.Sprintf("%s:%d", conf, s.Line)
		if s.Condition != "" {
			at[i] += " " + s.Condition
		}
	}
	return "shadowed by " + strings.Join(at, ", ")
}

// printTermPlan prints the plan, or what became of it. before is the plan as
// it was built: its rows say which keys the run had to write.
func printTermPlan(w io.Writer, before, plan core.TermKeyPlan, sub string, dryRun bool) {
	r := plan.Report
	file := termStep{action: plan.File, done: plan.FileDone, dangling: r.FileDangling}
	include := termStep{action: plan.Include, done: plan.IncludeDone, dangling: r.ConfigDangling, late: r.Include == core.TermKeyIncludeLate}
	_, _ = fmt.Fprintf(w, "  %s %s: %s\n", file.mark(plan, dryRun), r.File, file.fileAction(r.File, plan, dryRun))
	_, _ = fmt.Fprintf(w, "  %s %s: %s\n", include.mark(plan, dryRun), r.Config, include.includeAction(plan, dryRun))
	switch {
	case plan.Refused != "" && dryRun:
		_, _ = fmt.Fprintf(w, "  ! %s would be refused: %s, so it would write neither file\n", sub, plan.Refused)
	case plan.Refused != "":
		_, _ = fmt.Fprintf(w, "  ! refused: %s, so neither file is written\n", plan.Refused)
	case plan.Err != "":
		_, _ = fmt.Fprintf(w, "  ! failed: %s\n", plan.Err)
	}

	if sub == "install" {
		_, _ = fmt.Fprintln(w)
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		for i, row := range r.Rows {
			mark, note := termKeyLine(before.Report.Rows[i], plan, dryRun)
			if more := termRowNote(r, row); more != "" {
				note += "; " + more
			}
			_, _ = fmt.Fprintf(tw, "  %s %s\t%s\t%s\n", mark, row.Chord, row.Opens, note)
		}
		_ = tw.Flush()
		printShadowHint(w, r)
	}

	switch {
	case plan.Err != "" || plan.Refused != "" || !plan.Writes():
	case plan.Include == core.TermKeyCreate:
		_, _ = fmt.Fprintf(w, "\n  %s is new: restart kitty, which watches only a configuration that was there when it started.\n", filepath.Base(r.Config))
	default:
		_, _ = fmt.Fprintln(w, "\n  kitty reads the change by itself; revier sends it no signal.")
	}
}

// termKeyLine is the mark and the note of one key, as the desktop keys mark
// its keys: + for one that was already written, > for one this run writes,
// ! for one refused or not written. before is the row as the plan found it.
func termKeyLine(before core.TermKeyRow, plan core.TermKeyPlan, dryRun bool) (mark, note string) {
	switch {
	case before.Status == core.TermKeyRefused:
		return "!", "refused"
	case before.Status == core.TermKeyWritten:
		return "+", "ok"
	case plan.Refused != "" && dryRun:
		return "!", "would not be written"
	case dryRun:
		return ">", "would be written"
	case plan.FileDone:
		return ">", "written"
	}
	return "!", "not written"
}

// termStep is one of the two files' actions, and what became of it: done,
// or held back by a link to nothing on the way to the file.
type termStep struct {
	action   core.TermKeyAction
	done     bool
	dangling string
	// late is an include that is there, after a map of the user's.
	late bool
}

// runs reports whether the step changes its file on this run, or would on a
// dry run.
func (s termStep) runs(plan core.TermKeyPlan, dryRun bool) bool {
	if dryRun {
		return plan.Refused == "" && s.action.Writes()
	}
	return s.done
}

// mark is the glyph in front of a file's line: > for a change, - for
// none, ! for a change that did not happen and for an include placed after
// a map of the user's.
func (s termStep) mark(plan core.TermKeyPlan, dryRun bool) string {
	switch {
	case s.dangling != "":
		return "!"
	case s.late:
		return "!"
	case !s.action.Writes():
		return "-"
	case s.runs(plan, dryRun):
		return ">"
	}
	return "!"
}

// outcome is what a step that writes says: what it does, what it would do,
// or that it did not.
func (s termStep) outcome(verb string, plan core.TermKeyPlan, dryRun bool) string {
	switch {
	case s.runs(plan, dryRun):
		return wouldBe(verb, dryRun)
	case dryRun:
		return "would not be " + verb
	}
	return "not " + verb
}

// linkToNothing says that path is, or is in, a link that points at nothing:
// install and uninstall refuse to run until it points somewhere.
func linkToNothing(path, link string) string {
	if link == path {
		return "is a link to nothing"
	}
	return "in " + link + ", a link to nothing"
}

func (s termStep) fileAction(path string, plan core.TermKeyPlan, dryRun bool) string {
	if s.dangling != "" {
		return linkToNothing(path, s.dangling)
	}
	switch s.action {
	case core.TermKeyOK:
		return "ok"
	case core.TermKeyAbsent:
		return "not there"
	case core.TermKeyCreate:
		return s.outcome("created", plan, dryRun)
	case core.TermKeyUpdate:
		return s.outcome("rewritten", plan, dryRun)
	}
	return s.outcome("removed", plan, dryRun)
}

func (s termStep) includeAction(plan core.TermKeyPlan, dryRun bool) string {
	if s.dangling != "" {
		return linkToNothing(plan.Report.Config, s.dangling)
	}
	switch s.action {
	case core.TermKeyOK:
		if s.late {
			return termIncludeNote(plan.Report)
		}
		return "includes revier.conf already"
	case core.TermKeyAbsent:
		return "includes no revier.conf"
	case core.TermKeyKept:
		return fmt.Sprintf("the include at line %d is yours: it stays", plan.Report.IncludeLine)
	case core.TermKeyCreate:
		if s.runs(plan, dryRun) {
			return wouldBe("created, with the include", dryRun)
		}
		return s.outcome("created", plan, dryRun)
	case core.TermKeyAdd:
		return "include " + s.outcome("added at the top", plan, dryRun)
	}
	return "revier's include " + s.outcome("removed", plan, dryRun)
}
