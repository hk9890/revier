package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
	root := t.TempDir()
	dir := filepath.Join(root, "projects")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("REVIER_CONFIG_HOME", root)
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
		"4 project(s), 3 with problems",
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
	if !strings.Contains(out.String(), "1 project(s), 0 with problems") {
		t.Errorf("report = %q", out.String())
	}
}
