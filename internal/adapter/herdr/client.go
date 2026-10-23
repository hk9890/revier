package herdr

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// socketPath is the API socket of the server herdr's own CLI would talk to
// from this environment: HERDR_SOCKET_PATH, else the socket of the session
// HERDR_SESSION names, else the default session's.
func socketPath() string {
	if path := os.Getenv("HERDR_SOCKET_PATH"); path != "" {
		return path
	}
	dir := configDir()
	if name := os.Getenv("HERDR_SESSION"); name != "" && name != "default" {
		dir = filepath.Join(dir, "sessions", name)
	}
	return filepath.Join(dir, "herdr.sock")
}

func configDir() string {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "herdr")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "herdr")
}

// errNoServer is a socket nobody listens on: no herdr server runs yet.
var errNoServer = errors.New("herdr: no server running")

// request sends one request on a connection of its own and decodes the
// result into out. It returns the pid of the server that answered, which the
// kernel reports for the peer of a Unix socket, so no request asks for it.
func (h *Host) request(ctx context.Context, method string, params, out any) (int, error) {
	if h.call != nil {
		pid, raw, err := h.call(ctx, method, params)
		if err != nil {
			return 0, err
		}
		return pid, decode(method, raw, out)
	}

	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", h.socket())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ECONNREFUSED) {
			return 0, errNoServer
		}
		return 0, fmt.Errorf("herdr %s: %w", method, err)
	}
	defer func() { _ = conn.Close() }()
	// A server that accepts and never answers must not hold the caller past
	// its context: an end of ctx, by deadline or by cancel, ends the exchange.
	stop := context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Now()) })
	defer stop()
	fail := func(err error) (int, error) {
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		return 0, fmt.Errorf("herdr %s: %w", method, err)
	}

	pid, err := peerPID(conn.(*net.UnixConn))
	if err != nil {
		return fail(err)
	}

	line, err := json.Marshal(map[string]any{"id": "revier", "method": method, "params": params})
	if err != nil {
		return fail(err)
	}
	if _, err := conn.Write(append(line, '\n')); err != nil {
		return fail(err)
	}
	reply, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		return fail(err)
	}
	var resp response
	if err := json.Unmarshal(reply, &resp); err != nil {
		return fail(err)
	}
	if resp.Error != nil {
		resp.Error.Method = method
		return 0, resp.Error
	}
	return pid, decode(method, resp.Result, out)
}

func decode(method string, raw json.RawMessage, out any) error {
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("herdr %s: %w", method, err)
	}
	return nil
}

func (h *Host) socket() string {
	if h.Socket != "" {
		return h.Socket
	}
	return socketPath()
}

// peerPID is the pid of the process at the other end of a Unix socket.
func peerPID(conn *net.UnixConn) (int, error) {
	raw, err := conn.SyscallConn()
	if err != nil {
		return 0, err
	}
	var cred *syscall.Ucred
	var credErr error
	if err := raw.Control(func(fd uintptr) {
		cred, credErr = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	}); err != nil {
		return 0, err
	}
	if credErr != nil {
		return 0, credErr
	}
	return int(cred.Pid), nil
}
