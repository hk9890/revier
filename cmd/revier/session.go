package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/session"
)

const sessionUsage = `usage:
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
	s := a.core.Session(ctx, report, a.state.Current)
	s.At, s.Name = time.Now(), *name

	stored, path, err := session.Save(a.stateRoot, s)
	if err != nil {
		return err
	}
	fmt.Printf("%s: %s, %s\n", stored.ID,
		count(len(stored.Projects), "project"), count(stored.Targets(), "target"))
	if n := conversations(stored); n > 0 {
		fmt.Printf("  %s recorded\n", count(n, "agent conversation"))
	}
	// Named up front, not discovered during a restore after the reboot. An
	// attachment is a live id with no launch argv anywhere in the model, so
	// there is nothing that could bring one back.
	if n := attachments(report); n > 0 {
		fmt.Printf("  %s not recorded; they have no name to be reopened by\n", count(n, "attached instance"))
	}
	fmt.Printf("  %s\n", path)
	return nil
}

// cmdSessionRestore opens what a saved session recorded. It walks the plan in
// file order and never in parallel: a launch is bound to the window that
// appears after it, so two at once are two windows neither can be attributed
// to.
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
	if err != nil {
		return err
	}
	report, err := a.core.Survey(ctx, a.projects, a.state.Bound, a.state.Attached)
	if err != nil {
		return err
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	opened, failed := 0, 0
	for _, step := range a.core.RestorePlan(s, report) {
		if step.Action != core.RestoreLaunch {
			_, _ = fmt.Fprintf(w, "%s\t%s\t%s\n", step.Project, step.Target, step.Action)
			continue
		}
		if *dry {
			_, _ = fmt.Fprintf(w, "%s\t%s\t%s\n", step.Project, step.Target, resumeNote("would open", step))
			continue
		}
		p, ok := a.project(step.Project)
		if !ok {
			// The plan was built from this survey, so a project it knew
			// cannot be missing here. Reported rather than asserted.
			_, _ = fmt.Fprintf(w, "%s\t%s\t%s\n", step.Project, step.Target, core.RestoreNoProject)
			continue
		}
		// One project's failure is not the restore's: nineteen workspaces
		// still come back, and the one that did not is named.
		if _, err := a.goTargetResuming(ctx, p, step.Target, step.Resumes); err != nil {
			failed++
			_, _ = fmt.Fprintf(w, "%s\t%s\t%v\n", step.Project, step.Target, err)
			continue
		}
		opened++
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\n", step.Project, step.Target, resumeNote("opened", step))
	}
	if err := w.Flush(); err != nil {
		return err
	}

	if *dry {
		return nil
	}
	// End on the project the save was left on, so a restored desktop lands
	// where the saved one was. It is run-or-raise like everything else.
	if p, ok := a.project(s.Current); ok {
		if home, has := p.Home(); has {
			if _, err := a.goTarget(ctx, p, home.Name); err != nil {
				fmt.Printf("could not return to %s: %v\n", s.Current, err)
			}
		}
	}
	fmt.Printf("%s: opened %d\n", s.ID, opened)
	if failed > 0 {
		return fmt.Errorf("%d targets did not open", failed)
	}
	return nil
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
		_, _ = fmt.Fprintf(w, "%s\t%s\t%d\t%d\t%d\n", s.ID, s.Name, len(s.Projects), s.Targets(), conversations(s))
	}
	return w.Flush()
}

// resumeNote says what opening a target did, or would do, naming the agents it
// starts on a conversation rather than empty.
func resumeNote(verb string, step core.RestoreStep) string {
	if len(step.Resumes) == 0 {
		return verb
	}
	return fmt.Sprintf("%s, %s resumed", verb, count(len(step.Resumes), "agent"))
}

// count writes a number and its noun, pluralised. A summary that reads
// "1 projects" is a summary nobody proofread.
func count(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

func conversations(s session.Session) int {
	n := 0
	for _, p := range s.Projects {
		for _, t := range p.Targets {
			n += len(t.Panels)
		}
	}
	return n
}

func attachments(r core.Report) int {
	n := 0
	for _, v := range r.Views {
		for _, tv := range v.Targets {
			if tv.Attached {
				n++
			}
		}
	}
	return n
}
