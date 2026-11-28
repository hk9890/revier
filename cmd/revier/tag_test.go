package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/config"
)

// tagScratch is a configuration with the projects alpha and beta, each in a
// directory of its own that exists, and returns alpha's directory.
func tagScratch(t *testing.T) string {
	t.Helper()
	work := t.TempDir()
	alpha := filepath.Join(work, "alpha")
	eachScratch(t, map[string]string{"alpha": alpha, "beta": filepath.Join(work, "beta")}, "alpha", "beta")
	return alpha
}

func projectFileText(t *testing.T, name string) string {
	t.Helper()
	root, err := config.Root()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(config.ProjectsDir(root), name+".toml"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func tagRun(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out strings.Builder
	err := cmdTag(&out, args)
	return out.String(), err
}

// With no -p, the tags go to the project of the directory the command runs
// in, and the line it prints is that project's tags after the write.
func TestTagWritesTheProjectOfThisDirectory(t *testing.T) {
	t.Chdir(tagScratch(t))
	out, err := tagRun(t, "type=private", "language=go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "alpha  language=go type=private") {
		t.Errorf("revier tag = %q, want alpha's tags", out)
	}
	if text := projectFileText(t, "alpha"); !strings.Contains(text, "[tags]\ntype = \"private\"\nlanguage = \"go\"\n") {
		t.Errorf("alpha.toml =\n%s", text)
	}
	if text := projectFileText(t, "beta"); strings.Contains(text, "[tags]") {
		t.Errorf("beta.toml got tags:\n%s", text)
	}

	out, err = tagRun(t, "--remove", "language")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "alpha  type=private") || strings.Contains(projectFileText(t, "alpha"), "language") {
		t.Errorf("revier tag --remove language = %q, file\n%s", out, projectFileText(t, "alpha"))
	}

	// With nothing to write it prints the tags.
	if out, err = tagRun(t); err != nil || !strings.Contains(out, "alpha  type=private") {
		t.Errorf("revier tag = %q, %v", out, err)
	}
}

// A directory in no project is an error: the tag never goes to some other
// project.
func TestTagInADirectoryOfNoProjectFails(t *testing.T) {
	tagScratch(t)
	t.Chdir(t.TempDir())
	if _, err := tagRun(t, "type=work"); err == nil {
		t.Error("revier tag outside every project: want an error")
	}
}

func TestTagPNamesTheProject(t *testing.T) {
	t.Chdir(tagScratch(t))
	if _, err := tagRun(t, "-p", "beta", "type=work"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(projectFileText(t, "beta"), "type = \"work\"") || strings.Contains(projectFileText(t, "alpha"), "[tags]") {
		t.Errorf("revier tag -p beta wrote the wrong file")
	}
}

// -t and --match select every project to write; -p goes with neither.
func TestTagSelectorWritesEverySelectedProject(t *testing.T) {
	t.Chdir(tagScratch(t))
	for _, args := range [][]string{{"-p", "alpha", "type=work"}, {"-p", "beta", "type=work"}} {
		if _, err := tagRun(t, args...); err != nil {
			t.Fatal(err)
		}
	}
	out, err := tagRun(t, "-t", "type=work", "stage=prod")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "alpha  stage=prod type=work") || !strings.Contains(out, "beta   stage=prod type=work") {
		t.Errorf("revier tag -t type=work stage=prod = %q", out)
	}
	if out, err = tagRun(t, "--match", "bet", "--remove", "stage"); err != nil || !strings.Contains(out, "beta  type=work") || strings.Contains(out, "alpha") {
		t.Errorf("revier tag --match bet --remove stage = %q, %v", out, err)
	}
	if _, err := tagRun(t, "-p", "alpha", "-t", "type=work"); err == nil || !strings.HasPrefix(err.Error(), "usage:") {
		t.Errorf("revier tag -p with -t: err = %v, want a usage error", err)
	}
}

func TestTagRefusesAMalformedOrContradictoryTag(t *testing.T) {
	t.Chdir(tagScratch(t))
	for _, args := range [][]string{{"type"}, {"a b=c"}, {"type=work", "--remove", "type"}, {"--remove", ""}} {
		if _, err := tagRun(t, args...); err == nil {
			t.Errorf("revier tag %q: want an error", args)
		}
	}
	if strings.Contains(projectFileText(t, "alpha"), "[tags]") {
		t.Error("a refused tag command wrote the file")
	}
}
