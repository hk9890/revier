//go:build live

package herdr_test

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/hk9890/revier/internal/adapter/herdr"
)

// server starts a herdr server of this test's own, headless, with its config,
// state and socket in a scratch directory, and stops it when the test ends.
// Nothing attaches a client, and the user's own herdr is never reached: the
// environment that names it is left out of the server's.
func server(t *testing.T) *herdr.Host {
	t.Helper()
	bin, err := exec.LookPath("herdr")
	if err != nil {
		t.Skip("herdr not installed; L4 needs it")
	}
	// Short: a Unix socket path longer than 107 bytes cannot be bound, and
	// t.TempDir carries the test's name.
	dir, err := os.MkdirTemp("", "rvh")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sock := filepath.Join(dir, "s.sock")

	cmd := exec.Command(bin, "server")
	cmd.Env = append(scrubbed(os.Environ()),
		"XDG_CONFIG_HOME="+dir, "XDG_STATE_HOME="+dir, "HERDR_SOCKET_PATH="+sock)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start herdr server: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		_, _ = cmd.Process.Wait()
	})

	deadline := time.Now().Add(10 * time.Second)
	for {
		var pong map[string]any
		if call(t, sock, "ping", struct{}{}, &pong) == nil {
			return &herdr.Host{Socket: sock}
		}
		if time.Now().After(deadline) {
			t.Fatalf("herdr server on %s never answered ping", sock)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// scrubbed is the environment without what points herdr at another server or
// tells it it runs inside one.
func scrubbed(env []string) []string {
	var out []string
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		switch name {
		case "HERDR_ENV", "HERDR_SESSION", "HERDR_SOCKET_PATH", "HERDR_CONFIG_PATH",
			"HERDR_PANE_ID", "HERDR_TAB_ID", "HERDR_WORKSPACE_ID", "XDG_CONFIG_HOME", "XDG_STATE_HOME":
			continue
		}
		out = append(out, kv)
	}
	return out
}

// call sends one request to the server on sock and decodes its result into
// out. It is how a test arranges the server: the host does not open yet.
func call(t *testing.T, sock, method string, params, out any) error {
	t.Helper()
	conn, err := net.Dial("unix", sock)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	line, _ := json.Marshal(map[string]any{"id": "test", "method": method, "params": params})
	if _, err := conn.Write(append(line, '\n')); err != nil {
		return err
	}
	reply, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		return err
	}
	var resp struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code, Message string
		} `json:"error"`
	}
	if err := json.Unmarshal(reply, &resp); err != nil {
		return err
	}
	if resp.Error != nil {
		t.Fatalf("herdr %s: %s: %s", method, resp.Error.Code, resp.Error.Message)
	}
	return json.Unmarshal(resp.Result, out)
}

func ctx(t *testing.T) context.Context {
	c, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	return c
}
