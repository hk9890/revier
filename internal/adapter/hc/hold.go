package hc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/unix"

	"github.com/hk9890/revier/pkg/revier"
)

// AttachCmd is the revier command a view's window runs, as AttachPanel gives
// it: it is no command a user types.
const AttachCmd = "_hc-attach"

// heldLabel marks a finished terminal a window holds. Instances lists it, so
// the workspace stays the one instance a press raises, as a held kitty
// window does.
const heldLabel = "revier.held"

// heldLines is how much of a finished terminal's history a held window
// shows above its screen.
const heldLines = 1000

// heldWait bounds each call to the core once the attach has ended: the
// window waits for a person, not for the core.
const heldWait = 5 * time.Second

// heldPoll is how often a held window asks whether revier removed its
// terminal, as a close or a shutdown does.
const heldPoll = 500 * time.Millisecond

// Attach runs hc's attach of panel on the terminal in and out are, then
// holds the window when the terminal is revier's and its program ended on
// its own: see hold. Input typed before the hold is
// dropped, so a key meant for the program does not close the window. Past
// the attach it never starts a core: it dials the socket alone.
func (h *Host) Attach(ctx context.Context, bin string, panel revier.PanelID, in, out *os.File) error {
	attach := exec.CommandContext(ctx, bin, "attach", panel.String())
	attach.Stdin, attach.Stdout, attach.Stderr = in, out, out
	attachErr := attach.Run()
	_ = unix.IoctlSetInt(int(in.Fd()), unix.TCFLSH, unix.TCIFLUSH)
	held, err := h.hold(ctx, panel, in, out)
	if err != nil || held {
		return err
	}
	return attachErr
}

// hold shows what a finished terminal of revier's left on out, with how its
// program ended, and waits for Enter on in, then removes the terminal. A
// terminal revier removes meanwhile, as a close or a shutdown does, ends the
// hold too. The terminal carries heldLabel while held. It reports whether it
// held: a live terminal, one that is gone, one a kill ended, one started by
// hand and a core that is not running are not held.
func (h *Host) hold(ctx context.Context, panel revier.PanelID, in io.Reader, out io.Writer) (bool, error) {
	calls, cancel := context.WithTimeout(ctx, heldWait)
	defer cancel()
	s, err := h.dial(calls, false)
	if errors.Is(err, errNoServer) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer s.close()
	address := map[string]any{"terminal": panel.String()}
	var t terminal
	err = s.call("terminal.get", address, &t)
	if isNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if t.State != "finished" || t.Reason != "exited" || !isInstanceID(t.Labels[instanceLabel]) {
		return false, nil
	}
	if err := s.call("terminal.label", map[string]any{"terminal": panel.String(), "labels": map[string]string{heldLabel: "1"}}, nil); isNotFound(err) {
		return false, nil
	}
	text := "(revier could not read what it left)"
	var read struct {
		Text string `json:"text"`
	}
	if err := s.call("terminal.read", map[string]any{"terminal": panel.String(), "lines": heldLines}, &read); err == nil {
		text = strings.TrimSuffix(read.Text, "\n")
	}
	s.close()
	_, _ = fmt.Fprintf(out, "%s\n[%s %s; press Enter to close]", text, programOf(t), endOf(t))

	if h.waitForEnter(ctx, panel, in) {
		h.remove(ctx, panel)
	}
	return true, nil
}

// waitForEnter waits for Enter on in, '\r' or '\n' as the terminal's mode
// gives it, and reports true; or for the terminal to be gone, or the core,
// and reports false.
func (h *Host) waitForEnter(ctx context.Context, panel revier.PanelID, in io.Reader) bool {
	entered := make(chan struct{})
	go func() {
		b := make([]byte, 1)
		for {
			n, err := in.Read(b)
			if err != nil || (n == 1 && (b[0] == '\r' || b[0] == '\n')) {
				close(entered)
				return
			}
		}
	}()
	tick := time.NewTicker(heldPoll)
	defer tick.Stop()
	for {
		select {
		case <-entered:
			return true
		case <-ctx.Done():
			return false
		case <-tick.C:
			if !h.stillHeld(ctx, panel) {
				return false
			}
		}
	}
}

// stillHeld reports whether the core still holds the terminal. One that does not
// answer still holds it: the window keeps what it shows.
func (h *Host) stillHeld(ctx context.Context, panel revier.PanelID) bool {
	calls, cancel := context.WithTimeout(ctx, heldWait)
	defer cancel()
	s, err := h.dial(calls, false)
	if errors.Is(err, errNoServer) {
		return false
	}
	if err != nil {
		return true
	}
	defer s.close()
	return !isNotFound(s.call("terminal.get", map[string]any{"terminal": panel.String()}, nil))
}

// remove removes the terminal; one the sweep or a close removed is gone
// already.
func (h *Host) remove(ctx context.Context, panel revier.PanelID) {
	calls, cancel := context.WithTimeout(ctx, heldWait)
	defer cancel()
	s, err := h.dial(calls, false)
	if err != nil {
		return
	}
	defer s.close()
	_ = s.call("terminal.rm", map[string]any{"terminal": panel.String()}, nil)
}

// programOf is the name of the program a terminal ran.
func programOf(t terminal) string {
	if len(t.Argv) == 0 {
		return "the program"
	}
	return filepath.Base(t.Argv[0])
}

// endOf says how a finished terminal's program ended.
func endOf(t terminal) string {
	switch {
	case t.ExitCode != nil:
		return fmt.Sprintf("exited with code %d", *t.ExitCode)
	case t.Signal != nil:
		return fmt.Sprintf("ended on signal %d", *t.Signal)
	}
	return "ended"
}

// sweepAfter is how long a terminal whose program ended waits for a window
// to take it: a window that shows it holds it at once.
const sweepAfter = time.Minute

// sweep removes every finished terminal of revier's on this machine that
// ended over sweepAfter before now and that no window holds: one no window
// showed, as with no view or with kitty closed. A
// terminal on a machine hc added is that machine's revier's. It is one
// listing and one removal each, and a failure is left for the next sweep, as
// nothing waits on it.
func (s *session) sweep(now time.Time) {
	var list listResult
	if err := s.call("terminal.list", map[string]any{"all": true}, &list); err != nil {
		return
	}
	for _, t := range list.Terminals {
		if t.State != "finished" || t.FinishedMS == 0 || t.Labels[heldLabel] != "" ||
			strings.Contains(t.Address, "/") || !isInstanceID(t.Labels[instanceLabel]) {
			continue
		}
		if now.Sub(time.UnixMilli(t.FinishedMS)) > sweepAfter {
			_ = s.call("terminal.rm", map[string]string{"terminal": t.Address}, nil)
		}
	}
}
