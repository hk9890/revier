//go:build live

package hc_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hk9890/revier/internal/adapter/hc"
	"github.com/hk9890/revier/pkg/revier"
)

// endingPanel opens an instance whose command prints a line and exits 3 once
// the file the test writes exists, and returns it, its panel and that file.
func endingPanel(t *testing.T, h *hc.Host) (revier.TargetRef, revier.PanelID, string) {
	t.Helper()
	c := ctx(t)
	done := filepath.Join(t.TempDir(), "done")
	ref, err := h.Open(c, revier.Realization{Name: "ends", Launch: []string{"sh", "-c", `echo no such config; while [ ! -e "$0" ]; do sleep 0.05; done; exit 3`, done}})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	panel := waitFor(t, h, func(in []revier.Instance) bool { return len(in) == 1 })[0].Panels[0].ID
	return ref, panel, done
}

// endedPanel is endingPanel's instance once its command ended.
func endedPanel(t *testing.T, h *hc.Host) (revier.TargetRef, revier.PanelID) {
	t.Helper()
	ref, panel, done := endingPanel(t, h)
	if err := os.WriteFile(done, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	waitForRecords(t, ctx(t), h, ref, "finished")
	return ref, panel
}

// hcPath is the hc binary a window names by its path.
func hcPath(t *testing.T) string {
	t.Helper()
	bin, err := hc.Program()
	if err != nil {
		t.Fatal(err)
	}
	if bin, err = exec.LookPath(bin); err != nil {
		t.Fatal(err)
	}
	return bin
}

// A window whose command ended shows what it left and its exit code, though
// the program ended before hc's attach could show it, and removes the
// terminal on Enter (decisions.md D187).
func TestAWindowShowsWhatAnEndedCommandLeft(t *testing.T) {
	h := world(t)
	ref, panel := endedPanel(t, h)
	in, enter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = in.Close() }()
	if _, err := enter.WriteString("\n"); err != nil {
		t.Fatal(err)
	}
	_ = enter.Close()
	out, err := os.Create(filepath.Join(t.TempDir(), "window"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = out.Close() }()

	if err := h.Attach(ctx(t), hcPath(t), panel, in, out); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	shown, err := os.ReadFile(out.Name())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(shown), "no such config") || !strings.HasSuffix(string(shown), "[sh exited with code 3; press Enter to close]") {
		t.Errorf("the window shows %q, want the program's line and its exit code", shown)
	}
	waitForRecords(t, ctx(t), h, ref, "")
}

// A terminal a close ended holds no window.
func TestAClosedTerminalHoldsNoWindow(t *testing.T) {
	h, c := world(t), ctx(t)
	ref, err := h.Open(c, revier.Realization{Name: "home", Launch: []string{"sh", "-c", "exec sleep 30"}})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	panel := waitFor(t, h, func(in []revier.Instance) bool { return len(in) == 1 })[0].Panels[0].ID
	if err := h.ClosePanel(c, ref, panel); err != nil {
		t.Fatalf("ClosePanel: %v", err)
	}
	var out strings.Builder
	if held, err := h.Hold(c, panel, strings.NewReader(""), &out); err != nil || held || out.Len() != 0 {
		t.Errorf("Hold = %v, %v and showed %q; want not held", held, err, out.String())
	}
}

// A held terminal is its workspace's panel until revier closes it, as a
// shutdown or an agent close does: the close removes it, and the held window
// ends with no Enter.
func TestAHeldWindowEndsWithTheCloseOfItsTerminal(t *testing.T) {
	h := world(t)
	ref, panel := endedPanel(t, h)
	never, wait, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = never.Close(); _ = wait.Close() }()
	done := make(chan error, 1)
	go func() {
		_, err := h.Hold(ctx(t), panel, never, &bytes.Buffer{})
		done <- err
	}()
	got := waitFor(t, h, func(in []revier.Instance) bool { return len(in) == 1 })
	if got[0].Ref != ref || len(got[0].Panels) != 1 || got[0].Panels[0].ID != panel {
		t.Fatalf("Instances = %+v, want the held terminal's instance", got)
	}
	if err := h.ClosePanel(ctx(t), ref, panel); err != nil {
		t.Fatalf("ClosePanel of the held terminal: %v", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Hold = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the held window still waits after the close")
	}
	waitForRecords(t, ctx(t), h, ref, "")
}

// Once "hc server stop" ended the core, a window ends and starts no core: any
// hc command would.
func TestAWindowStartsNoCoreOnceItIsGone(t *testing.T) {
	h := world(t)
	_, panel := endedPanel(t, h)
	stop(h)
	var out strings.Builder
	if held, err := h.Hold(ctx(t), panel, strings.NewReader(""), &out); err != nil || held {
		t.Errorf("Hold = %v, %v; want not held", held, err)
	}
	if _, err := h.CorePID(ctx(t)); err == nil {
		t.Error("a core answers after the window ended; want none started")
	}
}

// A finished terminal no window took is removed by the next sweep once a
// minute has passed since its end, by the time the core records.
func TestASweepRemovesAnEndedTerminalAfterAMinute(t *testing.T) {
	h := world(t)
	ref, _ := endedPanel(t, h)
	c := ctx(t)
	if err := h.Sweep(c, time.Now()); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	waitForRecords(t, c, h, ref, "finished")
	if err := h.Sweep(c, time.Now().Add(2*time.Minute)); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	waitForRecords(t, c, h, ref, "")
}

// A view's window, on a terminal of its own, attaches the panel while its
// command runs; once the command ends the window shows what it left and its
// exit code, its workspace still lists it, and it stays until Enter
// (decisions.md D187). The test binary is revier here.
func TestAViewsWindowIsHeldUntilEnterOnceItsCommandEnds(t *testing.T) {
	script, err := exec.LookPath("script")
	if err != nil {
		t.Skip("script is not installed; the attach needs a terminal")
	}
	h, c := world(t), ctx(t)
	h.Revier = os.Args[0]
	ref, panel, done := endingPanel(t, h)
	argv, err := h.AttachPanel(panel)
	if err != nil {
		t.Fatalf("AttachPanel: %v", err)
	}
	window := windowCmd(script, argv)
	enter, err := window.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	var shown syncBuffer
	window.Stdout = &shown
	if err := window.Start(); err != nil {
		t.Fatal(err)
	}
	ended := make(chan error, 1)
	go func() { ended <- window.Wait() }()
	t.Cleanup(func() { _ = window.Process.Kill() })

	time.Sleep(time.Second)
	if err := os.WriteFile(done, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	const held = "[sh exited with code 3; press Enter to close]"
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(shown.String(), held) {
		select {
		case err := <-ended:
			t.Fatalf("the window ended unheld: %v; it showed %q", err, shown.String())
		case <-time.After(50 * time.Millisecond):
		}
		if time.Now().After(deadline) {
			t.Fatalf("the window never held; it showed %q", shown.String())
		}
	}
	if !strings.Contains(shown.String(), "no such config") {
		t.Errorf("the held window shows %q, want the command's line", shown.String())
	}
	if got, err := h.Instances(c); err != nil || len(got) != 1 || got[0].Ref != ref {
		t.Errorf("Instances while held = %+v, %v; want the workspace", got, err)
	}
	select {
	case err := <-ended:
		t.Fatalf("the held window ended before Enter: %v", err)
	case <-time.After(500 * time.Millisecond):
	}
	if _, err := enter.Write([]byte("\r")); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ended:
	case <-time.After(5 * time.Second):
		t.Fatal("the held window still runs after Enter")
	}
	waitForRecords(t, c, h, ref, "")
}

// syncBuffer is a buffer a process writes and a test reads at once.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}
