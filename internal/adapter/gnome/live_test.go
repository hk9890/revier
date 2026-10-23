//go:build live

// Layer L4: what the GNOME host starts is a real process, and so is the wctl
// it runs. No GNOME session and no real wctl: Open launches and asks wctl
// nothing, and the wctl the runner test finds on PATH is a script.
package gnome_test

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/hk9890/revier/internal/adapter/gnome"
	"github.com/hk9890/revier/pkg/revier"
)

// A launched application outlives the keypress that started it. The context
// bounds the host calls around a launch and never the application: a TUI
// activation ends it the moment the new window is bound. The application also
// gets a session of its own, so the hangup of the terminal revier runs in -
// the popup closing - does not reach it.
func TestOpenOutlivesTheKeypress(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")
	ctx, cancel := context.WithCancel(context.Background())
	_, err := (&gnome.Host{}).Open(ctx, revier.Realization{
		Launch: []string{"sh", "-c", `echo $$ > "$0"; exec sleep 30`, pidFile},
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	var pid int
	for deadline := time.Now().Add(2 * time.Second); pid == 0 && time.Now().Before(deadline); {
		if b, err := os.ReadFile(pidFile); err == nil {
			pid, _ = strconv.Atoi(strings.TrimSpace(string(b)))
		}
		time.Sleep(10 * time.Millisecond)
	}
	if pid == 0 {
		t.Fatal("the launched command never started")
	}
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })

	cancel()
	time.Sleep(300 * time.Millisecond)
	if err := syscall.Kill(pid, 0); err != nil {
		t.Fatalf("the application died with the keypress context: %v", err)
	}
	// /proc/<pid>/stat: "pid (comm) state ppid pgrp session ...", and comm
	// may hold spaces, so the fields are counted from its closing paren.
	stat, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		t.Fatal(err)
	}
	after := strings.Fields(string(stat[strings.LastIndexByte(string(stat), ')')+1:]))
	if sid, _ := strconv.Atoi(after[3]); sid != pid {
		t.Errorf("session = %d, want one of its own (%d)", sid, pid)
	}
}

// wctl prints its version document on standard output and exits non-zero
// when the loaded extension is outside the versions it was built for, and
// the host still reads the document. The recorder of the default-layer tests
// hands the document over itself, so this one runs a wctl that is a process.
func TestProbeReadsTheDocumentOfAWctlThatExitsNonZero(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "argv")
	doc := `{"wctl":"0.17.0","expects_extension":"17","extension":"16","compatible":false,"capabilities":["no-animation"]}`
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> '" + log + "'\n" +
		"case \"$*\" in 'version --json') echo '" + doc + "'; exit 5;; esac\n"
	if err := os.WriteFile(filepath.Join(dir, "wctl"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_CURRENT_DESKTOP", "GNOME")

	h := &gnome.Host{}
	if err := h.Probe(context.Background()); err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if err := h.Hide(context.Background(), revier.TargetRef{Host: "gnome", ID: "4181121382"}); err != nil {
		t.Fatalf("Hide: %v", err)
	}
	got, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if want := "version --json\nminimize 4181121382 --no-animation\n"; string(got) != want {
		t.Errorf("argv = %q, want %q", got, want)
	}
}

// wctl runs in a process group of its own, so the ctrl+c a terminal sends to
// revier's group, which cancels a shutdown or a restore after its step, does
// not end the call that step makes.
func TestWctlRunsInAProcessGroupOfItsOwn(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "pgrp")
	script := "#!/bin/sh\ncut -d' ' -f5 /proc/$$/stat > '" + file + "'\n"
	if err := os.WriteFile(filepath.Join(dir, "wctl"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	_ = (&gnome.Host{}).Focus(context.Background(), revier.TargetRef{Host: "gnome", ID: "4181121382"})
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("wctl never ran: %v", err)
	}
	if pgrp, _ := strconv.Atoi(strings.TrimSpace(string(b))); pgrp == 0 || pgrp == syscall.Getpgrp() {
		t.Errorf("wctl's process group = %d, want one other than revier's (%d)", pgrp, syscall.Getpgrp())
	}
}
