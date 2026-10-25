package main

import (
	"context"
	"flag"
	"fmt"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/record"
	"github.com/hk9890/revier/internal/state"
	"github.com/hk9890/revier/pkg/revier"
)

// cmdAttach attaches the focused window to a project. The state record has
// the attach, failed or not: what it changed is in the state's own lines, and
// a failure is in no other (decisions.md D151).
func cmdAttach(ctx context.Context, a *app, args []string) (err error) {
	fs := flag.NewFlagSet("attach", flag.ContinueOnError)
	project := projectFlag(fs)
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 0 {
		return fmt.Errorf("usage: revier attach [-p project]")
	}
	var p core.Project
	var ref revier.TargetRef
	defer func() {
		a.core.Ledger.Note(record.Line{Kind: record.KindAttach, Project: p.Name, Host: ref.Host, Ref: ref.ID}.Failed(err))
	}()
	if a.core.Window == nil {
		return fmt.Errorf("attach needs a window host; none is available here")
	}
	if p, err = a.resolveProject(ctx, *project); err != nil {
		return err
	}
	if ref, err = a.core.Window.Focused(ctx); err != nil {
		return err
	}
	if ref.IsZero() {
		return fmt.Errorf("no window is focused")
	}
	// The terminal inside the window is attached with it when the two pair
	// beyond doubt: a window carries no panels, so the window alone leaves a
	// survey nothing to probe for the agent in it (decisions.md D95).
	refs := a.core.Attachment(ctx, ref)
	a.commit(p.Name, func(s *state.State) {
		for _, r := range refs {
			s.Attach(p.Name, r)
		}
	})
	_, _ = fmt.Fprintf(a.out, "%s: attached %s\n", p.Name, describe(ref))
	return nil
}
