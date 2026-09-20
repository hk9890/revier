package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

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

// cmdShutdown closes what is open: every project's targets, or one project's,
// or only their agents or only what holds no agent (decisions.md D78). It
// saves the session first when it changed, so a restore brings back what the
// shutdown closed, and it refuses while an agent is busy unless forced.
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

	report, err := a.core.Survey(ctx, a.projects, a.state.Bound, a.state.Attached)
	if err != nil {
		return err
	}
	plan := core.CloseLast(a.core.ShutdownPlan(report, only, scope), report.Instances, core.RunsUnder())
	if len(plan) == 0 {
		_, _ = fmt.Fprintln(a.out, "nothing to close")
		return nil
	}
	if *dry {
		return printClosePlan(a.out, plan)
	}
	if busy := core.Busy(plan); len(busy) > 0 && !*force {
		if err := printClosePlan(a.out, busy); err != nil {
			return err
		}
		return errShutdownBusy
	}

	opts := core.ShutdownOpts{
		Force: *force, Projects: a.projects, Bound: a.state.Bound, Attached: a.state.Attached,
	}
	if !*noSave {
		// The save runs after the busy guard, so a shutdown the guard
		// refuses leaves no session file behind either.
		opts.Before = func() error {
			stored, saved, gaps, err := a.core.SaveChanged(ctx, a.stateRoot, report, a.state.Current, time.Now())
			switch {
			case err != nil:
				return err
			case saved:
				_, _ = fmt.Fprintf(a.out, "session %s saved: %s, %s\n", stored.ID,
					core.Count(len(stored.Projects), "project"), core.Count(stored.Targets(), "target"))
				for _, note := range gaps.Notes() {
					_, _ = fmt.Fprintf(a.out, "  %s\n", note)
				}
			case stored.ID != "":
				_, _ = fmt.Fprintf(a.out, "session %s already holds what is open\n", stored.ID)
			}
			return nil
		}
	}

	closed, err := a.core.Shutdown(ctx, plan, core.CloseWait, opts)
	var refused *core.BusyRefusal
	switch {
	case errors.As(err, &refused):
		if err := printClosePlan(a.out, core.Busy(refused.Plan)); err != nil {
			return err
		}
		return errShutdownTurnedBusy
	case err != nil:
		return fmt.Errorf("%w; nothing closed", err)
	}
	w := tabwriter.NewWriter(a.out, 0, 0, 2, ' ', 0)
	for _, r := range closed {
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\n", r.Project, r.Name(), r.Note())
	}
	if err := w.Flush(); err != nil {
		return err
	}
	n, open, failed := closed.Counts()
	_, _ = fmt.Fprintf(a.out, "closed %d, %d still open\n", n, open)
	if failed > 0 {
		return fmt.Errorf("%s did not close", core.Count(failed, "step"))
	}
	return nil
}

// printClosePlan prints what a shutdown closes, a busy agent marked.
func printClosePlan(out io.Writer, plan []core.CloseStep) error {
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	for _, s := range plan {
		note := "close"
		switch {
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
