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
	ref, err := a.goTarget(ctx, p, revier.TargetName(pos[0]))
	if err != nil {
		return err
	}
	fmt.Fprintf(a.out, "%s: %s\n", p.Name, describe(ref))
	return nil
}
