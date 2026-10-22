package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"text/tabwriter"
	"time"

	usecase "github.com/hk9890/revier/internal/app"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/session"
)

const sessionUsage = `revier session - save the set of open projects, and open it again

usage:
  revier session save [--name <label>]
  revier session restore [<id>|<name>] [--dry-run]
  revier session list [--json]
`

func cmdSession(ctx context.Context, a *app, args []string) error {
	sub := ""
	if len(args) > 0 {
		sub, args = args[0], args[1:]
	}
	switch sub {
	case "save":
		return cmdSessionSave(ctx, a, args)
	case "restore":
		return cmdSessionRestore(ctx, a, args)
	case "list":
		return cmdSessionList(a, args)
	default:
		fmt.Fprint(os.Stderr, sessionUsage)
		return fmt.Errorf("unknown session command %q", sub)
	}
}

// cmdSessionSave records the projects that are open now. It surveys every
// project rather than the ones a flag names: the point of the file is that it
// is the whole desktop, and a partial one restored after a reboot would look
// like the rest was lost.
func cmdSessionSave(ctx context.Context, a *app, args []string) error {
	fs := flag.NewFlagSet("session save", flag.ContinueOnError)
	name := fs.String("name", "", "label this session")
	if _, err := parseArgs(fs, args); err != nil {
		return err
	}
	report, err := a.core.Survey(ctx, a.projects)
	if err != nil {
		return err
	}
	stored, path, gaps, err := usecase.SaveSession(ctx, a.core, a.stateRoot, report, *name, time.Now())
	if errors.Is(err, usecase.ErrNothingOpen) {
		_, _ = fmt.Fprintln(a.out, err)
		return nil
	}
	if err != nil {
		return err
	}
	targets, conversations := stored.Targets(), stored.Conversations()
	_, _ = fmt.Fprintf(a.out, "%s: %s, %s\n", stored.ID,
		core.Count(len(stored.Projects), "project"), core.Count(targets, "target"))
	if conversations > 0 {
		_, _ = fmt.Fprintf(a.out, "  %s recorded\n", core.Count(conversations, "agent conversation"))
	}
	for _, note := range gaps.Notes() {
		_, _ = fmt.Fprintf(a.out, "  %s\n", note)
	}
	_, _ = fmt.Fprintf(a.out, "  %s\n", path)
	return nil
}

// cmdSessionRestore opens what a saved session recorded, as core.Restore
// walks it, and prints a line per recorded target as it is walked.
func cmdSessionRestore(ctx context.Context, a *app, args []string) error {
	fs := flag.NewFlagSet("session restore", flag.ContinueOnError)
	dry := fs.Bool("dry-run", false, "print what it would open, and open nothing")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 1 {
		return errors.New("usage: revier session restore [<id>|<name>] [--dry-run]")
	}
	ref := ""
	if len(pos) > 0 {
		ref = pos[0]
	}
	s, err := session.Load(a.stateRoot, ref)
	if ref == "" && errors.Is(err, session.ErrNoSession) {
		_, _ = fmt.Fprintln(a.out, "no saved session. write one with revier session save")
		return nil
	}
	if err != nil {
		return err
	}
	report, err := a.core.Survey(ctx, a.projects)
	if err != nil {
		return err
	}

	if *dry {
		slog.Info("session restore", "id", s.ID, "name", s.Name, "saved_at", s.At, "dry_run", true)
		preview := a.core.RestorePreview(s, report, a.projects)
		for _, r := range preview {
			core.LogRestore(r)
		}
		return printRestored(a.out, preview)
	}
	// Each step bounds its own launch and wait. The command's deadline is
	// one keypress's, and a walk of several cold starts outlasts it; a
	// cancel that comes before that deadline still stops the walk.
	walk, stop := core.WithoutDeadline(ctx)
	defer stop()
	// Each target is named before it opens, so a launch that hangs is the
	// line left on the screen, and what came of it follows on that line.
	plan := a.core.RestorePlan(s, report)
	pw, tw := 0, 0
	for _, step := range plan {
		pw, tw = max(pw, len(step.Project)), max(tw, len(step.Target))
	}
	restored, back := a.core.Restore(walk, s, report, a.projects, core.RestoreOpts{Progress: func(p core.Progress) {
		switch {
		case p.Phase != core.PhaseLaunch:
		case !p.Done:
			_, _ = fmt.Fprintf(a.out, "%-*s  %-*s  ", pw, p.Restored.Project, tw, p.Label)
		default:
			_, _ = fmt.Fprintln(a.out, p.Restored.Note())
		}
	}})
	if back != nil {
		_, _ = fmt.Fprintln(a.out, back)
	}
	opened, pending, failed := restored.Counts()
	if pending > 0 {
		_, _ = fmt.Fprintf(a.out, "%s: opened %d, %d not up yet\n", s.ID, opened, pending)
	} else {
		_, _ = fmt.Fprintf(a.out, "%s: opened %d\n", s.ID, opened)
	}
	if left := len(plan) - len(restored); left > 0 {
		_, _ = fmt.Fprintf(a.out, "cancelled: %s not reached\n", core.Count(left, "target"))
	}
	if failed > 0 {
		return fmt.Errorf("%d targets did not open", failed)
	}
	return nil
}

func printRestored(out io.Writer, rs core.Restored) error {
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	for _, r := range rs {
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\n", r.Project, r.Target, r.Note())
	}
	return w.Flush()
}

func cmdSessionList(a *app, args []string) error {
	fs := flag.NewFlagSet("session list", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "emit the sessions as JSON")
	if _, err := parseArgs(fs, args); err != nil {
		return err
	}
	all, err := session.List(a.stateRoot)
	if err != nil {
		return err
	}
	if *asJSON {
		enc := json.NewEncoder(a.out)
		enc.SetIndent("", "  ")
		return enc.Encode(all)
	}
	if len(all) == 0 {
		_, _ = fmt.Fprintf(a.out, "no saved sessions. write one with revier session save\n")
		return nil
	}
	w := tabwriter.NewWriter(a.out, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "ID\tREASON\tNAME\tPROJECTS\tTARGETS\tAGENTS")
	for _, s := range all {
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%d\t%d\n", s.ID, s.Reason.Text(), s.Name, len(s.Projects), s.Targets(), s.Agents())
	}
	return w.Flush()
}
