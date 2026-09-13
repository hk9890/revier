package tui_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/internal/theme"
	"github.com/hk9890/revier/internal/tui"
	"github.com/hk9890/revier/pkg/revier"
)

// configRoot points the configuration at a scratch directory holding text as
// config.toml.
func configRoot(t *testing.T, text string) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv("REVIER_CONFIG_HOME", root)
	if err := os.WriteFile(filepath.Join(root, "config.toml"), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func configText(t *testing.T, root string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func screen(m tui.Model) string { return strings.Join(lines(m), "\n") }

// run delivers what a command answers, as the program would.
func run(m tui.Model, cmd tea.Cmd) tui.Model {
	next, _ := m.Update(cmd())
	return next.(tui.Model)
}

// onRuntimeRow opens the config screen with the cursor on the runtime row.
func onRuntimeRow(m tui.Model) tui.Model {
	m, _ = press(m, "alt+c")
	for range 3 {
		m, _ = press(m, "down")
	}
	return m
}

// alt+c opens the screen: every setting with its value, the runtime in use,
// and the window host as information. Esc goes back to the surface.
func TestAltCOpensTheConfigScreen(t *testing.T) {
	configRoot(t, "")
	_, _, c, projects := world(t, 2)
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 120, 20)

	m, _ = press(m, "alt+c")
	if head := barLine(m); !strings.Contains(head, "configuration") {
		t.Fatalf("top line = %q, want the config screen", head)
	}
	for _, want := range []string{"catppuccin-mocha", "nerd", "alt-space", "auto", "in use: rt", "wm", "detected at start"} {
		if !strings.Contains(screen(m), want) {
			t.Errorf("config screen does not say %q:\n%s", want, screen(m))
		}
	}
	m, _ = press(m, "esc")
	if r := ruleLine(m); !strings.Contains(r, "working") {
		t.Errorf("rule = %q after esc, want the surface back", r)
	}
}

// A new theme is written at once, the file's comments kept, and the screen
// shows it without a restart.
func TestTheConfigScreenWritesTheThemeAndKeepsComments(t *testing.T) {
	root := configRoot(t, "# mine\n[ui]\ntheme = \"catppuccin-mocha\" # dark\n")
	_, _, c, projects := world(t, 1)
	cfg := &config.Config{UI: config.UI{Theme: "catppuccin-mocha"}}
	m := resize(tui.New(c, projects, stateWith(t, nil), cfg, time.Second, theme.Default(), ""), 120, 20)

	m, _ = press(m, "alt+c")
	m, _ = press(m, "left")
	if got, want := configText(t, root), "# mine\n[ui]\ntheme = \"catppuccin-macchiato\" # dark\n"; got != want {
		t.Errorf("config.toml = %q, want %q", got, want)
	}
	if !strings.Contains(screen(m), "catppuccin-macchiato") {
		t.Errorf("config screen does not show the new theme:\n%s", screen(m))
	}

	m, _ = press(m, "down")
	m, _ = press(m, "right")
	if got := configText(t, root); !strings.Contains(got, `glyphs = "unicode"`) {
		t.Errorf("config.toml = %q, want the next glyph set written", got)
	}
	if !strings.Contains(screen(m), "unicode") {
		t.Errorf("config screen does not show the new glyph set:\n%s", screen(m))
	}
}

// The trigger key is typed and written on Enter. A key that does not parse is
// named in the footer and nothing is written.
func TestTheConfigScreenWritesTheTriggerKey(t *testing.T) {
	root := configRoot(t, "")
	_, _, c, projects := world(t, 1)
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 120, 20)

	m, _ = press(m, "alt+c")
	m, _ = press(m, "down")
	m, _ = press(m, "down")
	m, _ = press(m, "enter")
	m = typeInto(m, "frob-q")
	m, _ = press(m, "enter")
	if f := footer(m); !strings.Contains(f, "frob") {
		t.Errorf("footer = %q, want the bad key refused", f)
	}
	if got := configText(t, root); got != "" {
		t.Errorf("config.toml = %q after a refused key, want it untouched", got)
	}

	for range "frob-q" {
		m, _ = press(m, "backspace")
	}
	m = typeInto(m, "alt-j")
	m, _ = press(m, "enter")
	if got, want := configText(t, root), "[ui]\ntrigger_key = \"alt-j\"\n"; got != want {
		t.Errorf("config.toml = %q, want %q", got, want)
	}
	if !strings.Contains(screen(m), "alt-j") {
		t.Errorf("config screen does not show the new key:\n%s", screen(m))
	}
}

// A runtime choice is probed first, then written, and the surface runs on
// the host it selected.
func TestTheConfigScreenSwitchesTheRuntime(t *testing.T) {
	root := configRoot(t, "")
	_, _, c, projects := world(t, 1)
	tmux := hosttest.NewRuntime("tmux")
	var asked []string
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 120, 20).
		WithRuntimes([]string{"tmux", "none"}, func(_ context.Context, want []string) (revier.Runtime, error) {
			asked = want
			return tmux, nil
		})

	m, cmd := press(onRuntimeRow(m), "right")
	if cmd == nil {
		t.Fatal("no probe for the runtime choice")
	}
	if got := configText(t, root); got != "" {
		t.Errorf("config.toml = %q before the probe answered, want it untouched", got)
	}
	m = run(m, cmd)
	if len(asked) != 1 || asked[0] != "tmux" {
		t.Errorf("probed %v, want [tmux]", asked)
	}
	if got, want := configText(t, root), "[hosts]\nruntime = [\"tmux\"]\n"; got != want {
		t.Errorf("config.toml = %q, want %q", got, want)
	}
	if !strings.Contains(screen(m), "in use: tmux") {
		t.Errorf("config screen does not show the new runtime:\n%s", screen(m))
	}
}

// A choice whose host does not probe is named, and not written: written, it
// would refuse the next start.
func TestTheConfigScreenKeepsARuntimeThatDoesNotProbe(t *testing.T) {
	root := configRoot(t, "")
	_, _, c, projects := world(t, 1)
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 120, 20).
		WithRuntimes([]string{"tmux"}, func(context.Context, []string) (revier.Runtime, error) {
			return nil, errors.New("tmux: not installed")
		})

	m, cmd := press(onRuntimeRow(m), "right")
	m = run(m, cmd)
	if f := footer(m); !strings.Contains(f, "not installed") {
		t.Errorf("footer = %q, want the probe failure", f)
	}
	if got := configText(t, root); got != "" {
		t.Errorf("config.toml = %q, want it untouched", got)
	}
	if !strings.Contains(screen(m), "in use: rt") {
		t.Errorf("config screen does not show the old runtime still in use:\n%s", screen(m))
	}
}
