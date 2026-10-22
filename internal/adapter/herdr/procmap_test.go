// Which process is a pane's, against a /proc tree the test writes. No herdr
// and no process, so the default layer runs it.
package herdr_test

import (
	"maps"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/adapter/herdr"
)

// A pane's process is the topmost that carries the pane's id and the server's
// socket: what it starts inherits both and is deeper, a process of another
// server's socket is that server's, and a process that outlived its server
// under a new parent is still its pane's - unless the server has a child of
// that pane id, which an older process left behind with a lower pid cannot
// displace.
func TestAPanesProcessIsTheTopmostWithItsIdAndSocket(t *testing.T) {
	root := t.TempDir()
	const sock = "/run/herdr.sock"
	write := func(pid, ppid int, env ...string) {
		dir := filepath.Join(root, strconv.Itoa(pid))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		stat := strconv.Itoa(pid) + " (a (b) c) S " + strconv.Itoa(ppid) + " 1 1 0 -1\n"
		if err := os.WriteFile(filepath.Join(dir, "stat"), []byte(stat), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "environ"), []byte(strings.Join(env, "\x00")+"\x00"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(50, 1, "HERDR_PANE_ID=w1:p2", "HERDR_SOCKET_PATH="+sock)                  // left by an earlier server
	write(100, 1, "HOME=/home/u")                                                   // the server
	write(101, 100, "HERDR_PANE_ID=w1:p1", "HERDR_SOCKET_PATH="+sock)               // a shell
	write(102, 101, "HERDR_PANE_ID=w1:p1", "HERDR_SOCKET_PATH="+sock)               // what it runs
	write(103, 100, "HERDR_PANE_ID=w1:p2", "HERDR_SOCKET_PATH="+sock+"/")           // the same socket, written otherwise
	write(104, 1, "HERDR_PANE_ID=w2:p1", "HERDR_SOCKET_PATH="+sock)                 // outlived a handoff
	write(105, 1, "HERDR_PANE_ID=w1:p1", "HERDR_SOCKET_PATH=/elsewhere/herdr.sock") // another server's pane

	got := herdr.PaneProcesses(root, sock, 100)
	want := map[string]int{"w1:p1": 101, "w1:p2": 103, "w2:p1": 104}
	if !maps.Equal(got, want) {
		t.Errorf("panes = %v, want %v", got, want)
	}
}
