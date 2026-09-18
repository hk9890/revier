package main

import (
	"errors"
	"fmt"
	"io"
	"strings"

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

	bad := 0
	for _, p := range projects {
		probs := config.Problems(p)
		if len(probs) == 0 {
			continue
		}
		bad++
		_, _ = fmt.Fprintf(out, "\n%s\n", p.File)
		if p.Invalid != nil {
			problemLine(out, "project", p.Invalid.Error())
		}
		for i := range p.Targets {
			if err := p.TargetErr(i); err != nil {
				// The column names the target, so the message need not
				// repeat it; every target refusal opens with its own name.
				what := fmt.Sprintf("target %q", p.Targets[i].Name)
				problemLine(out, what, strings.ReplaceAll(err.Error(), what+": ", ""))
			}
		}
	}

	_, _ = fmt.Fprintf(out, "\n%d project(s), %d with problems\n", len(projects), bad)
	if bad > 0 {
		// Exit 1, so a script that checks the configuration can act on it.
		// Nothing is printed by the caller: the report above is the message.
		return errSilent
	}
	return nil
}

// problemLine prints one problem and its advice. The file path is already the
// heading, so it is stripped from the message.
func problemLine(out io.Writer, what, msg string) {
	for _, line := range strings.Split(msg, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		_, _ = fmt.Fprintf(out, "  %-14s %s\n", what, trimPath(line))
		if fix := advise(line); fix != "" {
			_, _ = fmt.Fprintf(out, "  %-14s fix: %s\n", "", fix)
		}
		what = ""
	}
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
	case strings.Contains(msg, "expected"), strings.Contains(msg, "toml:"):
		return "the file is not valid TOML; the message names the line"
	}
	return ""
}

// errSilent ends a command with a non-zero status and no further message,
// because the command has printed its own report.
var errSilent = errors.New("")
