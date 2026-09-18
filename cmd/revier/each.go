package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/runlog"
	"github.com/hk9890/revier/internal/state"
	"github.com/hk9890/revier/pkg/revier"
)

const eachUsage = `revier each - run one command in every project's directory

usage:
  revier each [--filter '<sh>'] [-n] -- <cmd> [args...]
  revier each log                          past runs, newest first
  revier each log <run>                    how each project ended
  revier each log <run> <project>          what the command printed there

  --filter '<sh>'  a shell test run in each directory; the command runs only
                   where it exits 0. 'test -d .git' selects the repositories.
  -n, --dry-run    print the selection and run nothing

A project whose directory does not exist is skipped, and so is one whose
directory an earlier project has. Projects run one at a time, and each one's
output goes to a file under the state directory rather than to the terminal.
`

// errEachFailed means the command failed in at least one project. The summary
// has already named each one, so it carries no message of its own.
var errEachFailed = errors.New("the command failed in at least one project")

// cmdEach runs one argv in every project's directory.
//
// One at a time, never in parallel. The commands this exists for write shared
// state: a workspace trust lands in one file in the home directory, and a pull
// asks one ssh agent and one credential helper. Run side by side, the first
// loses writes and the second prompts ninety times at once. Output goes to a
// file per project either way, so the terminal shows one line per project.
func cmdEach(w io.Writer, args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "help", "--help", "-h":
			_, _ = fmt.Fprint(w, eachUsage)
			return nil
		case "log":
			return cmdEachLog(w, args[1:])
		}
	}

	// The command follows `--`, always. A command's own flags then cannot be
	// read as revier's, and `revier each log` cannot be read as a command.
	sep := -1
	for i, a := range args {
		if a == "--" {
			sep = i
			break
		}
	}
	if sep < 0 || sep == len(args)-1 {
		fmt.Fprint(os.Stderr, eachUsage)
		return errors.New("each: the command goes after --")
	}
	argv := args[sep+1:]

	fs := flag.NewFlagSet("each", flag.ContinueOnError)
	filter := fs.String("filter", "", "a shell test; run only where it exits 0")
	var dry bool
	fs.BoolVar(&dry, "dry-run", false, "print the selection and run nothing")
	fs.BoolVar(&dry, "n", false, "print the selection and run nothing (shorthand)")
	if err := fs.Parse(args[:sep]); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("each: unexpected %q before --", fs.Arg(0))
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
	var keep func(string) bool
	if *filter != "" {
		keep = shellTest(*filter)
	}
	picks := core.Select(projects, directory, keep)
	width := nameWidth(picks)

	if dry {
		// A project that would run has no status yet, which is what the
		// summary counts it by.
		results := make([]runlog.Result, len(picks))
		for i, p := range picks {
			word, detail := "would run", p.Project.Path
			if p.Skip != "" {
				results[i] = skipped(p)
				word, detail = resultLine(results[i], "")
			}
			printLine(w, width, p.Project.Name, word, detail)
		}
		printRunSummary(w, results, "")
		return nil
	}

	stateRoot, err := state.Root()
	if err != nil {
		return err
	}
	r, err := runlog.Start(stateRoot, argv, *filter, time.Now())
	if err != nil {
		return err
	}
	for _, p := range picks {
		// The name goes out before the command starts, so a prompt it
		// writes to the terminal says which project is asking.
		_, _ = fmt.Fprintf(w, "%-*s  ", width, p.Project.Name)
		res := skipped(p)
		if p.Skip == "" {
			res = runIn(r, p.Project, argv)
		}
		r.Results = append(r.Results, res)
		logEachResult(res)
		word, detail := resultLine(res, r.Dir)
		printStatus(w, word, detail)
		if err := r.Save(); err != nil {
			return err
		}
	}
	r.Finished = time.Now()
	if err := r.Save(); err != nil {
		return err
	}
	printRunSummary(w, r.Results, r.Dir)
	if r.Count(runlog.StatusFailed) > 0 {
		return errEachFailed
	}
	return nil
}

// directory reports the real directory a project path names. Two paths
// resolving alike are one checkout, which is what core.Select dedupes on.
func directory(path string) (string, bool) {
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", false
	}
	info, err := os.Stat(real)
	return real, err == nil && info.IsDir()
}

// shellTest is the --filter: the snippet run by sh in the directory, keeping it
// on exit 0. What it prints is not kept; it is a question, not a command.
func shellTest(test string) func(dir string) bool {
	return func(dir string) bool {
		c := exec.Command("sh", "-c", test)
		c.Dir = dir
		return c.Run() == nil
	}
}

// runIn runs argv in the project's directory with its output in the run's
// file for the project. Stdin is empty, so a command that waits for input ends
// instead of waiting on a terminal nobody is watching for it. There is no
// deadline: a pull runs as long as it runs, as an action does.
func runIn(r *runlog.Run, p core.Project, argv []string) runlog.Result {
	res := runlog.Result{Project: p.Name, Path: p.Path, Status: runlog.StatusFailed}
	out, name, err := r.Output(p.Name)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	defer func() { _ = out.Close() }()
	res.Output = name

	c := exec.Command(argv[0], argv[1:]...)
	c.Dir = p.Path
	c.Stdout, c.Stderr = out, out
	start := time.Now()
	err = c.Run()
	res.Seconds = time.Since(start).Seconds()

	var exit *exec.ExitError
	switch {
	case err == nil:
		res.Status = runlog.StatusOK
	case errors.As(err, &exit) && exit.ExitCode() >= 0:
		res.Exit = exit.ExitCode()
	default:
		// It never started, or a signal ended it: there is no exit status,
		// only the reason.
		res.Error = err.Error()
	}
	return res
}

// logEachResult is one project's line of a run. The run's own record holds its
// output; the log holds only how it ended, beside every other operation.
func logEachResult(res runlog.Result) {
	attrs := []any{"project", res.Project, "status", res.Status, "exit", res.Exit, "seconds", res.Seconds, "skip", res.Skip}
	if res.Status == runlog.StatusFailed {
		slog.Error("each project", append(attrs, "err", res.Error, "output", res.Output)...)
		return
	}
	slog.Info("each project", attrs...)
}

func skipped(p core.Pick) runlog.Result {
	return runlog.Result{Project: p.Project.Name, Path: p.Project.Path, Status: runlog.StatusSkipped, Skip: p.Skip, SameAs: p.SameAs}
}

// resultLine is the status word and the detail printed for one project. dir is
// the run's directory, so a failure names the file that says why.
func resultLine(res runlog.Result, dir string) (string, string) {
	switch res.Status {
	case runlog.StatusOK:
		return "ok", fmt.Sprintf("%.1fs", res.Seconds)
	case runlog.StatusFailed:
		why := fmt.Sprintf("exit %d", res.Exit)
		if res.Error != "" {
			why = res.Error
		}
		if res.Output == "" {
			return "failed", why
		}
		return "failed", fmt.Sprintf("%s, %.1fs, %s", why, res.Seconds, filepath.Join(dir, res.Output))
	}
	switch res.Skip {
	case core.SkipMissing:
		return "missing", res.Path
	case core.SkipRemote:
		return "remote", res.Path
	case core.SkipDuplicate:
		return "duplicate", "same directory as " + string(res.SameAs)
	}
	return string(res.Skip), ""
}

func nameWidth(picks []core.Pick) int {
	width := 0
	for _, p := range picks {
		width = max(width, len(p.Project.Name))
	}
	return width
}

func printLine(w io.Writer, width int, name revier.ProjectName, word, detail string) {
	_, _ = fmt.Fprintf(w, "%-*s  ", width, name)
	printStatus(w, word, detail)
}

func printStatus(w io.Writer, word, detail string) {
	_, _ = fmt.Fprintln(w, strings.TrimRight(fmt.Sprintf("%-9s  %s", word, detail), " "))
}

// printRunSummary counts the run and names every project that failed or had no
// directory: the two a user has to do something about. dir is empty for a dry
// run, which has no output to point at.
func printRunSummary(w io.Writer, results []runlog.Result, dir string) {
	var ok, failed, skipped int
	var failedNames, missingNames []string
	for _, res := range results {
		switch res.Status {
		case runlog.StatusOK:
			ok++
		case runlog.StatusFailed:
			failed++
			failedNames = append(failedNames, string(res.Project))
		case runlog.StatusSkipped:
			skipped++
			if res.Skip == core.SkipMissing {
				missingNames = append(missingNames, string(res.Project))
			}
		}
	}
	if dir == "" {
		_, _ = fmt.Fprintf(w, "\n%d would run, %d skipped\n", len(results)-skipped, skipped)
	} else {
		_, _ = fmt.Fprintf(w, "\n%d ok, %d failed, %d skipped\n", ok, failed, skipped)
	}
	if len(failedNames) > 0 {
		_, _ = fmt.Fprintf(w, "failed:   %s\n", strings.Join(failedNames, ", "))
	}
	if len(missingNames) > 0 {
		_, _ = fmt.Fprintf(w, "missing:  %s\n", strings.Join(missingNames, ", "))
	}
	if dir != "" {
		_, _ = fmt.Fprintf(w, "output:   %s\n", dir)
	}
}

// cmdEachLog reads past runs back: the list, one run, or one project's output.
func cmdEachLog(w io.Writer, args []string) error {
	stateRoot, err := state.Root()
	if err != nil {
		return err
	}
	switch len(args) {
	case 0:
		runs, err := runlog.List(stateRoot)
		if err != nil {
			return err
		}
		if len(runs) == 0 {
			_, _ = fmt.Fprintf(w, "no runs yet under %s\n", runlog.Dir(stateRoot))
			return nil
		}
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		_, _ = fmt.Fprintln(tw, "RUN\tOK\tFAILED\tSKIPPED\tCOMMAND")
		for _, r := range runs {
			_, _ = fmt.Fprintf(tw, "%s\t%d\t%d\t%d\t%s\n", r.ID,
				r.Count(runlog.StatusOK), r.Count(runlog.StatusFailed), r.Count(runlog.StatusSkipped), runCommand(r))
		}
		return tw.Flush()
	case 1:
		r, err := runlog.Load(stateRoot, args[0])
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(w, "%s  %s\n\n", r.ID, runCommand(r))
		width := 0
		for _, res := range r.Results {
			width = max(width, len(res.Project))
		}
		for _, res := range r.Results {
			word, detail := resultLine(res, r.Dir)
			printLine(w, width, res.Project, word, detail)
		}
		printRunSummary(w, r.Results, r.Dir)
		return nil
	case 2:
		r, err := runlog.Load(stateRoot, args[0])
		if err != nil {
			return err
		}
		for _, res := range r.Results {
			if string(res.Project) != args[1] {
				continue
			}
			if res.Output == "" {
				return fmt.Errorf("%s did not run in %s: %s", args[1], r.ID, res.Skip)
			}
			f, err := os.Open(filepath.Join(r.Dir, res.Output))
			if err != nil {
				return err
			}
			defer func() { _ = f.Close() }()
			_, err = io.Copy(w, f)
			return err
		}
		return fmt.Errorf("no project %q in run %s", args[1], r.ID)
	default:
		return errors.New("usage: revier each log [<run> [<project>]]")
	}
}

// runCommand is how a run is named in a listing: what it ran, the filter that
// chose where, and whether it got to the end.
func runCommand(r runlog.Run) string {
	s := strings.Join(r.Argv, " ")
	if r.Filter != "" {
		s = fmt.Sprintf("--filter '%s' -- %s", r.Filter, s)
	}
	if r.Finished.IsZero() {
		s += "  (unfinished)"
	}
	return s
}
