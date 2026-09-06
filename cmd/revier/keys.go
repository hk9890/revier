package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/hk9890/revier/internal/core"
)

const keysUsage = `revier keys - the desktop chords revier wants, and who holds each one

usage:
  revier keys status [--json]
`

// keyBinderWait bounds the desktop probe. It is short because nothing about
// this command is worth waiting on: gsettings answers in milliseconds, and a
// dconf service that does not answer at all should report that rather than
// hold the terminal.
const keyBinderWait = 3 * time.Second

func cmdKeys(ctx context.Context, a *app, args []string) error {
	sub := ""
	if len(args) > 0 {
		sub, args = args[0], args[1:]
	}
	switch sub {
	case "", "help", "--help", "-h":
		fmt.Print(keysUsage)
		return nil
	case "status":
		// The desktop is probed here and not in newApp: it costs a gsettings
		// process, and every other command - `revier go` on a keypress above
		// all - would pay for a value only this one reads.
		probe, cancel := context.WithTimeout(ctx, keyBinderWait)
		defer cancel()
		a.core.KeyBinder = selectKeyBinder(probe, keyBinders())
		return cmdKeysStatus(ctx, a, args)
	default:
		fmt.Fprint(os.Stderr, keysUsage)
		return fmt.Errorf("unknown keys command %q", sub)
	}
}

// cmdKeysStatus reads and prints. Nothing on this path writes a binding: a
// command an agent may run against the user's live desktop has to be one that
// cannot change it.
func cmdKeysStatus(ctx context.Context, a *app, args []string) error {
	fs := flag.NewFlagSet("keys status", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "emit the report as JSON")
	if _, err := parseArgs(fs, args); err != nil {
		return err
	}

	trigger, err := a.cfg.TriggerKey()
	if err != nil {
		return err
	}
	report, err := a.core.Keys(ctx, a.projects, trigger)
	switch {
	case errors.Is(err, core.ErrNoKeyBinder):
		// A machine with no desktop is a normal outcome, not a failure
		// (docs/design/decisions.md D26). Reporting it as an error would
		// leave a --json caller with an exit status and no JSON at all.
		report = core.KeyReport{Rows: []core.KeyRow{}}
		if !*asJSON {
			fmt.Fprintln(os.Stderr, "revier:", err)
		}
	case err != nil:
		return err
	}

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(report)
	}
	printKeys(os.Stdout, report)
	return nil
}

func printKeys(w io.Writer, report core.KeyReport) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "KEY\tTARGET\tSTATUS\tHELD BY")
	for _, r := range report.Rows {
		target := r.Target
		if r.Conflict {
			// The rows disagree with each other. Unmarked, one target on two
			// chords reads as two targets, and two targets on one chord reads
			// as a stale binding worth repairing.
			target += " (conflict)"
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", r.Chord, target, r.Status, dash(r.HeldBy))
	}
	_ = tw.Flush() // stdout; a write failure has nowhere left to be reported

	printOrphans(w, report.Orphaned)
	printConflicts(w, report.Rows)
}

// printOrphans is a table of its own. Its columns are not the ones above - an
// orphan has no target, and what a user needs is the shortcut's name, to find
// it in the desktop's settings, and the command, to see what it still does.
func printOrphans(w io.Writer, orphans []core.KeyRow) {
	if len(orphans) == 0 {
		return
	}
	_, _ = fmt.Fprintln(w, "\nORPHANED  revier holds these and no longer wants them")
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "KEY\tSTATUS\tNAME\tCOMMAND")
	for _, r := range orphans {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", r.Chord, r.Status, dash(r.HeldBy), r.Command)
	}
	_ = tw.Flush()
}

// printConflicts explains each disagreement once, in the direction it runs.
// The marks in the table say a row is in one; only this says which.
func printConflicts(w io.Writer, rows []core.KeyRow) {
	byTarget := map[string][]core.KeyRow{}
	byChord := map[core.Chord][]core.KeyRow{}
	for _, r := range rows {
		if !r.Conflict {
			continue
		}
		byTarget[r.Target] = append(byTarget[r.Target], r)
		byChord[r.Chord] = append(byChord[r.Chord], r)
	}

	for _, target := range sorted(byTarget) {
		group := byTarget[target]
		if len(group) < 2 {
			continue
		}
		_, _ = fmt.Fprintf(w, "\nconflict: target %q is on more than one key\n", target)
		for _, r := range group {
			_, _ = fmt.Fprintf(w, "  %s asked for by: %s\n", r.Chord, names(r.Projects))
		}
	}
	for _, chord := range sorted(byChord) {
		group := byChord[chord]
		if len(group) < 2 {
			continue
		}
		_, _ = fmt.Fprintf(w, "\nconflict: %s is asked for by more than one target\n", chord)
		for _, r := range group {
			_, _ = fmt.Fprintf(w, "  %s asked for by: %s\n", r.Target, names(r.Projects))
		}
	}
}

func sorted[K ~string, V any](m map[K]V) []K {
	out := make([]K, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func names[T ~string](in []T) string {
	out := make([]string, len(in))
	for i, v := range in {
		out[i] = string(v)
	}
	return strings.Join(out, ", ")
}
