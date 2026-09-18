package main

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/hk9890/revier/internal/config"
)

// cmdDoctor reports every project file that did not load whole, and what to do
// about each problem.
//
// It exists because loading stopped refusing. A refusal used to be unmissable:
// it was printed by whichever command the user ran next, and nothing ran until
// the file was fixed. Now a bad file costs only itself, which is the point, so
// there has to be one place that answers "what is wrong with my configuration"
// in full (decisions.md D85).
func cmdDoctor(out io.Writer, args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("usage: revier doctor")
	}
	root, err := config.Root()
	if err != nil {
		return err
	}
	_, projects, err := config.Load(root)
	if err != nil {
		return err
	}

	// A tabwriter, as `revier list` uses, so the column is as wide as the
	// longest label under it. A fixed width fits `target "web"` and not
	// `target "worktree"`, and a target name has no length limit.
	w := tabwriter.NewWriter(out, 0, 0, 1, ' ', 0)
	bad := 0
	for _, p := range projects {
		probs := config.Problems(p)
		if len(probs) == 0 {
			continue
		}
		bad++
		// Flushed per file, so one long target name does not widen the
		// column of every other file's report.
		_ = w.Flush()
		_, _ = fmt.Fprintf(out, "\n%s\n", p.File)
		if p.Invalid != nil {
			problemLine(w, "project", "", p.Invalid.Error())
		}
		for i := range p.Targets {
			if err := p.TargetErr(i); err != nil {
				// The column names the target, so the message need not
				// repeat it; every target refusal opens with its own name.
				what := fmt.Sprintf("target %q", p.Targets[i].Name)
				problemLine(w, what, what, err.Error())
			}
		}
	}
	_ = w.Flush()

	_, _ = fmt.Fprintf(out, "\n%d project(s), %d with problems\n", len(projects), bad)
	if bad > 0 {
		// Exit 1, so a script that checks the configuration can act on it.
		// Nothing is printed by the caller: the report above is the message.
		return errSilent
	}
	return nil
}

// problemLine prints one problem and its advice under a column naming what
// the problem is about. The file path is already the heading, so it is
// stripped from the message, and so is strip: the opening a target's
// refusals repeat, which the column has just printed.
func problemLine(out io.Writer, what, strip, msg string) {
	for _, line := range strings.Split(msg, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		_, _ = fmt.Fprintf(out, "  %s\t%s\n", what, trimName(strip, trimPath(line)))
		if fix := advise(line); fix != "" {
			_, _ = fmt.Fprintf(out, "  \tfix: %s\n", fix)
		}
		what = ""
	}
}

// trimName drops the opening a refusal repeats, with the colon that some of
// them carry and some do not: `target "web": launch[1]: ...` and `target
// "web" window realization has an empty match` are both the target's own
// name, and both would otherwise print it twice.
func trimName(strip, line string) string {
	if strip == "" {
		return line
	}
	if rest, ok := strings.CutPrefix(line, strip+": "); ok {
		return rest
	}
	if rest, ok := strings.CutPrefix(line, strip+" "); ok {
		return rest
	}
	return line
}

// trimPath drops the leading "<file>: " every loading error carries, because
// the report has already printed the file as its heading.
func trimPath(line string) string {
	if i := strings.Index(line, ".toml: "); i >= 0 {
		return line[i+len(".toml: "):]
	}
	return line
}

// advise is what to do about a problem. It matches on the message rather than
// on a typed error because these are the few refusals a user actually meets,
// and each one already says precisely what is wrong; the advice only says
// which edit answers it. A problem with no entry prints its message alone,
// which is what every command printed before this one existed.
func advise(msg string) string {
	switch {
	case strings.Contains(msg, "renders to an empty argument"):
		return "give the project the field the template reads, or drop the argument from the target"
	case strings.Contains(msg, "no target is marked home"):
		return "mark the workspace target `home = true`"
	case strings.Contains(msg, "is marked home, and an earlier target already is"):
		return "leave `home = true` on one target only"
	case strings.Contains(msg, "has an empty match"):
		return "give the realization a `match` that names the instance it finds"
	case strings.Contains(msg, "declared twice"):
		return "rename one of the two, or delete the duplicate table"
	case strings.Contains(msg, "share key"):
		return "give one of the two another key, or leave it without one"
	case strings.Contains(msg, "runtime realization has no name"):
		return "give the runtime realization the `name` its match finds"
	case strings.Contains(msg, "project has no path"):
		return "add `path = \"...\"`, or `[remote]` if the project lives on another machine"
	case strings.Contains(msg, "declares no realization"):
		return "give the target a `[target.window]` or `[target.runtime]` table"
	case strings.Contains(msg, "error parsing regexp"):
		return "fix the `match` pattern; the message names the part that does not parse"
	case strings.Contains(msg, "template:"):
		return "fix the template; the message names what does not parse or evaluate"
	case strings.Contains(msg, "name is not read"):
		return "delete the `name = ...` line; the file name is the project's name"
	case strings.Contains(msg, "toml: line"):
		return "the file is not valid TOML; the message names the line"
	}
	return ""
}

// errSilent ends a command with a non-zero status and no further message,
// because the command has printed its own report.
var errSilent = errors.New("")
