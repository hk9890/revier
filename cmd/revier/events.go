package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"time"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/events"
	"github.com/hk9890/revier/internal/state"
	"github.com/hk9890/revier/pkg/revier"
)

const eventsUsage = `revier events - what revier did in each project

usage:
  revier events [--days <n>] [--local]

  --days     how far back to read, in days of 24 hours (default 7)
  --local    only what this machine recorded; ask no linked host

One JSON line per event, oldest first:

  time      when
  event     go, go agent, agent new, shell new, action, agent session
  project   the project, by its name on the machine that recorded it
  host      the linked host that recorded it; absent for this machine
  target    where a go landed, or the workspace a tab opened in
  launched  true when a go started its target instead of raising it
  action    the action that ran
  agent     the harness of an agent session
  session   the conversation an agent holds, as its harness names it
  dir       the directory the agent works in, or a tab started in

An agent session is written when the surface first sees an agent hold a
conversation, and once on each later day it still does. A host that does not
answer is named on stderr, and the events of the others are printed.
`

// cmdEvents prints the event file, and with it what the revier on each host
// a project here links to recorded. It counts nothing: what "used" means is
// the reader's to decide (decisions.md D112).
func cmdEvents(out io.Writer, args []string) error {
	if len(args) > 0 && slices.Contains([]string{"help", "--help", "-h"}, args[0]) {
		_, _ = fmt.Fprint(out, eventsUsage)
		return nil
	}
	fs := flag.NewFlagSet("events", flag.ContinueOnError)
	days := fs.Int("days", 7, "how far back to read, in days")
	local := fs.Bool("local", false, "ask no linked host")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 0 || *days < 1 {
		return errors.New("usage: revier events [--days <n>] [--local]")
	}

	root, err := state.Root()
	if err != nil {
		return err
	}
	if *local {
		return writeEvents(context.Background(), out, root, *days, nil, nil)
	}
	cfgRoot, err := config.Root()
	if err != nil {
		return err
	}
	cfg, projects, err := config.Load(cfgRoot)
	if err != nil {
		return err
	}
	warnProblems(cfg, projects)
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	return writeEvents(ctx, out, root, *days, newCore(cfg, nil, nil, nil), projects)
}

// writeEvents prints what this machine recorded in the last days and, through
// c, what the hosts of the projects did, in the order it happened. A nil c
// asks no host.
func writeEvents(ctx context.Context, out io.Writer, root string, days int, c *core.Core, projects []core.Project) error {
	recorded, err := events.Read(root, time.Now().Add(-time.Duration(days)*24*time.Hour))
	if err != nil {
		return err
	}
	if c != nil {
		there, failed := c.RemoteEvents(ctx, projects, days)
		for _, host := range slices.Sorted(maps.Keys(failed)) {
			fmt.Fprintf(os.Stderr, "revier: warning: no events from %s: %v\n", host, failed[host])
		}
		recorded = append(recorded, there...)
		slices.SortStableFunc(recorded, func(a, b revier.Event) int { return a.Time.Compare(b.Time) })
	}
	enc := json.NewEncoder(out)
	for _, e := range recorded {
		if err := enc.Encode(e); err != nil {
			return err
		}
	}
	return nil
}
