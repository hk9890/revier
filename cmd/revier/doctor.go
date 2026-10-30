package main

import (
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
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
	cfg, projects, err := config.Load(root)
	if err != nil {
		return err
	}

	// A tabwriter, as `revier list` uses, so the column is as wide as the
	// longest label under it. A fixed width fits `target "web"` and not
	// `target "worktree"`, and a target name has no length limit.
	w := tabwriter.NewWriter(out, 0, 0, 1, ' ', 0)
	bad := 0
	if problems := append(hostProblems(cfg), cfg.Problems...); len(problems) > 0 {
		bad++
		_, _ = fmt.Fprintf(out, "\n%s\n", config.File(root))
		for _, err := range problems {
			what := subject(err.Error())
			problemLine(w, what, what, err.Error())
		}
	}
	for _, p := range projects {
		if len(config.Problems(p)) == 0 {
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
		for _, err := range slices.Concat(p.StatusRefused, p.AgentRefused) {
			what := subject(err.Error())
			problemLine(w, what, what, err.Error())
		}
	}
	_ = w.Flush()

	_, _ = fmt.Fprintf(out, "\n%s, %s with problems\n", core.Count(len(projects), "project"), core.Count(bad, "file"))
	// Not a problem: the files are shared between machines, and the link is
	// right on the other one (decisions.md D131).
	if n := len(cfg.SelfLinks); n > 0 {
		_, _ = fmt.Fprintf(out, "skipped %s to this machine:\n", core.Count(n, "link"))
		for _, file := range cfg.SelfLinks {
			_, _ = fmt.Fprintf(out, "  %s\n", file)
		}
	}
	// Not a problem either: the link works, and the remote screen does not
	// list its machine (decisions.md D154).
	if off := offList(cfg, projects); len(off) > 0 {
		_, _ = fmt.Fprintf(out, "%s to a machine not on the remote screen (a link works without it):\n", core.Count(len(off), "link"))
		for _, p := range off {
			_, _ = fmt.Fprintf(out, "  %s (%s)\n", p.File, p.Remote.Host)
		}
	}
	if bad > 0 {
		// Exit 1, so a script that checks the configuration can act on it.
		// Nothing is printed by the caller: the report above is the message.
		return problemsFound{files: bad}
	}
	return nil
}

// offList is the links whose host no [[machine]] table of config.toml names.
func offList(cfg *config.Config, projects []core.Project) []core.Project {
	var out []core.Project
	for _, p := range projects {
		if p.Remote != nil && !cfg.Declares(p.Remote.Host) {
			out = append(out, p)
		}
	}
	return out
}

// subject is what a refusal of config.toml or of a status table is about,
// which is the column: the opening up to the name it quotes, as in `action
// "c"`, or else up to the first colon. Some refusals put a colon after the
// name and some do not, `action "c": key ...` and `action "c" runs nothing`,
// so a cut at the colon alone took the whole of the second as the column.
func subject(msg string) string {
	kind, rest, _ := strings.Cut(msg, " ")
	if name, err := strconv.QuotedPrefix(rest); err == nil {
		return kind + " " + name
	}
	what, _, _ := strings.Cut(msg, ":")
	return what
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
	case strings.Contains(msg, "two tables stand at this place"):
		return "delete one of the two tables"
	case strings.Contains(msg, "declared twice"):
		return "rename one of the two, or delete the duplicate table"
	case strings.Contains(msg, "share key"):
		return "give one of the two another key, or leave it without one"
	case strings.Contains(msg, "runtime realization has no name"):
		return "give the runtime realization the `name` its match finds"
	case strings.Contains(msg, "project has no path"):
		return "add `path = \"...\"`, or `[remote]` if the project lives on another machine"
	case strings.Contains(msg, "declares no realization under [target.remote"):
		return "give the target a `[target.remote.window]` or `[target.remote.runtime]` table"
	case strings.Contains(msg, "declares no realization"):
		return "give the target a `[target.window]` or `[target.runtime]` table"
	case strings.Contains(msg, "error parsing regexp"):
		return "fix the `match` pattern; the message names the part that does not parse"
	case strings.Contains(msg, "template:"):
		return "fix the template; the message names what does not parse or evaluate"
	case strings.Contains(msg, "name is not read"):
		return "delete the `name = ...` line; the file name is the project's name"
	case strings.Contains(msg, "incompatible types"):
		// The decoder's line prefix is on this message too, and the file
		// is valid TOML: only the value is of the wrong type.
		return "give the key a value of the type the message names, for example a string in quotes"
	case strings.Contains(msg, "toml: line"):
		return "the file is not valid TOML; the message names the line"
	}
	return ""
}

// problemsFound is a doctor run that found files with problems. It ends the
// command with status 1 and no further message, because the report is the
// message, and it is logged as the outcome it is and not as a failure.
type problemsFound struct{ files int }

func (e problemsFound) Error() string {
	return core.Count(e.files, "file") + " with problems"
}
