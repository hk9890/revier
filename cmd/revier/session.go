package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"
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
	s, gaps := a.core.Session(ctx, report, a.state.Current)
	// An empty save would become the newest session, which a plain restore
	// opens: the save made before a reboot would lose to one made after it.
	if len(s.Projects) == 0 {
		fmt.Println("nothing is open; no session saved")
		return nil
	}
	s.At, s.Name = time.Now(), *name

	stored, path, err := session.Save(a.stateRoot, s)
	if err != nil {
		return err
	}
	targets, conversations, attached := stored.Targets(), stored.Conversations(), attachments(report)
	slog.Info("session saved", "id", stored.ID, "name", stored.Name, "path", path,
		"projects", len(stored.Projects), "targets", targets, "conversations", conversations,
		"unnamed_agents", gaps.Unnamed, "agents_in_tab", gaps.InTab, "attached_not_recorded", attached)
	for _, err := range gaps.Failed {
		slog.Warn("session save: probe could not be asked", "err", err)
	}
	fmt.Printf("%s: %s, %s\n", stored.ID,
		count(len(stored.Projects), "project"), count(targets, "target"))
	if conversations > 0 {
		fmt.Printf("  %s recorded\n", count(conversations, "agent conversation"))
	}
	// Said now, while the agents still run, so the gap can be closed before
	// the reboot rather than found after it.
	if gaps.Unnamed > 0 {
		fmt.Printf("  %s without a conversation id, to be restored empty\n", count(gaps.Unnamed, "agent"))
	}
	if len(gaps.InTab) > 0 {
		fmt.Printf("  %s in a tab target, to be restored without its conversation: %s\n",
			count(len(gaps.InTab), "agent"), strings.Join(gaps.InTab, ", "))
	}
	for _, err := range gaps.Failed {
		fmt.Printf("    could not ask %v\n", err)
	}
	// Named up front, not discovered during a restore after the reboot. An
	// attachment is a live id with no launch argv anywhere in the model, so
	// there is nothing that could bring one back.
	if attached > 0 {
		fmt.Printf("  %s not recorded; they have no name to be reopened by\n", count(attached, "attached instance"))
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

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	opened, pending, failed := 0, 0, 0
	slog.Info("session restore", "id", s.ID, "name", s.Name, "saved_at", s.At, "dry_run", *dry)
	for _, step := range a.core.RestorePlan(s, report) {
		if step.Action != core.RestoreLaunch {
			slog.Info("restore step", "project", step.Project, "target", step.Target, "action", step.Action.String())
			_, _ = fmt.Fprintf(w, "%s\t%s\t%s\n", step.Project, step.Target, step.Action)
			continue
		}
		p, ok := a.project(step.Project)
		if !ok {
			// The plan was built from this survey, so a project it knew
			// cannot be missing here. Reported rather than asserted.
			slog.Warn("restore step: planned project not loaded", "project", step.Project, "target", step.Target)
			_, _ = fmt.Fprintf(w, "%s\t%s\t%s\n", step.Project, step.Target, core.RestoreNoProject)
			continue
		}
		slog.Info("restore step", "project", step.Project, "target", step.Target, "action", step.Action.String(), "dry_run", *dry)
		if *dry {
			outcomes := a.core.Resumes(p, step.Target, step.Resumes)
			logResumes(step, outcomes, true)
			_, _ = fmt.Fprintf(w, "%s\t%s\t%s\n", step.Project, step.Target, resumeNote("would open", outcomes, nil))
			continue
		}
		// One project's failure is not the restore's: nineteen workspaces
		// still come back, and the one that did not is named.
		ref, res, err := a.goTargetResuming(ctx, p, step.Target, step.Resumes)
		logResumes(step, res.Agents, false)
		if err != nil {
			failed++
			_, _ = fmt.Fprintf(w, "%s\t%s\t%v\n", step.Project, step.Target, err)
			continue
		}
		// A window that has not shown yet is not a target that came back.
		if ref.IsZero() {
			pending++
			_, _ = fmt.Fprintf(w, "%s\t%s\t%s\n", step.Project, step.Target, resumeNote("launched, not up yet", res.Agents, res.AgentErr))
			continue
		}
		opened++
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\n", step.Project, step.Target, resumeNote("opened", res.Agents, res.AgentErr))
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
	slog.Info("session restored", "id", s.ID, "opened", opened, "pending", pending, "failed", failed)
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

// logResumes writes one line per recorded agent of a step: the conversation
// and directory it was recorded with, and what the launch did with it. The
// outcomes are in the order of the step's resumes; a press that failed before
// it launched has none, and every agent is logged with outcome none.
func logResumes(step core.RestoreStep, outcomes []core.AgentOutcome, dry bool) {
	for i, r := range step.Resumes {
		outcome := "none"
		if i < len(outcomes) {
			outcome = outcomes[i].String()
		}
		slog.Info("restore agent", "project", step.Project, "target", step.Target, "dry_run", dry,
			"harness", r.Harness, "session", r.Session, "dir", r.Dir, "outcome", outcome)
	}
}

// resumeNote says what opening a target did, or would do, to the agents it
// recorded: how many start on their conversation, how many start empty because
// the directory they worked in is gone or their conversation cannot be resumed
// here, and how many cannot be started at all, with tabErr for the ones whose
// tab failed to open. An agent recorded with no conversation starts empty as
// it always would, and is not worth a word.
func resumeNote(verb string, agents []core.AgentOutcome, tabErr error) string {
	n := map[core.AgentOutcome]int{}
	for _, o := range agents {
		n[o]++
	}
	note := verb
	if n[core.AgentResumed] > 0 {
		note += fmt.Sprintf(", %s resumed", count(n[core.AgentResumed], "agent"))
	}
	if n[core.AgentDirGone] > 0 {
		note += fmt.Sprintf(", %s empty: directory gone", count(n[core.AgentDirGone], "agent"))
	}
	if n[core.AgentUnresumable] > 0 {
		note += fmt.Sprintf(", %s empty: no probe here resumes its harness in its panel", count(n[core.AgentUnresumable], "agent"))
	}
	if n[core.AgentDropped] > 0 {
		note += fmt.Sprintf(", %s not restored: no agent panel declared, or no tab can be opened here", count(n[core.AgentDropped], "agent"))
	}
	if n[core.AgentInTab] > 0 {
		note += fmt.Sprintf(", %s not resumed: it ran in a tab target", count(n[core.AgentInTab], "agent"))
	}
	if n[core.AgentNotAdded] > 0 {
		note += fmt.Sprintf(", %s not restored: %v", count(n[core.AgentNotAdded], "agent"), tabErr)
	}
	return note
}

// count writes a number and its noun, pluralised. A summary that reads
// "1 projects" is a summary nobody proofread.
func count(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
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
