package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/runlog"
	"github.com/hk9890/revier/pkg/revier"
)

// taggedApp is listApp with tm tagged type=work and language=go, and demo
// untagged.
func taggedApp(t *testing.T) *app {
	t.Helper()
	a := listApp(t)
	for i := range a.projects {
		if a.projects[i].Name == "tm" {
			a.projects[i].Tags = map[string]string{"type": "work", "language": "go"}
		}
	}
	return a
}

func TestListTagIsTheProjectsThatCarryEveryTag(t *testing.T) {
	a := taggedApp(t)
	for _, args := range [][]string{
		{"-t", "type=work"},
		{"--tag", "type=work", "-t", "language=go"},
	} {
		out := output(t, a, func() error { return cmdList(t.Context(), a, args) })
		if !strings.Contains(out, "\ntm ") || strings.Contains(out, "demo") {
			t.Errorf("list %v = %q, want tm alone", args, out)
		}
	}
	// The JSON carries the tags, so a script can select by them itself.
	out := output(t, a, func() error { return cmdList(t.Context(), a, []string{"--json", "-t", "type=work"}) })
	var views []revier.ProjectView
	if err := json.Unmarshal([]byte(out), &views); err != nil {
		t.Fatal(err)
	}
	if len(views) != 1 || views[0].Project.Tags["language"] != "go" {
		t.Errorf("list --json -t type=work = %+v, want tm with its tags", views)
	}
}

func TestListTagThatNoProjectCarriesFails(t *testing.T) {
	a := taggedApp(t)
	err := cmdList(t.Context(), a, []string{"-t", "type=work", "-t", "language=ts"})
	if err == nil || err.Error() != "no project has the tags type=work language=ts" {
		t.Errorf("err = %v, want no project has the tags", err)
	}
}

func TestListTagRefusesNamesAndAMalformedTag(t *testing.T) {
	a := taggedApp(t)
	if err := cmdList(t.Context(), a, []string{"-t", "type=work", "tm"}); err == nil || !strings.HasPrefix(err.Error(), "usage:") {
		t.Errorf("list -t with a name: err = %v, want a usage error", err)
	}
	if err := cmdList(t.Context(), a, []string{"-t", "work"}); err == nil || !strings.Contains(err.Error(), "key=value") {
		t.Errorf("list -t work: err = %v, want the tag's form", err)
	}
}

// A link's tags are its own file's: the host is not asked for them.
const taggedLinkTOML = `[remote]
host = "far"
[tags]
type = "work"
`

// revier tags reads the files alone: a project file and a link file, and
// the table and the JSON both say which projects carry each tag.
func TestTagsListsEveryTagWithItsProjects(t *testing.T) {
	configRoot(t, "", map[string]string{
		"shop.toml": fmt.Sprintf("path = %q\n[tags]\ntype = \"work\"\nlanguage = \"go\"\n", t.TempDir()),
		"blog.toml": fmt.Sprintf("path = %q\n[tags]\ntype = \"private\"\nlanguage = \"go\"\n", t.TempDir()),
		"far.toml":  taggedLinkTOML,
	})
	t.Setenv("REVIER_STATE_HOME", t.TempDir())

	var out strings.Builder
	if err := cmdTags(&out, nil); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"TAG", "language=go   2      blog shop", "type=private  1      blog", "type=work     2      far shop"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("revier tags does not print %q:\n%s", want, out.String())
		}
	}

	out.Reset()
	if err := cmdTags(&out, []string{"--json"}); err != nil {
		t.Fatal(err)
	}
	var uses []tagUse
	if err := json.Unmarshal([]byte(out.String()), &uses); err != nil {
		t.Fatal(err)
	}
	if len(uses) != 3 || uses[2].Key != "type" || uses[2].Value != "work" || len(uses[2].Projects) != 2 {
		t.Errorf("revier tags --json = %+v", uses)
	}
}

func TestTagsWithNoneSaysWhereToAddThem(t *testing.T) {
	configRoot(t, "", map[string]string{"shop.toml": fmt.Sprintf("path = %q\n", t.TempDir())})
	t.Setenv("REVIER_STATE_HOME", t.TempDir())
	var out strings.Builder
	if err := cmdTags(&out, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "[tags]") {
		t.Errorf("revier tags with none = %q, want where to add them", out.String())
	}
}

// A project without the tag is not in the run at all: no line, not even a
// skipped one.
func TestEachTagRunsOnlyWhereEveryTagIs(t *testing.T) {
	work := t.TempDir()
	alpha, beta := filepath.Join(work, "alpha"), filepath.Join(work, "beta")
	eachScratch(t, map[string]string{"alpha": alpha, "beta": beta}, "alpha", "beta")
	appendFile(t, "alpha", "[tags]\ntype = \"work\"\n")

	var out strings.Builder
	if err := cmdEach(&out, []string{"-t", "type=work", "-n", "--", "true"}); err != nil {
		t.Fatalf("each -t: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "alpha") || strings.Contains(out.String(), "beta") {
		t.Errorf("each -t type=work -n = %q, want alpha alone", out.String())
	}
	if err := cmdEach(&out, []string{"-t", "type=private", "-n", "--", "true"}); err == nil {
		t.Error("each -t with no project tagged so: want an error")
	}
}

// appendFile adds text to the end of a project's file in the configuration
// root the test set.
func appendFile(t *testing.T, project, text string) {
	t.Helper()
	root, err := config.Root()
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(config.ProjectsDir(root), project+".toml")
	f, err := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if _, err := f.WriteString(text); err != nil {
		t.Fatal(err)
	}
}

func TestARunWithTagsIsNamedAsAShellReadsItBack(t *testing.T) {
	r := runlog.Run{Argv: []string{"git", "pull"}, Tags: []string{"type=work", "note=a b"}, Filter: "test -d .git"}
	want := `-t type=work -t 'note=a b' --filter 'test -d .git' -- git pull  (unfinished)`
	if got := runCommand(r); got != want {
		t.Errorf("runCommand = %s, want %s", got, want)
	}
}
