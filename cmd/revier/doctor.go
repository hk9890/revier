package main

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"
	"unicode"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/fsutil"
	"github.com/hk9890/revier/internal/shell"
	"github.com/hk9890/revier/internal/userunit"
	"github.com/hk9890/revier/pkg/revier"
)

// cmdDoctor reports every project file that did not load whole, and what to do
// about each problem. It also warns about project files that name one
// checkout, which split its work across two names.
//
// It exists because loading stopped refusing. A refusal used to be unmissable:
// it was printed by whichever command the user ran next, and nothing ran until
// the file was fixed. Now a bad file costs only itself, which is the point, so
// there has to be one place that answers "what is wrong with my configuration"
// in full (decisions/0085).
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
	if problems := slices.Concat(hostProblems(cfg), cfg.Problems, outsideProblems(cfg, projects)); len(problems) > 0 {
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
		for _, err := range slices.Concat(p.StatusRefused, p.TagRefused, p.AgentRefused) {
			what := subject(err.Error())
			problemLine(w, what, what, err.Error())
		}
	}
	_ = w.Flush()

	_, _ = fmt.Fprintf(out, "\n%s, %s with problems\n", core.Count(len(projects), "project"), core.Count(bad, "file"))
	ctx, cancel := context.WithTimeout(context.Background(), commandWait)
	defer cancel()
	// A selection that fails says nothing here: every other command reports
	// it.
	if rt, view, err := selectTerminal(ctx, cfg, cfg.Hosts.Runtime); err == nil && rt != nil {
		// Not a problem: under a runtime that draws nothing, which view
		// shows it depends on the machine, and nothing
		// else says so.
		if line := shownBy(rt, view); line != "" {
			_, _ = fmt.Fprintln(out, line)
		}
		// Not a problem either: the core runs while the user is logged in,
		// and the logout that ends the last session ends it.
		if line := coreLifetime(ctx, rt); line != "" {
			_, _ = fmt.Fprintln(out, line)
		}
	}
	// Not a problem: the files are shared between machines, and the link is
	// right on the other one.
	if n := len(cfg.SelfLinks); n > 0 {
		_, _ = fmt.Fprintf(out, "skipped %s to this machine:\n", core.Count(n, "link"))
		for _, file := range cfg.SelfLinks {
			_, _ = fmt.Fprintf(out, "  %s\n", file)
		}
	}
	// Not a problem either: the link works, and the remote screen does not
	// list its machine.
	if off := offList(cfg, projects); len(off) > 0 {
		_, _ = fmt.Fprintf(out, "%s to a machine not on the remote screen (a link works without it):\n", core.Count(len(off), "link"))
		for _, p := range off {
			_, _ = fmt.Fprintf(out, "  %s (%s)\n", p.File, p.Remote.Host)
		}
	}
	// Not a problem: a file of the assistant's name is the user's own
	// assistant, and alt+a raises it from then on.
	if i := slices.IndexFunc(projects, func(p core.Project) bool { return p.Name == config.Assistant }); i >= 0 {
		_, _ = fmt.Fprintf(out, "note: %s replaces the built-in config assistant; delete the file to bring it back\n", projects[i].File)
	}
	// A warning, not a problem: the exit status stays what the problems make
	// it.
	for _, d := range duplicates(projects) {
		_, _ = fmt.Fprintf(out, "warning: %s name %s; keep one file, delete the others:\n", core.Count(len(d.files), "project file"), d.what)
		for _, file := range d.files {
			_, _ = fmt.Fprintf(out, "  %s\n", file)
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

// duplicate is two or more project files that name one checkout: what they
// name, and the files.
type duplicate struct {
	what  string
	files []string
}

// duplicates is each group of local projects whose paths resolve to one
// directory, and each group of links that name one project on one host. A
// local project and a link are never compared: the path of a link is the
// path on its host, so the same text there is normal. A relative path is
// left out: the directory it names depends on where doctor runs.
func duplicates(projects []core.Project) []duplicate {
	type key struct {
		link revier.Link
		dir  string
	}
	var keys []key
	groups := map[key]*duplicate{}
	add := func(k key, what, file string) {
		g, ok := groups[k]
		if !ok {
			g = &duplicate{what: what}
			groups[k] = g
			keys = append(keys, k)
		}
		g.files = append(g.files, file)
	}
	for _, p := range projects {
		switch {
		case p.Remote != nil:
			add(key{link: *p.Remote}, fmt.Sprintf("one project, %s on %s", p.Remote.Project, p.Remote.Host), p.File)
		case filepath.IsAbs(p.Path):
			dir := fsutil.RealPath(p.Path)
			add(key{dir: dir}, "one directory, "+dir, p.File)
		}
	}
	var out []duplicate
	for _, k := range keys {
		if g := groups[k]; len(g.files) > 1 {
			out = append(out, *g)
		}
	}
	return out
}

// outsideProblems is each terminal key that fails at every press outside a
// workspace, because the agent it runs there cannot run in no project. kitty's
// own new tab hides the failure, so doctor says it; every other command does
// not, since the configuration loaded whole. A key `revier setup install` does
// not write is never pressed, so it is left out; with a trigger key that does
// not parse, install writes none.
func outsideProblems(cfg *config.Config, projects []core.Project) []error {
	trigger, err := cfg.TriggerKey()
	if err != nil {
		return nil
	}
	var errs []error
	for _, k := range core.InstalledTermKeys(cfg.TermKeys(), projects, trigger) {
		if k.Shell {
			continue
		}
		if err := cfg.OutsideKey(k.Agent).Refusal(); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", k.Label, err))
		}
	}
	return errs
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
	case strings.Contains(msg, `contains ":", which separates`),
		strings.Contains(msg, "contains whitespace or a control character"):
		return renameFix(msg)
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
	case strings.Contains(msg, "reads a project template, and no project holds"):
		return "drop the template from the agent's command, or set `outside` in [terminal] to \"tab\""
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

// renameFix is the rename that answers a refused project name, with the name
// taken from the message and quoted so that it copies into a shell as one
// word. The new name is the user's to choose. A name with a control
// character gets no fix: printed, its bytes would act on the terminal, and
// the line would not be the name. A name that starts with a dash follows
// `--`, so that it is not read as a flag.
func renameFix(msg string) string {
	_, rest, _ := strings.Cut(msg, `project name "`)
	quoted, err := strconv.QuotedPrefix(`"` + rest)
	if err != nil {
		return ""
	}
	name, _ := strconv.Unquote(quoted)
	if strings.IndexFunc(name, func(r rune) bool { return !unicode.IsPrint(r) }) >= 0 {
		return ""
	}
	if strings.HasPrefix(name, "-") {
		name = "-- " + shell.Quote(name)
	} else {
		name = shell.Quote(name)
	}
	return "rename the project: revier rename " + name + " <new name>"
}

// problemsFound is a doctor run that found files with problems. It ends the
// command with status 1 and no further message, because the report is the
// message, and it is logged as the outcome it is and not as a failure.
type problemsFound struct{ files int }

func (e problemsFound) Error() string {
	return core.Count(e.files, "file") + " with problems"
}

// shownBy says which view shows a runtime that does not draw itself, as
// [hosts] selects them on this machine, and "" for a runtime that is its own
// view: a kitty user reads nothing new.
func shownBy(rt revier.Runtime, view revier.View) string {
	if own, ok := rt.(revier.View); ok && own == view {
		return ""
	}
	if view == nil {
		line := fmt.Sprintf("runtime %s is shown by no view: nothing is focused", rt.Name())
		if _, ok := rt.(revier.Attacher); ok {
			line += "; revier open --attach puts a terminal on a workspace"
		}
		return line
	}
	return fmt.Sprintf("runtime %s is shown by the %s view", rt.Name(), view.Name())
}

// coreLifetime says that the hc core ends with the user's last login
// session, and every agent in it, where hc is the runtime in use and the
// user's service manager, which runs the core, does not linger; "" otherwise.
func coreLifetime(ctx context.Context, rt revier.Runtime) string {
	if rt.Name() != "hc" {
		return ""
	}
	if lingers, known := userunit.Outlives(ctx); !known || lingers {
		return ""
	}
	return "the hc core stops at your last logout, and every agent with it; run loginctl enable-linger to keep it running"
}
