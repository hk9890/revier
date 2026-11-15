package hc

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/hk9890/revier/pkg/revier"
)

// AttachCmd is the revier command a view's window runs, as AttachPanel gives
// it: it is no command a user types.
const AttachCmd = "_hc-attach"

// heldLines is how much of a finished terminal's history a held window
// shows above its screen.
const heldLines = 1000

// heldWait bounds each call to the core once the attach has ended: the
// window waits for a person, not for the core.
const heldWait = 5 * time.Second

// Attach runs hc's attach of panel on the terminal in and out are, then
// holds the window when the terminal's program ended on its own: it shows
// what the program left and how it ended, removes the terminal, and waits
// for Enter, as the kitty runtime holds a panel's command (decisions.md
// D187). A program that ended before the attach is held too. A detach, a
// kill, a shell, which hc removes as it exits, and a core that is gone end
// the window at once. Past the attach it never starts a core: it dials the
// socket alone.
func (h *Host) Attach(ctx context.Context, bin string, panel revier.PanelID, in, out *os.File) error {
	attach := exec.CommandContext(ctx, bin, "attach", panel.String())
	attach.Stdin, attach.Stdout, attach.Stderr = in, out, out
	attachErr := attach.Run()
	held, err := h.hold(ctx, panel, in, out)
	if err != nil || held {
		return err
	}
	return attachErr
}

// hold shows what a finished terminal left on out, with how its program
// ended, removes the terminal, and waits for a line on in. It reports
// whether it held: a live terminal, one that is gone, one a kill ended and
// a core that is not running are not held.
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
	params := map[string]any{"terminal": panel.String()}
	var t terminal
	err = s.call("terminal.get", params, &t)
	if isNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if t.State != "finished" || t.Reason != "exited" {
		return false, nil
	}
	var read struct {
		Text string `json:"text"`
	}
	params["lines"] = heldLines
	if err := s.call("terminal.read", params, &read); err != nil && !isNotFound(err) {
		return false, err
	}
	if err := s.call("terminal.rm", map[string]any{"terminal": panel.String()}, nil); err != nil && !isNotFound(err) {
		return false, err
	}
	s.close()
	_, _ = fmt.Fprintf(out, "%s\n[%s %s; press Enter to close]", strings.TrimSuffix(read.Text, "\n"), programOf(t), endOf(t))
	_, _ = bufio.NewReader(in).ReadString('\n')
	return true, nil
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
// to take it: a window that shows it reads it at once.
const sweepAfter = time.Minute

// sweep removes every finished terminal of revier's that ended over
// sweepAfter before now: one no window showed, as with no view or with
// kitty closed (decisions.md D187). It is one listing and one removal each,
// and a failure is left for the next sweep, as nothing waits on it.
func (s *session) sweep(now time.Time) {
	var list listResult
	if err := s.call("terminal.list", map[string]any{"all": true}, &list); err != nil {
		return
	}
	for _, t := range list.Terminals {
		if t.State != "finished" || t.FinishedMS == 0 || !isInstanceID(t.Labels[instanceLabel]) {
			continue
		}
		if now.Sub(time.UnixMilli(t.FinishedMS)) > sweepAfter {
			_ = s.call("terminal.rm", map[string]string{"terminal": t.Address}, nil)
		}
	}
}
