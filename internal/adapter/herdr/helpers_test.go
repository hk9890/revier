package herdr_test

import (
	"bufio"
	"encoding/json"
	"net"
	"path/filepath"
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
