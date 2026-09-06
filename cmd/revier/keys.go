package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/hk9890/revier/internal/core"
)

const keysUsage = `revier keys - the desktop chords revier wants, and who holds each one

usage:
  revier keys status [--json]
`

func cmdKeys(ctx context.Context, a *app, args []string) error {
	sub := ""
	if len(args) > 0 {
		sub, args = args[0], args[1:]
	}
	switch sub {
	case "status":
		return cmdKeysStatus(ctx, a, args)
	case "help", "--help", "-h":
		fmt.Print(keysUsage)
		return nil
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
	if err != nil {
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
			// The projects do not agree on this target's key, so the report
			// carries a row per chord. Without the mark the two rows look
			// like two different targets.
			target += " (conflict)"
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", r.Chord, target, r.Status, dash(r.HeldBy))
	}
	if len(report.Orphaned) > 0 {
		_, _ = fmt.Fprintln(tw, "\t\t\t")
		_, _ = fmt.Fprintln(tw, "ORPHANED\t\t\t")
		for _, r := range report.Orphaned {
			_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", r.Chord, "-", r.Status, r.Command)
		}
	}
	_ = tw.Flush() // stdout; a write failure has nowhere left to be reported

	for _, r := range report.Rows {
		if !r.Conflict {
			continue
		}
		_, _ = fmt.Fprintf(w, "\nconflict: target %q is bound to more than one key. %s is asked for by: %s\n",
			r.Target, r.Chord, names(r.Projects))
	}
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
