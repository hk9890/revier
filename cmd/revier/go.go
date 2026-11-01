package main

import (
	"context"
	"errors"
	"flag"
	"fmt"

	"github.com/hk9890/revier/pkg/revier"
)

func cmdGo(ctx context.Context, a *app, args []string) error {
	fs := flag.NewFlagSet("go", flag.ContinueOnError)
	project := projectFlag(fs)
	picker := fs.Bool("picker", false, "open the popup when no project resolves")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return fmt.Errorf("usage: revier go <target> [-p project] [--picker]")
	}
	p, err := a.resolveProject(ctx, *project)
	if errors.Is(err, errNoProject) && *picker {
		return cmdPopup(ctx, a)
	}
	if err != nil {
		return err
	}
	ref, res, err := a.goTarget(ctx, p, revier.TargetName(pos[0]))
	if err != nil {
		return err
	}
	landed := describe(ref)
	if res.Ran {
		// A command target binds nothing, so there is no instance to name.
		landed = "ran " + string(res.Target)
	}
	_, _ = fmt.Fprintf(a.out, "%s: %s\n", p.Name, landed)
	return nil
}
