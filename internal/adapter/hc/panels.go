package hc

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/hk9890/revier/pkg/revier"
)

// Close ends every terminal of the instance, live or finished, and removes
// its record: hc has no group to close. The kill is the only close hc
// offers, so an agent gets about a second to save.
func (h *Host) Close(ctx context.Context, r revier.TargetRef) error {
	ref, err := h.parse(r)
	if err != nil {
		return err
	}
	if !ref.byHand() {
		// It checks on each connection that the ref's core answers.
		return h.closeInstance(ctx, ref.core, ref.key)
	}
	s, _, err := h.session(ctx, r, false)
	if err != nil {
		return err
	}
	defer s.close()
	return s.end(ref.key)
}

// ClosePanel ends one terminal of the instance and removes its record, as
// Close does each. A terminal of another instance is refused. A terminal hc
// no longer holds is closed already, as Close takes one.
func (h *Host) ClosePanel(ctx context.Context, r revier.TargetRef, panel revier.PanelID) error {
	s, ref, err := h.session(ctx, r, false)
	if err != nil {
		return err
	}
	defer s.close()
	err = s.holds(ref, r, panel)
	switch {
	case errors.Is(err, revier.ErrPanelGone):
		return nil
	case err != nil:
		return err
	}
	return s.end(panel.String())
}

// errNotInInstance is a panel that is no terminal of the instance.
var errNotInInstance = errors.New("not in the instance")

// holds refuses a panel that is not a terminal of the ref's instance, live or
// finished, with errNotInInstance. A terminal hc no longer holds, as one a
// close removed or one whose program exited, is a panel that is gone: the
// error wraps revier.ErrPanelGone, which a read reports as such (decisions.md
// D172), a close takes as done, and a send fails on.
func (s *session) holds(ref ref, r revier.TargetRef, panel revier.PanelID) error {
	address := panel.String()
	if ref.byHand() {
		if address == ref.key {
			return nil
		}
		return fmt.Errorf("hc: panel %s: %w %s", address, errNotInInstance, r.ID)
	}
	var t terminal
	err := s.call("terminal.get", map[string]string{"terminal": address}, &t)
	if isNotFound(err) {
		return fmt.Errorf("%w: %w", revier.ErrPanelGone, err)
	}
	if err != nil {
		return err
	}
	if t.Labels[instanceLabel] != ref.key {
		return fmt.Errorf("hc: panel %s: %w %s", address, errNotInInstance, r.ID)
	}
	return nil
}

// SendText types text into the terminal as given, with no key added: a
// submit is a "\r" of its own. hc's send adds Enter and its keys refuse
// many characters, so the bytes go as a viewer's automatic input instead:
// an attach that takes no size, carries them, and closes. Automatic input is
// not a person typing, so hc neither waits for a quiet moment nor holds a
// later send of its own back for it. A terminal of another instance is
// refused.
//
// The terminal's output is read and credited while the text goes: a core
// that streams output to a viewer that does not read it stops reading that
// viewer's input, and a long text into a busy terminal would wait forever.
func (h *Host) SendText(ctx context.Context, r revier.TargetRef, panel revier.PanelID, text string) error {
	s, ref, err := h.session(ctx, r, true)
	if err != nil {
		return err
	}
	defer s.close()
	if err := s.holds(ref, r, panel); err != nil {
		return err
	}
	var attached struct {
		Channel        uint32 `json:"channel"`
		AutomaticInput bool   `json:"automatic_input"`
	}
	params := map[string]any{"terminal": panel.String(), "cols": 0, "rows": 0, "resumed": true}
	if err := s.callFramed("terminal.attach", params, &attached); err != nil {
		return err
	}
	if !attached.AutomaticInput {
		return errors.New("hc: the attach to " + panel.String() + " takes no automatic input, which revier types with")
	}
	// The deferred close ends the read when a write fails. A CLOSE of the
	// core's before this one's own fails the send: the text was not all
	// taken.
	st := s.watch(attached.Channel)
	for b := []byte(text); len(b) > 0; {
		n := min(len(b), typedChunk)
		if err := st.open(); err != nil {
			return s.fail("terminal.attach", err)
		}
		if err := s.pay(st); err != nil {
			return s.fail("terminal.attach", err)
		}
		if err := s.writeFrame(attached.Channel, frameAutomatic, b[:n]); err != nil {
			return s.fail("terminal.attach", err)
		}
		b = b[n:]
	}
	if err := st.open(); err != nil {
		return s.fail("terminal.attach", err)
	}
	// The core's own CLOSE answers once the bytes before it are taken.
	if err := s.writeFrame(attached.Channel, frameClose, nil); err != nil {
		return s.fail("terminal.attach", err)
	}
	if err := s.untilClosed(st); err != nil {
		return s.fail("terminal.attach", err)
	}
	return nil
}

// typedChunk is the most bytes of text one frame carries: the output's
// credit goes back between two frames, so a long text does not stall the
// terminal's output while it goes.
const typedChunk = 64 << 10

// ReadPanel is the terminal's screen as plain text, a "\n" between two
// lines. hc reads no colour, so the text carries no SGR.
//
// It reads no history, with scrollback or without: to read the history of a
// full-screen program, such as an agent's TUI on the alternate screen, hc
// scrolls the program with wheel input, which a person attached to it sees,
// and hc's terminal record does not say which program is full-screen.
//
// A terminal Open started is removed when its program exits. A terminal hc
// says not_found of is a panel that is gone: the read wraps
// revier.ErrPanelGone (decisions.md D172). hc reads a finished terminal it
// still holds, such as one started by hand, as it was left. A terminal of another instance is refused.
func (h *Host) ReadPanel(ctx context.Context, r revier.TargetRef, panel revier.PanelID, _ bool) (string, error) {
	s, ref, err := h.session(ctx, r, false)
	if err != nil {
		return "", err
	}
	defer s.close()
	if err := s.holds(ref, r, panel); err != nil {
		return "", err
	}
	params := map[string]any{"terminal": panel.String(), "lines": 0}
	var read struct {
		Text string `json:"text"`
	}
	err = s.call("terminal.read", params, &read)
	if isNotFound(err) {
		return "", fmt.Errorf("%w: %w", revier.ErrPanelGone, err)
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(read.Text, "\n"), nil
}

// session is a session with the core that made the ref, and the ref taken
// apart.
func (h *Host) session(ctx context.Context, r revier.TargetRef, framed bool) (*session, ref, error) {
	ref, err := h.parse(r)
	if err != nil {
		return nil, ref, err
	}
	s, err := h.dial(ctx, framed)
	if err != nil {
		return nil, ref, err
	}
	if err := ref.current(s); err != nil {
		s.close()
		return nil, ref, err
	}
	return s, ref, nil
}

// AttachCommand is the argv that puts the calling terminal on the instance's
// first terminal, by its name: hc attaches one terminal, not a group. Once
// that terminal is closed, the attach fails, as AttachCommand has no context
// to list the instance with. From inside that terminal hc refuses the attach
// and says so; from any other terminal of the core it attaches. A private
// socket is carried along. The caller execs it; nothing here takes over a
// terminal.
func (h *Host) AttachCommand(r revier.TargetRef) ([]string, error) {
	ref, err := h.parse(r)
	if err != nil {
		return nil, err
	}
	bin, err := program()
	if err != nil {
		return nil, err
	}
	target := ref.key
	if !ref.byHand() {
		target = terminalName(ref.key, 0)
	}
	argv := []string{bin, "attach", target}
	if h.Socket != "" {
		argv = append([]string{"env", "HC_SOCKET=" + h.Socket}, argv...)
	}
	return argv, nil
}
