//go:build live

// Layer L4: real processes read from this machine's /proc. A private tmux
// server gives each one the terminal sshd gives a served process, so it is in
// the foreground of a pty as a panel's process is.
package proc_test

import (
	"context"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hk9890/revier/internal/adapter/proc"
	"github.com/hk9890/revier/pkg/revier"
)

// A process started with a named agent's name in its environment is reported
// with that name as its panel's mark, and one started without it with none.
func TestAProcessStartedAsANamedAgentCarriesItsMark(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed; L4 needs it for a terminal")
	}
	socket := "revier-test-" + t.Name()
	workspace := "session:" + t.Name()
	t.Cleanup(func() { _ = exec.Command("tmux", "-L", socket, "kill-server").Run() })
	start := func(session, tag string, env ...string) {
		t.Helper()
		args := []string{"-L", socket, "-f", "/dev/null", "new-session", "-d", "-s", session,
			"-e", proc.TagVar + "=" + tag, "-e", proc.WorkspaceVar + "=" + workspace}
		for _, e := range env {
			args = append(args, "-e", e)
		}
		// The server's environment is every pane's: a name this test runs
		// under, inside an agent served here, would mark the default one.
		cmd := exec.Command("tmux", append(args, "exec sleep 600")...)
		cmd.Env = slices.DeleteFunc(os.Environ(), func(kv string) bool { return strings.HasPrefix(kv, proc.AgentVar+"=") })
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("tmux new-session: %v: %s", err, out)
		}
	}
	start("default", "box.1")
	start("named", "box.2", proc.AgentVar+"=dtop")

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
