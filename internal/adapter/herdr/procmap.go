package herdr

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/hk9890/revier/internal/adapter/proc"
)

// paneProcesses maps each pane of the server on socket, whose pid is server,
// to the process herdr started in it, from one read of /proc.
//
// herdr's listing names no process, and asking for one is a request per pane,
// which the hot path cannot afford. Every process herdr starts carries
// HERDR_PANE_ID and HERDR_SOCKET_PATH in its environment instead, and so does
// everything that process starts. The pane's own process is the topmost one:
// its parent has another pane id, or none.
//
// Several can be topmost for one pane: a daemon the pane started has left the
// tree, and a process an earlier server on this socket left behind carries a
// pane id the new server numbers again. The child of the server wins, and with
// none the lowest pid: a process that outlived the server it was started
// under, as a live handoff to a new herdr binary leaves it, is no longer the
// server's child.
//
// The environment of another user's process cannot be read, and is passed
// over, as is a process that ended meanwhile.
func paneProcesses(root, socket string, server int) map[string]int {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	type process struct {
		pane string
		ppid int
	}
	byPID := map[int]process{}
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		env, err := os.ReadFile(filepath.Join(root, e.Name(), "environ"))
		if err != nil || !bytes.Contains(env, []byte("HERDR_PANE_ID=")) {
			continue
		}
		pane, sock := "", ""
		for _, kv := range strings.Split(string(env), "\x00") {
			name, value, _ := strings.Cut(kv, "=")
			switch name {
			case "HERDR_PANE_ID":
				pane = value
			case "HERDR_SOCKET_PATH":
				sock = value
			}
		}
		if pane == "" || filepath.Clean(sock) != filepath.Clean(socket) {
			continue
		}
		ppid, ok := proc.ParentIn(root, pid)
		if !ok {
			continue
		}
		byPID[pid] = process{pane: pane, ppid: ppid}
	}

	out := map[string]int{}
	for pid, p := range byPID {
		if up, ok := byPID[p.ppid]; ok && up.pane == p.pane {
			continue
		}
		cur, ok := out[p.pane]
		child, curChild := p.ppid == server, byPID[cur].ppid == server
		if !ok || child && !curChild || child == curChild && pid < cur {
			out[p.pane] = pid
		}
	}
	return out
}
