package herdr_test

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/adapter/herdr"
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

// writeProc writes a process into a fake /proc at root: a shell, sh, in the
// foreground of a terminal of its own. Its stat names its parent, with a
// command name that holds spaces and parentheses; environ holds env.
func writeProc(t *testing.T, root string, pid, ppid int, env ...string) {
	t.Helper()
	dir := filepath.Join(root, strconv.Itoa(pid))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	id := strconv.Itoa(pid)
	// state, ppid, pgrp, session, tty_nr, tpgid.
	stat := id + " (a (b) c) S " + strconv.Itoa(ppid) + " " + id + " " + id + " 34816 " + id + "\n"
	for name, content := range map[string]string{
		"stat":    stat,
		"cmdline": "sh\x00",
		"environ": strings.Join(env, "\x00") + "\x00",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// recording is a host whose server has pid 9 and answers every request with
// an empty result, and the methods it was asked. Its /proc is empty, so the
// server's PATH is this process's.
func recording(t *testing.T) (*herdr.Host, *[]string) {
	var asked []string
	h := &herdr.Host{}
	h.SetProcRoot(t.TempDir())
	h.SetCaller(func(_ context.Context, method string, _ any) (int, []byte, error) {
		asked = append(asked, method)
		return 9, []byte(`{}`), nil
	})
	return h, &asked
}
