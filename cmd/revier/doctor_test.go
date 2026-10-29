package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/config"
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
	if err != (problemsFound{files: 3}) {
		t.Fatalf("err = %v, want the silent non-zero end for 3 files", err)
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

// A link to a machine the remote screen does not list still works, so it is
// no problem; doctor names it, and not a link to a machine on the list.
func TestDoctorNamesTheLinksToAMachineNotOnTheList(t *testing.T) {
	t.Setenv("REVIER_SSH_CONFIG", filepath.Join(t.TempDir(), "none"))
	doctorRoot(t, map[string]string{
		"far.toml":  "[remote]\nhost = \"farbox\"\nproject = \"far\"\n",
		"near.toml": "[remote]\nhost = \"buildbox\"\nproject = \"near\"\n",
	})
	root := os.Getenv("REVIER_CONFIG_HOME")
	if err := config.AddMachine(root, "buildbox"); err != nil {
		t.Fatal(err)
	}

	var out strings.Builder
	if err := cmdDoctor(&out, nil); err != nil {
		t.Fatalf("cmdDoctor: %v", err)
	}
	far := filepath.Join(root, "projects", "far.toml")
	if want := "1 link to a machine not on the remote screen (a link works without it):\n  " + far + " (farbox)\n"; !strings.Contains(out.String(), want) {
		t.Errorf("report should contain %q:\n%s", want, out.String())
	}
	if strings.Contains(out.String(), "near.toml") {
		t.Errorf("a link to a machine on the list should not be named:\n%s", out.String())
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
	if err := cmdDoctor(&out, nil); err != (problemsFound{files: 1}) {
		t.Fatalf("err = %v, want the silent non-zero end for 1 file", err)
	}
	for _, want := range []string{"config.toml", `action "sync"`, "typed text", "1 project, 1 file with problems"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("report should contain %q:\n%s", want, out.String())
		}
	}
}

// A refused [[status]] table is reported under the file that writes it, and
// so is a pane status a project changes that still runs nothing: doctor is
// where both are read (decisions.md D85).
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
	if err := cmdDoctor(&out, nil); err != (problemsFound{files: 2}) {
		t.Fatalf("err = %v, want the silent non-zero end for 2 files", err)
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

// A refused [[agent]] table, or a key in a project file, is reported under
// the file that writes it.
func TestDoctorReportsAgentProblemsUnderTheirFile(t *testing.T) {
	doctorRoot(t, map[string]string{"agents.toml": sound + `
[[agent]]
name = "dtop"
command = ["dtop"]
key = "ctrl+shift+d"
`})
	var out strings.Builder
	if err := cmdDoctor(&out, nil); err != (problemsFound{files: 1}) {
		t.Fatalf("err = %v, want the silent non-zero end for 1 file", err)
	}
	for _, want := range []string{"agents.toml", `agent "dtop"`, "config.toml alone"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("report should contain %q:\n%s", want, out.String())
		}
	}
}

// A link declares its realization under [target.remote], so a link target
// with none is never told to add the local table a line above told it to
// move, and a local target keeps the local advice.
func TestDoctorAdvisesALinkTargetTheRemoteTable(t *testing.T) {
	t.Setenv("REVIER_SSH_CONFIG", filepath.Join(t.TempDir(), "none"))
	doctorRoot(t, map[string]string{
		"far.toml":  "path = \"/x\"\n[remote]\nhost = \"buildbox\"\n\n[[target]]\nname = \"ed\"\n  [target.window]\n  launch = [\"code\"]\n  match = { class = \"^code$\" }\n",
		"near.toml": sound + "\n[[target]]\nname = \"ed\"\n",
	})

	var out strings.Builder
	if err := cmdDoctor(&out, nil); err != (problemsFound{files: 2}) {
		t.Fatalf("err = %v, want two files with problems", err)
	}
	root := filepath.Join(os.Getenv("REVIER_CONFIG_HOME"), "projects")
	ofFar, ofNear, ok := strings.Cut(out.String(), filepath.Join(root, "near.toml"))
	if !ok {
		t.Fatalf("report should name near.toml:\n%s", out.String())
	}
	if !strings.Contains(ofFar, "fix: give the target a `[target.remote.window]` or `[target.remote.runtime]` table") {
		t.Errorf("far.toml should be advised on the remote tables:\n%s", out.String())
	}
	if strings.Contains(ofFar, "`[target.window]`") {
		t.Errorf("far.toml should never be advised a local table:\n%s", out.String())
	}
	if !strings.Contains(ofNear, "fix: give the target a `[target.window]` or `[target.runtime]` table") {
		t.Errorf("near.toml should keep the local advice:\n%s", out.String())
	}
}

// An unknown name in [hosts] stops every command that lists or opens, so
// doctor reports it, in any position: after a host that works and after
// none too, where host selection never reaches it.
func TestDoctorReportsAnUnknownHostName(t *testing.T) {
	configRoot(t, "[hosts]\nruntime = [\"none\", \"ghostty\"]\nwindow = [\"sway\"]\n", map[string]string{"sound.toml": sound})

	var out strings.Builder
	if err := cmdDoctor(&out, nil); !errors.As(err, new(problemsFound)) {
		t.Fatalf("err = %v, want the silent non-zero end", err)
	}
	for _, want := range []string{
		`hosts.runtime unknown host "ghostty"; known: herdr, kitty, tmux, none`,
		`hosts.window  unknown host "sway"; known: gnome, none`,
		"1 project, 1 file with problems",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("report should contain %q:\n%s", want, out.String())
		}
	}
}

// A value of the wrong type is valid TOML, so its advice is about the type,
// and only a syntax error is told the file is not valid TOML.
func TestDoctorTellsATypeMismatchFromASyntaxError(t *testing.T) {
	doctorRoot(t, map[string]string{"typed.toml": "path = 5\n", "syntax.toml": "path = \n"})

	var out strings.Builder
	if err := cmdDoctor(&out, nil); !errors.As(err, new(problemsFound)) {
		t.Fatalf("err = %v, want the silent non-zero end", err)
	}
	root := filepath.Join(os.Getenv("REVIER_CONFIG_HOME"), "projects")
	ofSyntax, ofTyped, ok := strings.Cut(out.String(), filepath.Join(root, "typed.toml"))
	if !ok {
		t.Fatalf("report should name typed.toml:\n%s", out.String())
	}
	if !strings.Contains(ofTyped, "fix: give the key a value of the type the message names") || strings.Contains(ofTyped, "not valid TOML") {
		t.Errorf("typed.toml should be advised on the type alone:\n%s", out.String())
	}
	if !strings.Contains(ofSyntax, "fix: the file is not valid TOML") {
		t.Errorf("syntax.toml should keep the TOML advice:\n%s", out.String())
	}
}

// A refusal names what it is about with a colon after the name or without
// one. Either way the column is the name, and the message prints once.
func TestDoctorColumnsARefusalWithNoColonByItsName(t *testing.T) {
	configRoot(t, "[[action]]\nkey = \"ctrl-y\"\nname = \"c\"\n", map[string]string{"sound.toml": sound})

	var out strings.Builder
	if err := cmdDoctor(&out, nil); !errors.As(err, new(problemsFound)) {
		t.Fatalf("err = %v, want the silent non-zero end", err)
	}
	if want := "  action \"c\" runs nothing\n"; !strings.Contains(out.String(), want) {
		t.Errorf("report should contain %q:\n%s", want, out.String())
	}
	if n := strings.Count(out.String(), "runs nothing"); n != 1 {
		t.Errorf("the refusal is printed %d times, want once:\n%s", n, out.String())
	}
}
