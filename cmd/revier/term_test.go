package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
)

// termApp is an app over a config.toml of text, its kitty directory a fresh
// one that holds kittyConf.
func termApp(t *testing.T, text, kittyConf string) (*app, string) {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, "config.toml")
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Read(root)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "kitty")
	t.Setenv("KITTY_CONFIG_DIRECTORY", dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "kitty.conf"), []byte(kittyConf), 0o644); err != nil {
		t.Fatal(err)
	}
	return &app{cfg: cfg, core: &core.Core{}, out: &bytes.Buffer{}}, dir
}

func runTerm(t *testing.T, a *app, args ...string) (string, error) {
	t.Helper()
	var b bytes.Buffer
	a.out = &b
	err := cmdTerm(context.Background(), a, args)
	return b.String(), err
}

// A dry run says what install would do and writes nothing; the run writes
// it, and status then reports every key written and the user's own map that
// wins over one.
func TestTermInstallDryRunThenRunThenStatus(t *testing.T) {
	a, dir := termApp(t, "[[agent]]\nname = \"dtop\"\ncommand = [\"dtop\"]\nkey = \"ctrl+shift+d\"\n",
		"map ctrl+shift+t new_tab\n")

	out, err := runTerm(t, a, "install", "-n")
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	for _, want := range []string{"dry run", "revier.conf: would be created", "include would be added at the top", "shadowed by kitty.conf:1"} {
		if !strings.Contains(out, want) {
			t.Errorf("dry run does not say %q:\n%s", want, out)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "revier.conf")); !os.IsNotExist(err) {
		t.Fatalf("the dry run wrote revier.conf: %v", err)
	}

	out, err = runTerm(t, a, "install")
	if err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}
	if !strings.Contains(out, "kitty reads the change by itself") || !strings.Contains(out, "shadowed by kitty.conf:3") {
		t.Errorf("install output:\n%s", out)
	}

	out, err = runTerm(t, a, "status")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"revier.conf: current", "includes revier.conf at line 2", "ctrl+shift+d", "agent dtop", "written", "shadowed by kitty.conf:3"} {
		if !strings.Contains(out, want) {
			t.Errorf("status does not say %q:\n%s", want, out)
		}
	}

	out, err = runTerm(t, a, "uninstall", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var plan struct{ File, Include core.TermAction }
	if err := json.Unmarshal([]byte(out), &plan); err != nil || plan.File != core.TermRemove || plan.Include != core.TermRemove {
		t.Errorf("uninstall plan = %+v, %v:\n%s", plan, err, out)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "kitty.conf")); string(b) != "map ctrl+shift+t new_tab\n" {
		t.Errorf("kitty.conf after uninstall = %q", b)
	}
}

// A terminal key on the desktop's trigger key is refused, the rest are
// written, and the run says it is incomplete.
func TestTermInstallOfAKeyTheDesktopHoldsIsIncomplete(t *testing.T) {
	a, dir := termApp(t, "[terminal]\nshell_key = \"alt+space\"\n", "")
	out, err := runTerm(t, a, "install")
	if !errors.Is(err, errKeysIncomplete) {
		t.Errorf("err = %v, want errKeysIncomplete", err)
	}
	if !strings.Contains(out, "refused: the desktop key that opens revier") {
		t.Errorf("output:\n%s", out)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "revier.conf"))
	if strings.Contains(string(b), "alt+space") || !strings.Contains(string(b), "map ctrl+shift+z") {
		t.Errorf("revier.conf =\n%s", b)
	}
}
