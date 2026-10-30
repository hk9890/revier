package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"time"

	usecase "github.com/hk9890/revier/internal/app"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/pkg/revier"
)

const renameUsage = "usage: revier rename <old> <new>"

// cmdRename gives a project a new name: its file, its state and the saved
// sessions move with it, as the project screen's rename moves them. It is
// refused while anything of the project runs here, by the guard the TUI asks
// too, since its windows are found by titles the name is part of
// (decisions.md D80).
func cmdRename(ctx context.Context, a *app, args []string) error {
	fs := flag.NewFlagSet("rename", flag.ContinueOnError)
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 2 {
		return errors.New(renameUsage)
	}
	from, to := revier.ProjectName(pos[0]), revier.ProjectName(pos[1])
	p, ok := a.project(from)
	switch {
	case !ok:
		return fmt.Errorf("no project named %q", from)
	case p.File == "":
		return fmt.Errorf("%s is built in and in no file; there is nothing to rename", from)
	}
	report, err := a.core.Survey(ctx, []core.Project{p})
	if err != nil {
		return err
	}
	// The first settle of this process: no window is new to it, so it claims
	// nothing, and it prunes the refs to windows that are gone, which the
	// guard would otherwise read as running.
	st := a.core.Settle(report, a.projects, time.Now())
	settled := core.Settled{Surveyed: true, Bound: st.Bound, Attached: st.Attached}
	if err := a.core.RefuseChange(report.Views[0], settled, "renaming"); err != nil {
		return err
	}
	renamed, err := usecase.RenameProject(a.cfgRoot, a.stateRoot, a.core.Ledger, from, to, a.cfg.Inherited())
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(a.out, "renamed %s to %s: %s\n", from, to, renamed.File)
	return nil
}
