package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A message of several lines carries its label on the first line only: the
// column says what the problem is about, and the lines under it continue it.
func TestAContinuationLineHasNoLabel(t *testing.T) {
	var out strings.Builder
	problemLine(&out, `target "editor"`, "", "the first line\nthe second line")
	if want := "  target \"editor\"\tthe first line\n  \tthe second line\n"; out.String() != want {
		t.Errorf("problemLine wrote %q, want %q", out.String(), want)
	}
}

const sound = `
path = "/tmp/sound"

[[target]]
name = "home"
home = true
  [target.runtime]
  name = "home"
  launch = ["sh"]
  match = { title = "^home$" }
`

// doctorRoot writes a configuration root and points revier at it.
func doctorRoot(t *testing.T, files map[string]string) {
	t.Helper()
	configRoot(t, "", files)
}

// doctor reports each problem under the file that holds it, and ends non-zero
// so a script can act on it. It is the one place the reasons are read in full,
// now that no other command refuses to run over them (decisions.md D85).
func TestDoctorReportsEachProblemUnderItsFile(t *testing.T) {
	doctorRoot(t, map[string]string{
		"sound.toml":    sound,
		"nohome.toml":   strings.Replace(sound, "home = true\n", "", 1),
		"bad.toml":      "path = \"/p\"\nthis is not toml\n",
		"template.toml": strings.Replace(sound, `launch = ["sh"]`, `launch = ["sh", "{{.Path)}}"]`, 1),
	})

	var out strings.Builder
	err := cmdDoctor(&out, nil)
	if !errors.Is(err, errSilent) {
		t.Fatalf("err = %v, want the silent non-zero end", err)
	}
	got := out.String()

	for _, want := range []string{
		"nohome.toml",
		"no target is marked home",
		"fix: mark the workspace target `home = true`",
		"bad.toml",
		"line 2",
		"fix: the file is not valid TOML",
		"template.toml",
		"fix: fix the template",
		"4 projects, 3 files with problems",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("report should contain %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "sound.toml") {
		t.Errorf("a project that loaded whole should not be reported:\n%s", got)
	}
	// A template that does not parse says "unexpected", which is not the
	// decoder's "expected": the advice names the template, not the TOML.
	if strings.Count(got, "not valid TOML") != 1 {
		t.Errorf("the TOML advice should be given once, for bad.toml alone:\n%s", got)
	}
}

// With nothing wrong, doctor ends clean, so it can gate a script.
func TestDoctorEndsCleanWhenEverythingLoads(t *testing.T) {
	doctorRoot(t, map[string]string{"sound.toml": sound})

	var out strings.Builder
	if err := cmdDoctor(&out, nil); err != nil {
		t.Fatalf("cmdDoctor: %v", err)
	}
	if !strings.Contains(out.String(), "1 project, 0 files with problems") {
		t.Errorf("report = %q", out.String())
	}
}

// A link to this machine is left out of every load, and doctor says how many
// it left and which files: a missing project is otherwise hard to explain. It
// is no problem, since the link is right on the other machine.
func TestDoctorNamesTheLinksToThisMachineItSkipped(t *testing.T) {
	machine, err := os.Hostname()
	if err != nil {
		t.Skip("no hostname:", err)
	}
	t.Setenv("REVIER_SSH_CONFIG", filepath.Join(t.TempDir(), "none"))
	doctorRoot(t, map[string]string{"sound.toml": sound, "self.toml": "[remote]\nhost = \"" + machine + "\"\n"})

	var out strings.Builder
	if err := cmdDoctor(&out, nil); err != nil {
		t.Fatalf("cmdDoctor: %v", err)
	}
	self := filepath.Join(os.Getenv("REVIER_CONFIG_HOME"), "projects", "self.toml")
	for _, want := range []string{"1 project, 0 files with problems", "skipped 1 link to this machine:\n  " + self + "\n"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("report should contain %q:\n%s", want, out.String())
		}
	}
}

// A mistake in config.toml is reported under that file, as a project's is
// under its own, and costs only what it names: the projects still load.
func TestDoctorReportsConfigProblemsUnderTheirFile(t *testing.T) {
	doctorRoot(t, map[string]string{"sound.toml": sound})
	root := os.Getenv("REVIER_CONFIG_HOME")
	if err := os.WriteFile(filepath.Join(root, "config.toml"), []byte("[[action]]\nkey = \"y\"\nname = \"sync\"\nrun = [\"true\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out strings.Builder
	if err := cmdDoctor(&out, nil); !errors.Is(err, errSilent) {
		t.Fatalf("err = %v, want the silent non-zero end", err)
	}
	for _, want := range []string{"config.toml", `action "sync"`, "typed text", "1 project, 1 file with problems"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("report should contain %q:\n%s", want, out.String())
		}
	}
}

// A refused [[status]] table is reported under the file that writes it, and
// so is a pane status a project changes that still runs nothing: doctor is
// where both are read (decisions.md D134).
func TestDoctorReportsStatusProblemsUnderTheirFile(t *testing.T) {
	doctorRoot(t, map[string]string{
		"sound.toml": sound,
		"tables.toml": sound + `
[[status]]
at = "rule-left"
run = ["true"]

[[status]]
at = "project-line"
every = "10s"
`,
	})
	root := os.Getenv("REVIER_CONFIG_HOME")
	twoTables := "[[status]]\nat = \"rule-right\"\nrun = [\"date\"]\n\n[[status]]\nat = \"rule-right\"\nrun = [\"uptime\"]\n"
	if err := os.WriteFile(filepath.Join(root, "config.toml"), []byte(twoTables), 0o644); err != nil {
		t.Fatal(err)
	}

	var out strings.Builder
	if err := cmdDoctor(&out, nil); !errors.Is(err, errSilent) {
		t.Fatalf("err = %v, want the silent non-zero end", err)
	}
	got := out.String()

	// config.toml is reported first, so the report splits at the project's
	// heading into what each file holds.
	ofConfig, ofProject, ok := strings.Cut(got, filepath.Join(root, "projects", "tables.toml"))
	if !ok {
		t.Fatalf("report should name tables.toml:\n%s", got)
	}
	for _, want := range []string{
		"config.toml",
		`status "rule-right"`,
		"two tables stand at this place",
		"fix: delete one of the two tables",
	} {
		if !strings.Contains(ofConfig, want) {
			t.Errorf("config.toml's report should contain %q:\n%s", want, got)
		}
	}
	for _, want := range []string{
		`status "rule-left"`,
		"only config.toml declares it",
		`status "project-line"`,
		"runs nothing",
	} {
		if !strings.Contains(ofProject, want) {
			t.Errorf("tables.toml's report should contain %q:\n%s", want, got)
		}
	}
	if !strings.Contains(got, "2 projects, 2 files with problems") {
		t.Errorf("report should count config.toml and tables.toml alone:\n%s", got)
	}
}
