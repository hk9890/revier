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
	// The project of the working directory is read from the files, before
	// the first frame. The one of the focused window lists every host, so
	// the surface asks for it once it shows. Neither is an error to miss:
	// the TUI opens on the first row instead.
	start := revier.ProjectName("")
	if p, ok := a.resolveHere(); ok {
		start = p.Name
	}
	m := tui.New(a.core, a.projects, a.stateRoot, a.cfg, time.Second, th, start).
		WithRuntimes(append(slices.Clone(defaultRuntimeOrder), hostNone), func(ctx context.Context, want []string) (revier.Runtime, error) {
			return selectRuntime(ctx, want, runtimeAdapters())
		}).
		WithStart(func(ctx context.Context) (revier.ProjectName, bool) {
			p, ok := a.resolveAway(ctx)
			return p.Name, ok
		})
	// All motion reports the pointer with no button held, which the hover
	// needs, as well as the wheel and clicks. It takes plain drag-to-select
	// from the terminal; shift-drag still selects in kitty and most others.
	opts := []tea.ProgramOption{tea.WithAltScreen(), tea.WithMouseAllMotion()}
	// The popup's terminal says so in the environment, which is read and
	// dropped here: what the surface launches must not take it for the
	// popup. Focus reports are how the hidden popup learns it was raised.
	if os.Getenv(core.PopupEnv) != "" {
		_ = os.Unsetenv(core.PopupEnv)
		m = m.WithPopup()
		opts = append(opts, tea.WithReportFocus())
	}
	_, err = tea.NewProgram(m, opts...).Run()
	return err
}
