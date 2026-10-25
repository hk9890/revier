package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/hk9890/revier/internal/core"
)

const termUsage = `revier term - the terminal keys revier wants, in the terminal's own configuration

usage:
  revier term status [--json]              what the terminal holds of them now
  revier term install [-n] [--json]        write them
  revier term uninstall [-n] [--json]      remove what revier wrote

The keys are [terminal] shell_key and agent_key, and the key of each
[[agent]] in config.toml. For kitty, install writes revier.conf in kitty's
configuration directory and one include of it at the top of kitty.conf, so a
map of yours below it wins. A key a desktop key of revier's is on is left
out: the desktop takes the press first.

  -n, --dry-run  print what would happen and change nothing
  --json   print the report, or the plan and what became of it, as JSON
`

func cmdTerm(ctx context.Context, a *app, args []string) error {
	sub := ""
	if len(args) > 0 {
		sub, args = args[0], args[1:]
	}
	switch sub {
	case "", "help", "--help", "-h":
		_, _ = fmt.Fprint(a.out, termUsage)
		return nil
	case "status":
		// status is given the reader, so nothing on its path can write.
		a.core.TermKeys = termKeys()
		return cmdTermStatus(ctx, a, args)
	case "install", "uninstall":
		a.core.TermKeys = termKeyWriter()
		return cmdTermApply(ctx, a, sub, args)
	default:
		fmt.Fprint(os.Stderr, termUsage)
		return fmt.Errorf("unknown term command %q", sub)
	}
}

func cmdTermStatus(ctx context.Context, a *app, args []string) error {
	fs := flag.NewFlagSet("term status", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "emit the report as JSON")
	if _, err := parseArgs(fs, args); err != nil {
		return err
	}
	trigger, err := a.cfg.TriggerKey()
	if err != nil {
		return err
	}
	report, err := a.core.TermKeyReport(ctx, a.cfg.TermKeys(), a.projects, trigger)
	if err != nil {
		return err
	}
	if *asJSON {
		enc := json.NewEncoder(a.out)
		enc.SetIndent("", "  ")
		return enc.Encode(report)
	}
	printTermReport(a.out, report)
	return nil
}

func printTermReport(w io.Writer, r core.TermReport) {
	_, _ = fmt.Fprintf(w, "terminal: %s\n", r.Terminal)
	_, _ = fmt.Fprintf(w, "%s: %s\n", r.File, termFileNote(r.FileState))
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
func printShadowHint(w io.Writer, r core.TermReport) {
	for _, row := range r.Rows {
		for _, s := range row.Shadowed {
			if s.Condition == "" {
				_, _ = fmt.Fprintf(w, "\n  A map of yours in %s wins over revier's in every window: delete it to use revier's key.\n", filepath.Base(r.Config))
				return
			}
		}
	}
}

func termFileNote(s core.TermFileState) string {
	switch s {
	case core.TermFileCurrent:
		return "current"
	case core.TermFileStale:
		return "stale: it holds other keys than config.toml declares; `revier term install` rewrites it"
	}
	return "absent"
}

func termIncludeNote(r core.TermReport) string {
	switch {
	case !r.ConfigExists:
		return "absent"
	case r.Include == core.TermIncludeAbsent:
		return "does not include revier.conf, so the terminal reads none of these keys"
	case r.Include == core.TermIncludeLate:
		return fmt.Sprintf("includes revier.conf at line %d, after a map of yours on one of these keys: revier's map wins over it, a conditional one too. Move the include to the top", r.IncludeLine)
	case r.IncludeOwned:
		return fmt.Sprintf("includes revier.conf at line %d", r.IncludeLine)
	}
	return fmt.Sprintf("includes revier.conf at line %d, a line of yours", r.IncludeLine)
}

// termRowNote is why a key is not what it looks like: a desktop key on it,
// or a map of the user's that wins over revier's.
func termRowNote(r core.TermReport, row core.TermRow) string {
	if row.Status == core.TermRefused {
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

// cmdTermApply is install and uninstall: build the plan, apply it unless this
// is a dry run, print what happened.
func cmdTermApply(ctx context.Context, a *app, sub string, args []string) error {
	fs := flag.NewFlagSet("term "+sub, flag.ContinueOnError)
	var dryRun bool
	fs.BoolVar(&dryRun, "dry-run", false, "print what would happen and change nothing")
	fs.BoolVar(&dryRun, "n", false, "print what would happen and change nothing (shorthand)")
	asJSON := fs.Bool("json", false, "emit the plan as JSON")
	if _, err := parseArgs(fs, args); err != nil {
		return err
	}
	trigger, err := a.cfg.TriggerKey()
	if err != nil {
		return err
	}
	planFor := a.core.PlanInstallTermKeys
	if sub == "uninstall" {
		planFor = a.core.PlanUninstallTermKeys
	}
	plan, err := planFor(ctx, a.cfg.TermKeys(), a.projects, trigger)
	if err != nil {
		return err
	}
	if !dryRun {
		plan = a.core.ApplyTermKeys(ctx, plan)
		switch {
		case plan.Err != "":
			slog.Error("term "+sub, "file", plan.File, "include", plan.Include, "err", plan.Err)
		case plan.Done:
			slog.Info("term "+sub, "file", plan.File, "include", plan.Include, "path", plan.Report.File)
		}
	}

	if *asJSON {
		enc := json.NewEncoder(a.out)
		enc.SetIndent("", "  ")
		if err := enc.Encode(plan); err != nil {
			return err
		}
	} else {
		printTermPlan(a.out, plan, sub, dryRun)
	}

	if plan.Err != "" {
		return errKeysIncomplete
	}
	if sub == "install" {
		for _, row := range plan.Report.Rows {
			if row.Status == core.TermRefused {
				return errKeysIncomplete
			}
		}
	}
	return nil
}

func printTermPlan(w io.Writer, plan core.TermPlan, sub string, dryRun bool) {
	_, _ = fmt.Fprintf(w, "=== revier term %s ===\n\n", sub)
	if dryRun {
		_, _ = fmt.Fprint(w, "  dry run: nothing is changed\n\n")
	}
	r := plan.Report
	_, _ = fmt.Fprintf(w, "  %s %s: %s\n", termMark(plan.File, plan, dryRun), r.File, termFileAction(plan.File, dryRun))
	_, _ = fmt.Fprintf(w, "  %s %s: %s\n", termMark(plan.Include, plan, dryRun), r.Config, termIncludeAction(plan, dryRun))
	if plan.Err != "" {
		_, _ = fmt.Fprintf(w, "  ! failed: %s\n", plan.Err)
	}

	if sub == "install" {
		_, _ = fmt.Fprintln(w)
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		for _, row := range r.Rows {
			mark := "+"
			if row.Status == core.TermRefused {
				mark = "!"
			}
			_, _ = fmt.Fprintf(tw, "  %s %s\t%s\t%s\n", mark, row.Chord, row.Opens, dash(termRowNote(r, row)))
		}
		_ = tw.Flush()
		printShadowHint(w, r)
	}

	switch {
	case plan.Err != "" || !plan.Writes():
	case plan.Include == core.TermCreate:
		_, _ = fmt.Fprintf(w, "\n  %s is new: restart kitty, which watches only a configuration that was there when it started.\n", filepath.Base(r.Config))
	default:
		_, _ = fmt.Fprintln(w, "\n  kitty reads the change by itself; revier sends it no signal.")
	}
}

// termMark is the glyph in front of a file's line: > for a change, - for
// none, ! for a change that failed.
func termMark(a core.TermAction, plan core.TermPlan, dryRun bool) string {
	switch {
	case a == core.TermOK || a == core.TermAbsent || a == core.TermKept:
		return "-"
	case plan.Err != "":
		return "!"
	case plan.Done || dryRun:
		return ">"
	}
	return "-"
}

func termFileAction(a core.TermAction, dryRun bool) string {
	switch a {
	case core.TermOK:
		return "ok"
	case core.TermAbsent:
		return "not there"
	case core.TermCreate:
		return wouldBe("created", dryRun)
	case core.TermUpdate:
		return wouldBe("rewritten", dryRun)
	}
	return wouldBe("removed", dryRun)
}

func termIncludeAction(plan core.TermPlan, dryRun bool) string {
	switch plan.Include {
	case core.TermOK:
		return "includes revier.conf already"
	case core.TermAbsent:
		return "includes no revier.conf"
	case core.TermKept:
		return fmt.Sprintf("the include at line %d is yours: it stays", plan.Report.IncludeLine)
	case core.TermCreate:
		return wouldBe("created, with the include", dryRun)
	case core.TermAdd:
		return "include " + wouldBe("added at the top", dryRun)
	}
	return "revier's include " + wouldBe("removed", dryRun)
}
