package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"text/tabwriter"
	"time"

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
	report, err := a.core.Survey(ctx, a.projects, a.state.Bound, a.state.Attached)
	if err != nil {
		return err
	}
	stored, path, gaps, err := a.core.SaveSession(ctx, a.stateRoot, report, a.state.Current, *name, time.Now())
	if errors.Is(err, core.ErrNothingOpen) {
		fmt.Println(err)
		return nil
	}
	if err != nil {
		return err
	}
	targets, conversations := stored.Targets(), stored.Conversations()
	fmt.Printf("%s: %s, %s\n", stored.ID,
		core.Count(len(stored.Projects), "project"), core.Count(targets, "target"))
	if conversations > 0 {
		fmt.Printf("  %s recorded\n", core.Count(conversations, "agent conversation"))
	}
	for _, note := range gaps.Notes() {
		fmt.Printf("  %s\n", note)
	}
	fmt.Printf("  %s\n", path)
	return nil
}

// cmdSessionRestore opens what a saved session recorded, as core.Restore
// walks it, and prints a line per recorded target.
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
		fmt.Println("no saved session. write one with revier session save")
		return nil
	}
	if err != nil {
		return err
	}
	report, err := a.core.Survey(ctx, a.projects, a.state.Bound, a.state.Attached)
	if err != nil {
		return err
	}

	if *dry {
		slog.Info("session restore", "id", s.ID, "name", s.Name, "saved_at", s.At, "dry_run", true)
		preview := a.core.RestorePreview(s, report, a.projects)
		for _, r := range preview {
			core.LogRestore(r)
		}
		return printRestored(preview)
	}
	restored, back := a.core.Restore(ctx, s, report, a.projects, ledger{a})
	if err := printRestored(restored); err != nil {
		return err
	}
	if back != nil {
		fmt.Println(back)
	}
	opened, pending, failed := restored.Counts()
	if pending > 0 {
		fmt.Printf("%s: opened %d, %d not up yet\n", s.ID, opened, pending)
	} else {
		fmt.Printf("%s: opened %d\n", s.ID, opened)
	}
	if failed > 0 {
		return fmt.Errorf("%d targets did not open", failed)
	}
	return nil
}

func printRestored(rs core.Restored) error {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
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
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(all)
	}
	if len(all) == 0 {
		fmt.Printf("no saved sessions. write one with revier session save\n")
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "ID\tNAME\tPROJECTS\tTARGETS\tAGENTS")
	for _, s := range all {
		_, _ = fmt.Fprintf(w, "%s\t%s\t%d\t%d\t%d\n", s.ID, s.Name, len(s.Projects), s.Targets(), s.Conversations())
	}
	return w.Flush()
}
