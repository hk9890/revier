// What the panel capabilities ask of hc, against a core the test serves
// itself. No hc and no process, so the default layer runs it.
package hc_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hk9890/revier/internal/adapter/hc"
	"github.com/hk9890/revier/pkg/revier"
)

var home = revier.TargetRef{Host: "hc", ID: "core1/0a1b2c3d4e5f6a7b", Title: "home"}

// homeCore is a core whose instance home holds the live terminal t7.
func homeCore(t *testing.T) (*fakeCore, string) {
	c, sock := newCore(t)
	c.add(&fakeTerminal{Address: "t7", State: "live", Labels: map[string]string{"revier.instance": "0a1b2c3d4e5f6a7b"}})
	return c, sock
}

// The text goes as one automatic-input frame on an attach that takes no
// size, exactly as given, and the attach is closed.
func TestSendTextIsAutomaticInputOnAnAttachThatTakesNoSize(t *testing.T) {
	c, sock := homeCore(t)
	text := "fix it\x1b[201~ ä"
	if err := (&hc.Host{Socket: sock}).SendText(t.Context(), home, "t7", text); err != nil {
		t.Fatalf("SendText: %v", err)
	}
	attach := c.paramsOf("terminal.attach")
	if len(attach) != 1 || attach[0]["terminal"] != "t7" || attach[0]["cols"] != 0.0 || attach[0]["rows"] != 0.0 || attach[0]["resumed"] != true {
		t.Fatalf("attach = %v, want t7 at 0x0, resumed", attach)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if string(c.input) != text || !c.closed {
		t.Errorf("input = %q, closed %v; want %q, then the attach closed", c.input, c.closed, text)
	}
	// The credit for the first paint can come at any point.
	if kinds := slices.DeleteFunc(slices.Clone(c.kinds), func(k byte) bool { return k == 1 }); !slices.Equal(kinds, []byte{6, 2}) {
		t.Errorf("frame kinds = %v, want automatic input, then close", c.kinds)
	}
}

// A long text into a terminal whose output floods the attach still goes
// whole: the output is read and credited while the text is written, so
// neither side waits on the other.
func TestSendTextOfALongTextIntoABusyTerminalFinishes(t *testing.T) {
	c, sock := homeCore(t)
	c.mu.Lock()
	c.flood = 8 << 20
	c.mu.Unlock()
	text := strings.Repeat("x", 6<<20)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	if err := (&hc.Host{Socket: sock}).SendText(ctx, home, "t7", text); err != nil {
		t.Fatalf("SendText: %v", err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.input) != len(text) || !c.closed {
		t.Errorf("input = %d bytes, closed %v; want %d, then the attach closed", len(c.input), c.closed, len(text))
	}
}

// A terminal of another instance takes no text and is not read: both are
// refused, and neither is a panel that is gone.
func TestSendTextAndReadPanelRefuseATerminalOfAnotherInstance(t *testing.T) {
	c, sock := homeCore(t)
	c.add(&fakeTerminal{Address: "t3", State: "live", Labels: map[string]string{"revier.instance": "ffffffffffffffff"}})
	h := &hc.Host{Socket: sock}
	refused := func(err error) bool { return err != nil && !errors.Is(err, revier.ErrPanelGone) }
	if err := h.SendText(t.Context(), home, "t3", "x"); !refused(err) {
		t.Errorf("SendText into another instance's terminal = %v, want a refusal", err)
	}
	if err := h.SendText(t.Context(), revier.TargetRef{Host: "hc", ID: "core1/t3"}, "t7", "x"); !refused(err) {
		t.Errorf("SendText into t7 on the instance t3 = %v, want a refusal", err)
	}
	if _, err := h.ReadPanel(t.Context(), home, "t3", false); !refused(err) {
		t.Errorf("ReadPanel of another instance's terminal = %v, want a refusal", err)
	}
	if _, err := h.ReadPanel(t.Context(), revier.TargetRef{Host: "hc", ID: "core1/t3"}, "t7", false); !refused(err) {
		t.Errorf("ReadPanel of t7 on the instance t3 = %v, want a refusal", err)
	}
	if calls := c.called(); slices.Contains(calls, "terminal.attach") || slices.Contains(calls, "terminal.read") {
		t.Errorf("hc asked %q, want no attach and no read", calls)
	}
}

// A terminal hc no longer holds is closed already: ClosePanel of it passes,
// as Close takes one, and asks for no kill.
func TestClosePanelOfATerminalHcNoLongerHoldsPasses(t *testing.T) {
	c, sock := homeCore(t)
	if err := (&hc.Host{Socket: sock}).ClosePanel(t.Context(), home, "t9"); err != nil {
		t.Errorf("ClosePanel of a terminal hc no longer holds = %v, want nil", err)
	}
	if calls := c.called(); slices.Contains(calls, "terminal.kill") {
		t.Errorf("hc asked %q, want no kill", calls)
	}
}

// A core that closes the attach before it took all the text, and before the
// viewer's own CLOSE, fails the send: the text was not all delivered.
func TestSendTextFailsWhenTheCoreClosesTheAttachEarly(t *testing.T) {
	c, sock := homeCore(t)
	c.mu.Lock()
	c.closeEarly = true
	c.mu.Unlock()
	err := (&hc.Host{Socket: sock}).SendText(t.Context(), home, "t7", strings.Repeat("x", 6<<20))
	if err == nil {
		t.Fatal("SendText passed, want a failure: the core closed the attach before it took the text")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		t.Errorf("the viewer sent its CLOSE after the core's; want the send failed first")
	}
}

// A terminal of the instance that a close removed reads as a panel that is
// gone (decisions.md D172), and is not read.
func TestReadPanelOfATerminalACloseRemovedSaysThePanelIsGone(t *testing.T) {
	c, sock := homeCore(t)
	h := &hc.Host{Socket: sock}
	if err := h.ClosePanel(t.Context(), home, "t7"); err != nil {
		t.Fatalf("ClosePanel: %v", err)
	}
	if _, err := h.ReadPanel(t.Context(), home, "t7", false); !errors.Is(err, revier.ErrPanelGone) {
		t.Errorf("ReadPanel of a removed terminal = %v, want ErrPanelGone", err)
	}
	if calls := c.called(); slices.Contains(calls, "terminal.read") {
		t.Errorf("hc asked %q, want no read", calls)
	}
}

// The read is the screen, with scrollback or without: hc reads the history of
// a full-screen program by scrolling it.
func TestReadPanelAsksForTheScreenAlone(t *testing.T) {
	c, sock := homeCore(t)
	c.read = "line 1\nline 2\n"
	h := &hc.Host{Socket: sock}
	got, err := h.ReadPanel(t.Context(), home, "t7", false)
	if err != nil || got != "line 1\nline 2" {
		t.Errorf("ReadPanel = %q, %v; want the two lines", got, err)
	}
	if _, err := h.ReadPanel(t.Context(), home, "t7", true); err != nil {
		t.Fatalf("ReadPanel with scrollback: %v", err)
	}
	reads := c.paramsOf("terminal.read")
	if len(reads) != 2 || reads[0]["lines"] != 0.0 || reads[1]["lines"] != 0.0 || reads[0]["terminal"] != "t7" {
		t.Errorf("reads = %v, want t7 with no history, twice", reads)
	}
}

// A terminal hc no longer holds is a panel that is gone, and any other
// failure stays a failure (decisions.md D172).
func TestReadPanelOfATerminalHcNoLongerHoldsSaysThePanelIsGone(t *testing.T) {
	c, sock := homeCore(t)
	h := &hc.Host{Socket: sock}
	failWith := func(e map[string]any) {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.readErr = e
	}

	failWith(notFound)
	if _, err := h.ReadPanel(t.Context(), home, "t7", false); !errors.Is(err, revier.ErrPanelGone) || !strings.Contains(err.Error(), "not_found") {
		t.Errorf("ReadPanel of a terminal hc no longer holds = %v, want ErrPanelGone with hc's reason", err)
	}
	failWith(map[string]any{"code": -32000, "message": "no machine m1", "data": map[string]string{"kind": "machine_unknown"}})
	if _, err := h.ReadPanel(t.Context(), home, "t7", false); err == nil || errors.Is(err, revier.ErrPanelGone) {
		t.Errorf("ReadPanel on a machine hc does not know = %v, want a failure that is not ErrPanelGone", err)
	}
}

// Close ends each terminal of the instance and removes its record, a
// finished one's too, and leaves every other terminal; ClosePanel ends one,
// and only one of the instance.
func TestCloseEndsEveryTerminalOfTheInstanceAlone(t *testing.T) {
	c, sock := newCore(t)
	mine := map[string]string{"revier.instance": "0a1b2c3d4e5f6a7b"}
	c.add(&fakeTerminal{Address: "t1", State: "live", Labels: mine})
	c.add(&fakeTerminal{Address: "t2", State: "live", Labels: mine})
	c.add(&fakeTerminal{Address: "t3", State: "live", Labels: map[string]string{"revier.instance": "ffffffffffffffff"}})
	c.add(&fakeTerminal{Address: "t4", State: "live"})
	c.add(&fakeTerminal{Address: "t5", State: "finished", Labels: mine})
	h := &hc.Host{Socket: sock}

	if err := h.ClosePanel(t.Context(), home, "t3"); err == nil {
		t.Error("ClosePanel of another instance's terminal passed")
	}
	if err := h.ClosePanel(t.Context(), revier.TargetRef{Host: "hc", ID: "core1/t4"}, "t1"); err == nil {
		t.Error("ClosePanel of t1 on the instance t4 passed")
	}
	if err := h.ClosePanel(t.Context(), home, "t2"); err != nil {
		t.Fatalf("ClosePanel: %v", err)
	}
	if live := c.live(); !slices.Equal(live, []string{"t1", "t3", "t4"}) {
		t.Errorf("after ClosePanel live = %q, want t1 t3 t4", live)
	}
	if err := h.Close(t.Context(), home); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if records := c.records(); !slices.Equal(records, []string{"t3", "t4"}) {
		t.Errorf("after Close records = %q, want t3 t4", records)
	}
	if err := h.Close(t.Context(), revier.TargetRef{Host: "hc", ID: "core1/t4"}); err != nil {
		t.Fatalf("Close by hand: %v", err)
	}
	if live := c.live(); !slices.Equal(live, []string{"t3"}) {
		t.Errorf("after Close of t4 live = %q, want t3", live)
	}
}

// The terminals of an instance are ended all at once: hc's kill takes up to
// a second for a program that saves on neither HUP nor TERM, and one after
// another, an instance of many panels would hold the close for as many
// seconds.
func TestCloseEndsTheTerminalsAllAtOnce(t *testing.T) {
	const kill = 500 * time.Millisecond
	c, sock := newCore(t)
	c.mu.Lock()
	c.killDelay = kill
	c.mu.Unlock()
	for _, address := range []string{"t1", "t2", "t3", "t4"} {
		c.add(&fakeTerminal{Address: address, State: "live", Labels: map[string]string{"revier.instance": "0a1b2c3d4e5f6a7b"}})
	}
	began := time.Now()
	if err := (&hc.Host{Socket: sock}).Close(t.Context(), home); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if took := time.Since(began); took > 3*kill {
		t.Errorf("Close of 4 terminals took %s, want about one kill's %s", took, kill)
	}
	if records := c.records(); len(records) != 0 {
		t.Errorf("records = %q, want none", records)
	}
}

// A terminal hc says not_found of is ended already: one removed when its
// program exited, which the kill itself can make it do, closes as done.
func TestCloseTakesATerminalHcRemovedAsEnded(t *testing.T) {
	c, sock := newCore(t)
	mine := map[string]string{"revier.instance": "0a1b2c3d4e5f6a7b"}
	c.add(&fakeTerminal{Address: "t1", State: "live", Labels: mine, rm: true})
	c.add(&fakeTerminal{Address: "t2", State: "live", Labels: mine})
	if err := (&hc.Host{Socket: sock}).Close(t.Context(), home); err != nil {
		t.Fatalf("Close = %v, want a terminal hc removed taken as ended", err)
	}
	if records := c.records(); len(records) != 0 {
		t.Errorf("records = %q, want none", records)
	}
	if rms := c.paramsOf("terminal.rm"); len(rms) != 2 {
		t.Errorf("removed %v, want t1 and t2 asked for", rms)
	}
}

// A core that starts on the socket between the close's listing and its
// kills gives the same addresses to terminals of its own: the close ends
// none of them.
func TestCloseEndsNoTerminalOfACoreThatStartedMeanwhile(t *testing.T) {
	c, sock := newCore(t)
	c.add(&fakeTerminal{Address: "t1", State: "live", Labels: map[string]string{"revier.instance": "0a1b2c3d4e5f6a7b"}})
	c.mu.Lock()
	c.idAfterList = "core2"
	c.mu.Unlock()
	if err := (&hc.Host{Socket: sock}).Close(t.Context(), home); err == nil || !strings.Contains(err.Error(), "core2") {
		t.Errorf("Close = %v, want a failure that names the core now answering", err)
	}
	if calls := c.called(); slices.Contains(calls, "terminal.kill") || slices.Contains(calls, "terminal.rm") {
		t.Errorf("hc asked %q, want no kill and no rm", calls)
	}
}

// A ref that another core made names nothing on this one, and nothing is
// done to it.
func TestARefOfAnotherCoreIsRefused(t *testing.T) {
	c, sock := newCore(t)
	h := &hc.Host{Socket: sock}
	other := revier.TargetRef{Host: "hc", ID: "core0/0a1b2c3d4e5f6a7b"}
	if err := h.Close(t.Context(), other); err == nil {
		t.Error("Close passed")
	}
	if err := h.SendText(t.Context(), other, "t1", "x"); err == nil {
		t.Error("SendText passed")
	}
	if _, err := h.ReadPanel(t.Context(), other, "t1", false); err == nil {
		t.Error("ReadPanel passed")
	}
	if err := h.ClosePanel(t.Context(), revier.TargetRef{Host: "tmux", ID: "$1"}, "t1"); err == nil {
		t.Error("ClosePanel of a tmux ref passed")
	}
	if calls := c.called(); slices.ContainsFunc(calls, func(m string) bool { return m != "hello" }) {
		t.Errorf("hc asked %q, want hellos alone", calls)
	}
}

// The attach is to the instance's first terminal by its name, or to a
// terminal made by hand by its address, with a private socket carried along.
func TestAttachCommandIsTheFirstTerminalOfTheInstance(t *testing.T) {
	t.Setenv("PATH", hcLink(t))
	got, err := (&hc.Host{}).AttachCommand(home)
	if err != nil || !slices.Equal(got, []string{"hc", "attach", "rv-0a1b2c3d4e5f6a7b-0"}) {
		t.Errorf("AttachCommand = %q, %v", got, err)
	}
	// hc is also Holochain's CLI: herdr-core, where there is one, is hc's.
	t.Setenv("PATH", hcLink(t)+":"+fakeBin(t, "herdr-core"))
	got, err = (&hc.Host{}).AttachCommand(home)
	if err != nil || !slices.Equal(got, []string{"herdr-core", "attach", "rv-0a1b2c3d4e5f6a7b-0"}) {
		t.Errorf("AttachCommand with hc and herdr-core on PATH = %q, %v", got, err)
	}
	got, err = (&hc.Host{Socket: "/s.sock"}).AttachCommand(revier.TargetRef{Host: "hc", ID: "core1/t4"})
	if err != nil || !slices.Equal(got, []string{"env", "HC_SOCKET=/s.sock", "herdr-core", "attach", "t4"}) {
		t.Errorf("AttachCommand by hand = %q, %v", got, err)
	}
}
