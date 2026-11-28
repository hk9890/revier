package main

import (
	"flag"
	"fmt"
	"io"
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
		_, _ = fmt.Fprintln(w, "no project has a tag. add them under [tags] in a project file")
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
