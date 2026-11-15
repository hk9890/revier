//go:build live

// Layer L4: real processes read from this machine's /proc. Each one is
// started on a pty of its own, the terminal sshd gives a served process, so
// it is in the foreground of a pty as a panel's process is.
package proc_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/hk9890/revier/internal/adapter/proc"
	"github.com/hk9890/revier/pkg/revier"
)

// onPty starts sleep as the leader of a session whose terminal is a new pty,
// with env added to the test's environment, and ends it with the test.
func onPty(t *testing.T, env ...string) {
	t.Helper()
	ptmx, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		t.Skipf("no pty: %v", err)
	}
	t.Cleanup(func() { _ = ptmx.Close() })
	if err := unix.IoctlSetPointerInt(int(ptmx.Fd()), unix.TIOCSPTLCK, 0); err != nil {
		t.Fatal(err)
	}
	n, err := unix.IoctlGetInt(int(ptmx.Fd()), unix.TIOCGPTN)
	if err != nil {
		t.Fatal(err)
	}
	tty, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", n), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tty.Close() }()
	cmd := exec.Command("sleep", "600")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = tty, tty, tty
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true}
	// A name this test runs under, inside an agent served here, would mark
	// the unnamed one.
	cmd.Env = append(slices.DeleteFunc(os.Environ(), func(kv string) bool { return strings.HasPrefix(kv, proc.AgentVar+"=") }), env...)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
}

// A process started with a named agent's name in its environment is reported
// with that name as its panel's mark, and one started without it with none.
func TestAProcessStartedAsANamedAgentCarriesItsMark(t *testing.T) {
	workspace := "session:" + t.Name()
	onPty(t, proc.TagVar+"=box.1", proc.WorkspaceVar+"="+workspace)
	onPty(t, proc.TagVar+"=box.2", proc.WorkspaceVar+"="+workspace, proc.AgentVar+"=dtop")

	var panels []revier.Panel
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		got, err := (&proc.Host{}).Instances(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		for _, inst := range got {
			if inst.Ref.ID == workspace {
				panels = inst.Panels
			}
		}
		if len(panels) == 2 && panels[0].Runs("sleep") && panels[1].Runs("sleep") {
			break
		}
	}
	marks := map[revier.PanelID]string{}
	for _, p := range panels {
		marks[p.ID] = p.Vars[revier.PanelAgentVar]
	}
	if len(panels) != 2 || marks["box.1"] != "" || marks["box.2"] != "dtop" {
		t.Fatalf("panels = %+v, want box.1 unmarked and box.2 marked dtop", panels)
	}
}
