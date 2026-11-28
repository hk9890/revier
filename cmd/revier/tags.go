package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/plain"
	"github.com/hk9890/revier/pkg/revier"
)

// tagFlags is a selector given once per tag: -t type=work -t language=go
// selects the projects that carry both.
type tagFlags []core.Tag

func (f *tagFlags) String() string { return strings.Join(f.words(), " ") }

// words is each tag as the command line wrote it.
func (f tagFlags) words() []string {
	words := make([]string, len(f))
	for i, t := range f {
		words[i] = t.String()
	}
	return words
}

func (f *tagFlags) Set(s string) error {
	t, err := core.ParseTag(s)
	if err != nil {
		return err
	}
	*f = append(*f, t)
	return nil
}

func (f *tagFlags) register(fs *flag.FlagSet) {
	fs.Var(f, "tag", "only the projects with this key=value tag; given again, with each")
	fs.Var(f, "t", "only the projects with this key=value tag (shorthand)")
}

// tagUse is one tag as `revier tags --json` gives it.
type tagUse struct {
	Key      string               `json:"key"`
	Value    string               `json:"value"`
	Projects []revier.ProjectName `json:"projects"`
}

// cmdTags prints every tag the project files carry, with the projects that
// carry each: what a -t selector can name. It reads the files alone and asks
// no host.
func cmdTags(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("tags", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "emit the tags as JSON")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 0 {
		return fmt.Errorf("usage: revier tags [--json]")
	}
	root, err := config.Root()
	if err != nil {
		return err
	}
	cfg, projects, err := config.Load(root)
	if err != nil {
		return err
	}
	warnProblems(cfg, projects)
	uses := core.TagsInUse(projects)
	if *asJSON {
		out := make([]tagUse, len(uses))
		for i, u := range uses {
			out[i] = tagUse{Key: u.Key, Value: u.Value, Projects: u.Projects}
		}
		return printJSON(w, out)
	}
	if len(uses) == 0 {
		_, _ = fmt.Fprintln(w, "no project has a tag. add one with revier tag key=value")
		return nil
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "TAG\tCOUNT\tPROJECTS")
	for _, u := range uses {
		names := make([]string, len(u.Projects))
		for i, n := range u.Projects {
			names[i] = string(n)
		}
		// A tab in a value would move every column after it.
		_, _ = fmt.Fprintf(tw, "%s\t%d\t%s\n", plain.Line(u.String()), len(u.Projects), strings.Join(names, " "))
	}
	return tw.Flush()
}

// cmdTag sets, removes or prints the tags of the project of this directory,
// of the one -p names, or of every project -t and --match select. It writes
// the project files alone and asks no host. A directory in no project is an
// error, never the last project or the focused window's: a tag written to the
// wrong project would select it silently from then on.
func cmdTag(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("tag", flag.ContinueOnError)
	project := projectFlag(fs)
	var selector tagFlags
	selector.register(fs)
	// An empty word is refused: read as no --match, a script's empty
	// variable would tag the project the directory resolves to.
	var word string
	match := &word
	fs.Func("match", "every project whose name, directory, repository or name on a link's host holds this word", func(value string) error {
		if value == "" {
			return errors.New("it names no word")
		}
		word = value
		return nil
	})
	var remove []string
	fs.Func("remove", "remove the tag with this key; given again, each", func(key string) error {
		if err := config.ValidateTagKey(key); err != nil {
			return err
		}
		remove = append(remove, key)
		return nil
	})
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	var set []core.Tag
	for _, a := range pos {
		t, err := core.ParseTag(a)
		if err != nil {
			return fmt.Errorf("usage: revier tag [key=value].. [--remove key]..: %w", err)
		}
		if err := config.ValidateTagKey(t.Key); err != nil {
			return err
		}
		if slices.Contains(remove, t.Key) {
			return fmt.Errorf("tag %q is both set and removed", t.Key)
		}
		if i := slices.IndexFunc(set, func(s core.Tag) bool { return s.Key == t.Key }); i >= 0 && set[i].Value != t.Value {
			return fmt.Errorf("tag %q is set twice, to %q and %q", t.Key, set[i].Value, t.Value)
		}
		set = append(set, t)
	}
	selects := len(selector) > 0 || *match != ""
	if *project != "" && selects {
		return errors.New("usage: revier tag -p <name> takes no -t and no --match")
	}

	root, err := config.Root()
	if err != nil {
		return err
	}
	cfg, projects, err := config.Load(root)
	if err != nil {
		return err
	}
	warnProblems(cfg, projects)
	picks, err := tagPicks(projects, *project, selector, *match, selects)
	if err != nil {
		return err
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	defer func() { _ = tw.Flush() }()
	if len(set) == 0 && len(remove) == 0 {
		for _, p := range picks {
			tagLine(tw, p)
		}
		return nil
	}
	failed := 0
	for _, p := range picks {
		written, err := config.WriteTags(p.File, set, remove, cfg.Inherited())
		if err != nil {
			fmt.Fprintf(os.Stderr, "revier: %s: %v\n", p.Name, err)
			failed++
			continue
		}
		tagLine(tw, written)
	}
	if failed > 0 {
		return fmt.Errorf("%s not written", core.Count(failed, "project"))
	}
	return nil
}

// tagPicks is the projects a tag command acts on: those -t and --match select
// together, else the one -p names, else the one this directory is in. The
// match reads the project files alone, as the rest of the command does: a
// word in an agent's activity finds a project for list --match, not here.
func tagPicks(projects []core.Project, name string, selector []core.Tag, match string, selects bool) ([]core.Project, error) {
	if !selects {
		ask := core.Ask{Name: revier.ProjectName(name)}
		if name == "" {
			ask.Dir = workingDir()
		}
		p, err := (&core.Core{}).ProjectFor(context.Background(), projects, ask)
		if err != nil {
			return nil, err
		}
		return []core.Project{p}, nil
	}
	picks := core.WithTags(projects, selector)
	if match != "" {
		picks = slices.DeleteFunc(picks, func(p core.Project) bool { return !core.Matches(revier.ProjectView{Project: p.Project}, match) })
	}
	if len(picks) == 0 {
		return nil, errors.New("no project matches the selection")
	}
	return picks, nil
}

// tagLine is one project and its tags, by key.
func tagLine(w io.Writer, p core.Project) {
	words := []string{}
	for _, t := range core.SortedTags(p.Tags) {
		words = append(words, plain.Line(t.String()))
	}
	_, _ = fmt.Fprintf(w, "%s\t%s\n", p.Label(), dash(strings.Join(words, " ")))
}
