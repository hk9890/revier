package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/hk9890/revier/internal/adapter/gnome"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/pkg/revier"
)

const setupUsage = `revier setup - what revier writes into this machine's configuration

usage:
  revier setup status [--json]              what each part holds now
  revier setup install [--force] [-n] [--json]
                                            write all of it
  revier setup uninstall [-n] [--json]      remove what revier wrote

The parts:
  desktop keys   the project and target keys, and the popup's trigger key,
                 as GNOME shortcuts
  terminal keys  [terminal] shell_key and agent_key, and the key of each
                 [[agent]], in revier.conf in kitty's configuration directory
                 and one include of it at the top of kitty.conf
  popup rules    the window control extension's rules for the popup's
                 window, which size and place it before it is drawn

A part this machine cannot do is skipped, and the line says why.

  --force  take a key another shortcut holds, and replace a rule of yours
           for the popup's window. Without it, install writes what is free
           and reports the rest.
  -n, --dry-run  print what would happen and change nothing
  --json   print the report, or the plan and what became of it, as JSON
`

// keyBinderWait bounds each probe, and setupWait the whole run. Both are
// short because nothing about this command is worth waiting on: gsettings
// and wctl answer in milliseconds, and a service that has stopped answering
// should say so rather than hold the terminal. setupWait covers every
// subprocess of the three parts, so it is the looser of the two.
const (
	keyBinderWait = 3 * time.Second
	setupWait     = 20 * time.Second
)

// setupStores are the three stores `revier setup` reads and writes. A nil
// store is one this machine does not have; the error beside it says why.
type setupStores struct {
	keys     revier.KeyBinder
	term     revier.TermKeys
	rules    revier.WindowRules
	rulesWhy error
}

// machineStores probes this machine for the stores. write picks the types
// that write: status is given the readers, which cannot write whatever a
// later change does to them.
func machineStores(ctx context.Context, write bool) setupStores {
	probe, cancel := context.WithTimeout(ctx, keyBinderWait)
	defer cancel()
	var s setupStores
	if write {
		s.keys = selectKeyWriter(probe, keyWriters())
		s.term = termKeyWriter()
		rules := &gnome.RuleWriter{}
		if s.rulesWhy = rules.Probe(probe); s.rulesWhy == nil {
			s.rules = rules
		}
		return s
	}
	s.keys = selectKeyBinder(probe, keyBinders())
	s.term = termKeys()
	rules := &gnome.Rules{}
	if s.rulesWhy = rules.Probe(probe); s.rulesWhy == nil {
		s.rules = rules
	}
	return s
}

func cmdSetup(ctx context.Context, a *app, args []string) error {
	sub := ""
	if len(args) > 0 {
		sub, args = args[0], args[1:]
	}
	switch sub {
	case "", "help", "--help", "-h":
		_, _ = fmt.Fprint(a.out, setupUsage)
		return nil
	case "status", "install", "uninstall":
	default:
		fmt.Fprint(os.Stderr, setupUsage)
		return fmt.Errorf("unknown setup command %q", sub)
	}

	fs := flag.NewFlagSet("setup "+sub, flag.ContinueOnError)
	var force, dryRun bool
	if sub == "install" {
		fs.BoolVar(&force, "force", false, "take a key or a rule something else holds")
	}
	if sub != "status" {
		fs.BoolVar(&dryRun, "dry-run", false, "print what would happen and change nothing")
		fs.BoolVar(&dryRun, "n", false, "print what would happen and change nothing (shorthand)")
	}
	asJSON := fs.Bool("json", false, "emit the report or the plan as JSON")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 0 {
		return errors.New("usage: " + setupLine(sub))
	}
	trigger, err := a.cfg.TriggerKey()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, setupWait)
	defer cancel()
	stores := a.stores
	if stores == nil {
		stores = machineStores
	}
	s := stores(ctx, sub != "status")
	a.core.KeyBinder, a.core.TermKeys, a.core.WindowRules = s.keys, s.term, s.rules

	run := setupRun{a: a, sub: sub, force: force, dryRun: dryRun, trigger: trigger, rulesWhy: s.rulesWhy}
	if sub == "status" {
		return run.status(ctx, *asJSON)
	}
	return run.apply(ctx, *asJSON)
}

func setupLine(sub string) string {
	switch sub {
	case "install":
		return "revier setup install [--force] [-n] [--json]"
	case "uninstall":
		return "revier setup uninstall [-n] [--json]"
	}
	return "revier setup status [--json]"
}

// setupRun is one run of status, install or uninstall over the three parts.
type setupRun struct {
	a             *app
	sub           string
	force, dryRun bool
	trigger       core.Chord
	rulesWhy      error
}

// setupReport is what status prints as JSON. A part this machine cannot do
// is null, and Skipped says why.
type setupReport struct {
	Keys    *core.KeyReport       `json:"keys"`
	Term    *core.TermKeyReport   `json:"term"`
	Rules   *core.PopupRuleReport `json:"rules"`
	Skipped map[string]string     `json:"skipped"`
}

// setupPlan is what install and uninstall print as JSON: each part's plan
// and what became of it.
type setupPlan struct {
	Keys    *core.KeyPlan       `json:"keys"`
	Term    *core.TermKeyPlan   `json:"term"`
	Rules   *core.PopupRulePlan `json:"rules"`
	Skipped map[string]string   `json:"skipped"`
	// Failed is a part that could not be planned, and why.
	Failed map[string]string `json:"failed"`
}

// errSetupIncomplete means the command ran and the configuration is not all
// that was asked for. It carries no message: every reason has already been
// printed against the part it belongs to.
var errSetupIncomplete = errors.New("some of the setup was not installed")

func (r setupRun) status(ctx context.Context, asJSON bool) error {
	a := r.a
	out := setupReport{Skipped: map[string]string{}}

	keys, err := a.core.KeyReport(ctx, a.projects, r.trigger)
	switch {
	case errors.Is(err, core.ErrNoKeyBinder):
		out.Skipped["keys"] = err.Error()
	case err != nil:
		return err
	default:
		out.Keys = &keys
	}
	term, err := a.core.TermKeyReport(ctx, a.cfg.TermKeys(), a.projects, r.trigger)
	switch {
	case errors.Is(err, core.ErrNoTermKeys):
		out.Skipped["term"] = err.Error()
	case err != nil:
		return err
	default:
		out.Term = &term
	}
	rules, err := a.core.PopupRuleReport(ctx)
	switch {
	case errors.Is(err, core.ErrNoWindowRules):
		out.Skipped["rules"] = r.skipped(err)
	case err != nil:
		return err
	default:
		out.Rules = &rules
	}

	if asJSON {
		return a.printJSON(out)
	}
	w := a.out
	_, _ = fmt.Fprintln(w, "desktop keys:")
	if out.Keys != nil {
		printKeys(w, *out.Keys)
	} else {
		_, _ = fmt.Fprintf(w, "  - skipped: %s\n", out.Skipped["keys"])
	}
	_, _ = fmt.Fprintln(w, "\nterminal keys:")
	if out.Term != nil {
		printTermReport(w, *out.Term)
	} else {
		_, _ = fmt.Fprintf(w, "  - skipped: %s\n", out.Skipped["term"])
	}
	_, _ = fmt.Fprintln(w, "\npopup rules:")
	if out.Rules != nil {
		printRuleReport(w, *out.Rules)
	} else {
		_, _ = fmt.Fprintf(w, "  - skipped: %s\n", out.Skipped["rules"])
	}
	return nil
}

// skipped is why the rules part is skipped: what the probe found, else the
// sentinel.
func (r setupRun) skipped(err error) string {
	if r.rulesWhy != nil {
		return r.rulesWhy.Error()
	}
	return err.Error()
}
