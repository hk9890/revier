package main

import (
	"context"
	"os"
	"slices"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/term"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/tui"
	"github.com/hk9890/revier/pkg/revier"
)

// cmdTUI runs the surface. Without a terminal on its output - `revier | grep`,
// or a writer that is no file at all - it prints the table instead, so a
// script sees what it always saw.
func cmdTUI(a *app) error {
	if f, ok := a.out.(*os.File); !ok || !term.IsTerminal(int(f.Fd())) {
		return cmdList(context.Background(), a, nil)
	}
	th, err := a.cfg.Theme()
	if err != nil {
		return err
	}
	start, popup := tuiStart(a)
	m := tui.New(a.core, a.projects, a.stateRoot, a.cfg, time.Second, th, start).
		WithRuntimes(append(slices.Clone(defaultRuntimeOrder), hostNone), func(ctx context.Context, want []string) (revier.Runtime, error) {
			return selectRuntime(ctx, want, runtimeAdapters())
		})
	// All motion reports the pointer with no button held, which the hover
	// needs, as well as the wheel and clicks. It takes plain drag-to-select
	// from the terminal; shift-drag still selects in kitty and most others.
	opts := []tea.ProgramOption{tea.WithAltScreen(), tea.WithMouseAllMotion()}
	if popup {
		// Focus reports are how the hidden popup learns it was raised.
		m = m.WithPopup()
		opts = append(opts, tea.WithReportFocus())
	} else {
		m = m.WithStart(func(ctx context.Context) (revier.ProjectName, bool) {
			p, ok := a.resolveAway(ctx)
			return p.Name, ok
		})
	}
	_, err = tea.NewProgram(m, opts...).Run()
	return err
}

// tuiStart reads what the surface needs from where it was started - the
// project it opens on, and whether it is the popup - and then leaves.
//
// The popup's terminal says so in the environment, which is read and dropped
// here: what the surface launches must not take it for the popup. The value
// is the project `revier popup` resolved at the keypress, when the focused
// window was still the user's.
//
// The project of the working directory is read from the files, before the
// first frame. The one of the focused window lists every host, so the surface
// asks for it once it shows; in the popup it would find the popup itself, so
// the popup's answer stands. Neither is an error to miss: the TUI opens on
// the first row instead.
func tuiStart(a *app) (start revier.ProjectName, popup bool) {
	mark, popup := os.LookupEnv(core.PopupEnv)
	_ = os.Unsetenv(core.PopupEnv)
	if popup {
		if p, ok := a.project(revier.ProjectName(mark)); ok {
			start = p.Name
		}
	} else if p, ok := a.resolveHere(); ok {
		start = p.Name
	}
	leaveStartDir()
	return start, popup
}
