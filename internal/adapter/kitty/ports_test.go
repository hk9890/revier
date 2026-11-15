package kitty_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hk9890/revier/internal/adapter/kitty"
	"github.com/hk9890/revier/pkg/revier"
)

// kitty runs its programs and draws them, so one host is the runtime and the
// view over the same kitty processes (decisions.md D176), and each optional
// capability is on the half the core asks it of.
func TestKittyIsTheRuntimeAndTheViewWithTheirCapabilities(t *testing.T) {
	var rt revier.Runtime = &kitty.Host{}
	for name, ok := range map[string]bool{
		"TabOpener":   is[revier.TabOpener](rt),
		"PanelWriter": is[revier.PanelWriter](rt),
		"PanelReader": is[revier.PanelReader](rt),
		"PanelCloser": is[revier.PanelCloser](rt),
		"Closer":      is[revier.Closer](rt),
	} {
		if !ok {
			t.Errorf("the kitty runtime is no %s", name)
		}
	}
	view, ok := rt.(revier.View)
	if !ok {
		t.Fatal("the kitty runtime is not its own view")
	}
	if !is[revier.PanelFocuser](view) || !is[revier.PanelFinder](view) {
		t.Error("the kitty view cannot focus a panel or map a key's panel id")
	}
	if !view.Capabilities().OSWindows {
		t.Error("the kitty view does not claim OS windows")
	}
}

// Over hc, kitty is a view alone: it draws hc's terminals in windows of its
// own, maps a key's window to a terminal, and opens kitty's own tab outside a
// workspace, and runs no program of the runtime's (decisions.md D180, D183).
func TestTheKittyViewOverAnotherRuntimeIsAViewAlone(t *testing.T) {
	var view revier.View = &kitty.View{}
	for name, ok := range map[string]bool{
		"Shower":       is[revier.Shower](view),
		"PanelFocuser": is[revier.PanelFocuser](view),
		"PanelFinder":  is[revier.PanelFinder](view),
		"BesideOpener": is[revier.BesideOpener](view),
	} {
		if !ok {
			t.Errorf("the kitty view is no %s", name)
		}
	}
	if is[revier.Runtime](view) || is[revier.TabOpener](view) {
		t.Error("the kitty view runs programs")
	}
	if !view.Capabilities().OSWindows {
		t.Error("the kitty view does not claim OS windows")
	}
}

// With no display there is nothing to show a window on, so the view is not
// usable, and hc runs unshown, as over ssh (decisions.md D178).
func TestTheKittyViewNeedsADisplay(t *testing.T) {
	bin := t.TempDir()
	for _, name := range []string{"kitty", "kitten"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)
	t.Setenv("WAYLAND_DISPLAY", "")
	t.Setenv("DISPLAY", "")
	view := &kitty.View{}
	if err := view.Probe(t.Context()); err == nil {
		t.Error("Probe with no display passed")
	}
	t.Setenv("WAYLAND_DISPLAY", "wayland-0")
	if err := view.Probe(t.Context()); err != nil {
		t.Errorf("Probe with a display: %v", err)
	}
}

func is[C any](v any) bool {
	_, ok := v.(C)
	return ok
}
