package herdr

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// paneProcesses maps each pane of the server on socket to the process herdr
// started in it, from one read of /proc.
//
// herdr's listing names no process, and asking for one is a request per pane,
// which the hot path cannot afford. Every process herdr starts carries
// HERDR_PANE_ID and HERDR_SOCKET_PATH in its environment instead, and so does
// everything that process starts. The pane's own process is the topmost one:
// its parent has another pane id, or none. That holds also for a process that
// outlived the server it was started under, as a live handoff to a new herdr
// binary leaves it, where it is no longer the server's child.
//
// The environment of another user's process cannot be read, and is passed
// over, as is a process that ended meanwhile.
func paneProcesses(root, socket string) map[string]int {
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
		dir := filepath.Join(root, e.Name())
		env, err := os.ReadFile(filepath.Join(dir, "environ"))
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
		ppid, ok := parent(dir)
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
		if cur, ok := out[p.pane]; !ok || pid < cur {
			out[p.pane] = pid
		}
	}
	return out
}

// parent reads the parent pid from a process's stat. The command name is in
// parentheses and may hold spaces and parentheses of its own, so the fields
// are counted from its last closing one: state, then ppid.
func parent(dir string) (int, bool) {
	stat, err := os.ReadFile(filepath.Join(dir, "stat"))
	if err != nil {
		return 0, false
	}
	fields := strings.Fields(string(stat[bytes.LastIndexByte(stat, ')')+1:]))
	if len(fields) < 2 {
		return 0, false
	}
	ppid, err := strconv.Atoi(fields[1])
	return ppid, err == nil
}
