package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	usecase "github.com/hk9890/revier/internal/app"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/pkg/revier"
)

const shutdownUsage = "usage: revier shutdown [<project>] [--agents | --targets] [--force] [--no-session-save] [--dry-run]"

// errShutdownBusy is a shutdown refused because an agent is working or
// waiting for an answer. The agents are printed before it.
var errShutdownBusy = errors.New("agents are busy; nothing saved and nothing closed. use --force to shut down anyway")

// errShutdownTurnedBusy is a shutdown the close path refused: an agent was
// idle when the plan was made and is busy now. Nothing was saved and nothing
// closed.
var errShutdownTurnedBusy = errors.New("agents turned busy since the plan was made; nothing saved and nothing closed. use --force to shut down anyway")

// errShutdownIncomplete is a shutdown that ended with a target still open or
// not read, or that a cancel stopped with steps left. It fails the command,
// so `revier shutdown && poweroff` does not power off under an editor that
// still asks about unsaved work.
var errShutdownIncomplete = errors.New("shutdown incomplete; not safe to leave")

// closeWait is how long a shutdown or an agent close waits for what it
// closed to go. A variable, so a test of a target that stays does not wait
// the whole core.CloseWait.
var closeWait = core.CloseWait

// cmdShutdown closes what is open: every project's targets, or one project's,
// or only their agents or only what holds no agent. It saves the session
// first when it changed, so a restore brings back what the shutdown closed,
// and it refuses while an agent is busy unless forced (decisions.md D78). A
// shutdown of every project, all of each, is recorded in the state, so the
// next surface offers the restore (D141). ctrl+c stops it between two steps,
// and ends its wait for what it closed.
func cmdShutdown(ctx context.Context, a *app, args []string) error {
	fs := flag.NewFlagSet("shutdown", flag.ContinueOnError)
	agents := fs.Bool("agents", false, "close only the agents")
	targets := fs.Bool("targets", false, "close only the targets that hold no agent")
	force := fs.Bool("force", false, "close busy agents too")
	noSave := fs.Bool("no-session-save", false, "do not save the session first")
	dry := fs.Bool("dry-run", false, "print what it would close, and close nothing")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 1 || (*agents && *targets) {
		return errors.New(shutdownUsage)
	}
	var only revier.ProjectName
	if len(pos) == 1 {
		only = revier.ProjectName(pos[0])
		if _, ok := a.project(only); !ok {
			return fmt.Errorf("no project named %q", only)
		}
	}
	scope := core.ShutdownAll
	switch {
	case *agents:
		scope = core.ShutdownAgents
	case *targets:
		scope = core.ShutdownTargets
	}

	report := a.core.SurveyToClose(ctx, a.projects, only)
	plan := a.core.ShutdownPlan(report, only, scope)
	if len(plan) == 0 {
		_, _ = fmt.Fprintln(a.out, "nothing to close")
		return nil
	}
	if *dry {
		// A dry run prints the order this listing gives. The close takes its
		// own from the survey it rechecks with.
		return printClosePlan(a.out, core.CloseLast(plan, report.Instances, a.core.Self))
	}
	if busy := core.Busy(plan); len(busy) > 0 && !*force {
		if err := printClosePlan(a.out, busy); err != nil {
			return err
		}
		return errShutdownBusy
	}

	req := usecase.CloseRequest{
		Core: a.core, StateRoot: a.stateRoot, Plan: plan, Projects: a.projects,
		Project: only, Scope: scope, Force: *force, Save: !*noSave, Wait: closeWait,
	}
	// The save is said before the first close, as app.Close makes it.
	req.Saved = func(s usecase.Saved) {
		switch {
		case s.New:
			_, _ = fmt.Fprintf(a.out, "session %s saved: %s, %s\n", s.Session.ID,
				core.Count(len(s.Session.Projects), "project"), core.Count(s.Session.Targets(), "target"))
			for _, note := range s.Gaps.Notes() {
				_, _ = fmt.Fprintf(a.out, "  %s\n", note)
			}
		case s.Session.ID != "":
			_, _ = fmt.Fprintf(a.out, "session %s already holds what is open\n", s.Session.ID)
		}
	}

	// Each step is named before it closes, so a host that hangs on one is
	// the step left on the screen, and what came of it follows on that line.
	pw, nw := 0, 0
	for _, s := range plan {
		pw, nw = max(pw, len(s.Project)), max(nw, len(s.Name()))
	}
	var said []string      // what each step's line said, in the order it closed
	var walked core.Closed // the steps closed, before the wait says which went
	req.Progress = func(p core.Progress) {
		switch {
		case p.Phase == core.PhaseWait:
			// The wait is for each step asked to close that did not fail.
			if asked, _, _ := walked.Counts(); asked > 0 {
				_, _ = fmt.Fprintf(a.out, "waiting for %d to close\n", asked)
			}
			return
		case p.Phase != core.PhaseClose:
			return
		case !p.Done:
			_, _ = fmt.Fprintf(a.out, "%-*s  %-*s  ", pw, p.Closed.Project, nw, p.Label)
			return
		}
		walked = append(walked, p.Closed)
		said = append(said, p.Closed.Note())
		_, _ = fmt.Fprintln(a.out, said[len(said)-1])
	}
	// ctrl+c stops the close between two steps, and ends the wait after
	// them. A second ctrl+c ends the command before the shutdown returns, so
	// it abandons the record, which takes it back or keeps it. app.Close
	// decides whether the record is written at all, and the record of a close
	// that is not full has nothing to take back.
	req.Record = usecase.NewShutdownRecord(a.core, a.stateRoot)
	walk, stop := interruptible(ctx, req.Record.Abandon)
	defer stop()
	res, err := usecase.Close(walk, req)
	closed := res.Closed
	cancelled := errors.Is(walk.Err(), context.Canceled)
	switch {
	case res.Refused != nil:
		if err := printClosePlan(a.out, core.Busy(res.Refused)); err != nil {
			return err
		}
		return errShutdownTurnedBusy
	// A cancel before the first close fails the save on the context it
	// ended; the user's word for that is the cancel.
	case cancelled && errors.Is(err, context.Canceled):
		_, _ = fmt.Fprintln(a.out, "cancelled; nothing closed")
		return errShutdownIncomplete
	case err != nil:
		return fmt.Errorf("%w; nothing closed", err)
	}
	// A step is listed once more after the wait when what came of it changed:
	// it did not go, or a close that failed went all the same.
	for i, r := range closed {
		if r.Note() != said[i] {
			_, _ = fmt.Fprintf(a.out, "%-*s  %-*s  %s\n", pw, r.Project, nw, r.Name(), r.Note())
		}
	}
	n, open, failed := closed.Counts()
	_, _ = fmt.Fprintf(a.out, "closed %d, %d still open\n", n, open)
	// A cancel that came after the last step was walked left no step
	// unreached; it can only have ended the wait, which is said when
	// something was still open then.
	left := len(plan) - len(closed)
	switch {
	case cancelled && left > 0:
		_, _ = fmt.Fprintf(a.out, "cancelled: %s not reached\n", core.Count(left, "step"))
	case cancelled && open > 0:
		_, _ = fmt.Fprintln(a.out, "cancelled: the wait for what was closed to go ended")
	}
	if failed > 0 {
		return fmt.Errorf("%s did not close", core.Count(failed, "step"))
	}
	if open > 0 || (cancelled && left > 0) {
		return errShutdownIncomplete
	}
	_, _ = fmt.Fprintln(a.out, "shutdown complete; safe to leave")
	return nil
}

// printClosePlan prints what a shutdown closes, a busy agent marked.
func printClosePlan(out io.Writer, plan []core.CloseStep) error {
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	for _, s := range plan {
		note := "close"
		switch {
		case s.Unread != "":
			note = "leave open: " + s.Unread
		case s.Action == core.CloseUnsupported:
			note = "leave open: its host cannot close it"
		case s.Busy():
			note = "close, " + busyNote(s.BusyAgents())
		case len(s.Agents) > 0:
			note = "close, and " + core.Count(len(s.Agents), "agent") + " in it"
		}
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\n", s.Project, s.Name(), note)
	}
	return w.Flush()
}

// busyNote names busy agents by their status.
func busyNote(agents []revier.AgentView) string {
	var busy []string
	for _, a := range agents {
		busy = append(busy, a.State.Harness+" "+a.State.Status.String())
	}
	return "busy: " + strings.Join(busy, ", ")
}
