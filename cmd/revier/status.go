package main

import (
	"context"
	"flag"
	"fmt"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/pkg/revier"
)

func cmdStatus(ctx context.Context, a *app, args []string) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	project := projectFlag(fs)
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 0 {
		return fmt.Errorf("usage: revier status [-p project]")
	}
	p, err := a.resolveProject(ctx, *project)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(a.out, "project   %s\npath      %s\n", p.Name, p.Path)
	_, _ = fmt.Fprintf(a.out, "runtime   %s\nwindow    %s\n", hostName(a.core.Runtime), hostName(a.core.Window))
	if attached := a.state.Attached[p.Name]; len(attached) > 0 {
		_, _ = fmt.Fprintf(a.out, "attached  %s\n", core.Count(len(attached), "window"))
	}
	return nil
}

func hostName(h revier.Host) string {
	if h == nil {
		return "-"
	}
	return h.Name()
}
