package main

import (
	"context"
	"flag"
	"fmt"

	"github.com/hk9890/revier/internal/state"
)

func cmdAttach(ctx context.Context, a *app, args []string) error {
	fs := flag.NewFlagSet("attach", flag.ContinueOnError)
	project := projectFlag(fs)
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 0 {
		return fmt.Errorf("usage: revier attach [-p project]")
	}
	if a.core.Window == nil {
		return fmt.Errorf("attach needs a window host; none is available here")
	}
	p, err := a.resolveProject(ctx, *project)
	if err != nil {
		return err
	}
	ref, err := a.core.Window.Focused(ctx)
	if err != nil {
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
