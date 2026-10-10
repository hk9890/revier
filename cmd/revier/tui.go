package main

import (
	"context"
	"os"
	"os/signal"
	"slices"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/term"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/ledger"
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
	// The surface owns the terminal, so a state write that fails is in the
	// log and not on stderr.
	m := tui.New(a.core.WithLedger(ledger.File{Root: a.stateRoot}), a.projects, a.stateRoot, a.cfg, time.Second, th, start).
		WithRuntimes(append(slices.Clone(defaultRuntimeOrder), hostNone), func(ctx context.Context, want []string) (revier.Runtime, error) {
			return selectHost(ctx, "runtime", want, defaultRuntimeOrder, runtimeAdapters())
		})
	// All motion reports the pointer with no button held, which the hover
	// needs, as well as the wheel and clicks. It takes plain drag-to-select
	// from the terminal; shift-drag still selects in kitty and most others.
	opts := []tea.ProgramOption{tea.WithAltScreen(), tea.WithMouseAllMotion()}
	if popup {
		// Focus reports are how the hidden popup learns it was raised; input
		// says so too where a report is lost.
		m = m.WithPopup()
		opts = append(opts, tea.WithReportFocus())
	} else {
		m = m.WithStart(func(ctx context.Context) (revier.ProjectName, bool) {
			p, ok := a.resolveAway(ctx)
			return p.Name, ok
		})
	}
	return runSurface(m, opts...)
}

// signalEnd is the surface ended by a signal: its terminal closed, or
// something stopped the process. It is a normal outcome, with the status a
// shell gives a command a signal ended.
type signalEnd struct{ sig syscall.Signal }

func (e signalEnd) Error() string { return "ended by signal: " + e.sig.String() }

// killWait is how long a surface that a signal ended has to give the
// terminal back. A command that holds the terminal and outlives the signal
// would hold the surface with it.
const killWait = time.Second

// runSurface runs the surface until it quits or a signal ends it, and names
// the signal. The program runs without bubbletea's own handler, which does
// not catch a hangup and turns a termination into a quit that names no
// signal: a surface whose terminal closed ended with no line in the log.
//
// An interrupt ends nothing. The surface reads ctrl+c as a key, so the
// terminal sends an interrupt only while an action, a clone or the assistant
// holds it, and that one is for the command.
func runSurface(m tea.Model, opts ...tea.ProgramOption) error {
	p := tea.NewProgram(m, append(opts, tea.WithoutSignalHandler())...)
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGHUP, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(signals)
	quit := make(chan error, 1)
	go func() {
		_, err := p.Run()
		quit <- err
	}()
	for {
		select {
		case err := <-quit:
			return err
		case sig := <-signals:
			if sig == syscall.SIGINT {
				continue
			}
			p.Kill()
			select {
			case <-quit:
			case <-time.After(killWait):
			}
			return signalEnd{sig.(syscall.Signal)}
		}
	}
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
