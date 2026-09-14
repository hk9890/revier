package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"text/tabwriter"
	"unicode"
	"unicode/utf8"

	"github.com/hk9890/revier/internal/core"
)

// cmdKeysApply is install and uninstall. They differ in the plan they build
// and in nothing else, so they share the run: build the plan, print it, apply
// it unless this is a dry run, print what happened.
func cmdKeysApply(ctx context.Context, a *app, sub string, args []string) error {
	fs := flag.NewFlagSet("keys "+sub, flag.ContinueOnError)
	force := fs.Bool("force", false, "take a key something else holds")
	var dryRun bool
	fs.BoolVar(&dryRun, "dry-run", false, "print what would happen and change nothing")
	fs.BoolVar(&dryRun, "n", false, "print what would happen and change nothing (shorthand)")
	asJSON := fs.Bool("json", false, "emit the plan as JSON")
	if _, err := parseArgs(fs, args); err != nil {
		return err
	}
	if sub == "uninstall" && *force {
		// Nothing to force: uninstall removes what revier wrote and never
		// touches anything else, so there is no key it could be asked to take.
		return fmt.Errorf("keys uninstall takes no --force: it removes only revier's own shortcuts")
	}

	trigger, err := a.cfg.TriggerKey()
	if err != nil {
		return err
	}

	plan, err := buildPlan(ctx, a, sub, trigger)
	if err != nil {
		return err
	}
	if !dryRun {
		plan = a.core.ApplyKeys(ctx, plan, *force)
		for _, s := range plan.Steps {
			if s.Err != "" {
				slog.Error("keys "+sub, "step", s)
			} else if s.Done {
				slog.Info("keys "+sub, "step", s)
			}
		}
	}

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(plan); err != nil {
			return err
		}
	} else {
		printPlan(os.Stdout, plan, sub, dryRun, *force)
	}

	// A skipped key and a failed one are both reasons the desktop is not what
	// was asked for, so the status says so and a script can see it. The status
	// is the same either way: a caller reading JSON is the one most likely to
	// be a script, so it is the last one that should have to parse the plan to
	// find out the run was incomplete.
	for _, s := range plan.Steps {
		if s.Err != "" || (s.Action.Blocked() && !*force) {
			return errKeysIncomplete
		}
	}
	return nil
}

// errKeysIncomplete means the command ran and the desktop is not fully what
// was asked for. It carries no message: every reason has already been printed
// against the key it belongs to.
var errKeysIncomplete = fmt.Errorf("some keys were not installed")

func buildPlan(ctx context.Context, a *app, sub string, trigger core.Chord) (core.KeyPlan, error) {
	if sub == "uninstall" {
		return a.core.PlanUninstallKeys(ctx, a.projects, trigger)
	}
	return a.core.PlanInstallKeys(ctx, a.projects, trigger)
}

// acts reports whether the step changes the desktop on this run. A dry run has
// no Done to read, so it is worked out the same way the run itself works it
// out: everything that writes, less what --force was not given for.
func acts(s core.KeyStep, force bool) bool {
	if s.Action.Blocked() && !force {
		return false
	}
	return s.Action.Writes()
}

// mark is the glyph in front of a key's line. It says at a glance whether the
// key is revier's, which is the question the whole command answers.
func mark(s core.KeyStep, dryRun, force bool) string {
	switch {
	case s.Action == core.KeyOK:
		return "+"
	case s.Action == core.KeyAbsent:
		return "-"
	case s.Err != "":
		return "!"
	case s.Done || (dryRun && acts(s, force)):
		return ">"
	default:
		return "-" // skipped: something else holds it
	}
}

// note is what the line says happened to the key. Every key gets one, changed
// or not: a run that printed only the keys it touched would leave the reader
// counting to find out whether the others were already right.
func note(s core.KeyStep, dryRun, force bool) string {
	if s.Err != "" {
		return "failed: " + s.Err
	}
	switch s.Action {
	case core.KeyOK:
		return "ok"
	case core.KeyAbsent:
		return "not installed"
	}
	if !acts(s, force) {
		return "skipped: " + heldBy(s) + " holds it"
	}

	switch s.Action {
	case core.KeyTakeOver:
		return displaced(s, "switched off", dryRun)
	case core.KeyClear:
		return displaced(s, "cleared", dryRun)
	}
	return wouldBe(verbs[s.Action], dryRun)
}

var verbs = map[core.KeyAction]string{
	core.KeyCreate: "created",
	core.KeyUpdate: "command rewritten",
	core.KeyEnable: "switched on",
	core.KeyRemove: "removed",
}

// displaced is the note for a key taken from something else. It says what
// happens to revier's own shortcut as well, and says nothing about it when it
// is already right: "created" for a shortcut that exists would be a lie.
func displaced(s core.KeyStep, what string, dryRun bool) string {
	own, ok := verbs[s.Own]
	if !ok {
		return heldBy(s) + " " + wouldBe(what, dryRun)
	}
	return wouldBe(own, dryRun) + ", " + heldBy(s) + " " + what
}

func wouldBe(verb string, dryRun bool) string {
	if dryRun {
		return "would be " + verb
	}
	return verb
}

func heldBy(s core.KeyStep) string {
	if s.HeldBy != "" {
		return s.HeldBy
	}
	return "something else"
}

func printPlan(w io.Writer, plan core.KeyPlan, sub string, dryRun, force bool) {
	_, _ = fmt.Fprintf(w, "=== revier keys %s ===\n\n", sub)
	if dryRun {
		_, _ = fmt.Fprint(w, "  dry run: nothing is changed\n\n")
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, s := range plan.Steps {
		_, _ = fmt.Fprintf(tw, "  %s %s\t%s\t%s\n",
			mark(s, dryRun, force), prettyChord(s.Chord), bindingName(s), note(s, dryRun, force))
		// The undo goes with the attempt, not with the success: a step that
		// failed after clearing the setting is exactly the one whose reader
		// needs the line back.
		if s.Undo != "" && acts(s, force) {
			_, _ = fmt.Fprintf(tw, "  \t\tundo: %s\n", s.Undo)
		}
	}
	_ = tw.Flush() // stdout; a write failure has nowhere left to be reported

	printSummary(w, plan, sub, dryRun, force)
}

func printSummary(w io.Writer, plan core.KeyPlan, sub string, dryRun, force bool) {
	total := len(plan.Steps)

	// A dry run has nothing done, so it counts what it said it would do.
	// Counting only Done would report every dry run as nothing installed.
	settled := 0
	for _, s := range plan.Steps {
		switch {
		case s.Action == core.KeyOK, s.Done:
			settled++
		case dryRun && acts(s, force):
			settled++
		}
	}

	if sub == "uninstall" {
		// Only the keys revier holds count. "4 of 4 released" on a desktop
		// where revier holds nothing would read as work that happened.
		held, released := 0, 0
		for _, s := range plan.Steps {
			if s.Action != core.KeyRemove {
				continue
			}
			held++
			if s.Done || dryRun {
				released++
			}
		}
		switch {
		case held == 0:
			_, _ = fmt.Fprintln(w, "\n  keys: revier holds none of them")
		case dryRun:
			_, _ = fmt.Fprintf(w, "\n  keys: %d of %d would be released\n", released, held)
		default:
			_, _ = fmt.Fprintf(w, "\n  keys: %d of %d released\n", released, held)
		}
		return
	}

	switch {
	case dryRun:
		_, _ = fmt.Fprintf(w, "\n  keybindings: %d of %d would be installed\n", settled, total)
	default:
		word := "no"
		if settled == total {
			word = "yes"
		}
		_, _ = fmt.Fprintf(w, "\n  keybindings: %s (%d/%d installed)\n", word, settled, total)
	}

	if blocked := plan.Blocked(); blocked > 0 && !force {
		_, _ = fmt.Fprintf(w,
			"  %d %s held by something else. --force takes %s.\n",
			blocked, plural(blocked, "key is", "keys are"), plural(blocked, "it", "them"))
	}
	if settled == total && !dryRun {
		_, _ = fmt.Fprintln(w, "  `revier keys uninstall` gives them back.")
	}
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// bindingName is what the desktop's settings window will show for the key.
func bindingName(s core.KeyStep) string {
	if s.Target == "" {
		return "revier"
	}
	return "revier: " + s.Target
}

// prettyChord is the canonical chord with the separator a settings window
// uses, so a key printed here is findable in GNOME's own list.
func prettyChord(c core.Chord) string {
	parts := strings.Split(string(c), "+")
	for i, p := range parts {
		first, size := utf8.DecodeRuneInString(p)
		if size == 0 {
			continue
		}
		parts[i] = string(unicode.ToUpper(first)) + p[size:]
	}
	return strings.Join(parts, "+")
}
