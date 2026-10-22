package herdr_test

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// serve listens on a socket of its own and answers each request line with
// what answer returns for it.
func serve(t *testing.T, answer func(req map[string]any) string) string {
	t.Helper()
	sock := filepath.Join(t.TempDir(), "s.sock")
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			line, err := bufio.NewReader(conn).ReadBytes('\n')
			if err == nil {
				var req map[string]any
				_ = json.Unmarshal(line, &req)
				_, _ = conn.Write([]byte(answer(req) + "\n"))
			}
			_ = conn.Close()
		}
	}()
	return sock
}

// writeProc writes a process into a fake /proc at root: its parent in stat,
// with a command name that holds spaces and parentheses, and its environment.
func writeProc(t *testing.T, root string, pid, ppid int, env ...string) {
	t.Helper()
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
