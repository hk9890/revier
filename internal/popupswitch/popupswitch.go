// Package popupswitch is the socket the popup's surface listens on, and the
// press that tells it to switch its list there (decisions.md D182). The
// desktop takes the trigger key before the popup's terminal sees it, so the
// second press reaches the surface here, whatever runtime runs the popup.
//
// The socket is a file in the runtime directory, which only the user can
// enter. One line, "switch", is the whole of what a press says.
package popupswitch

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// message is what a press writes, and what the surface switches on.
const message = "switch"

// readWait bounds how long the surface waits for the line of a press that
// connected: a client that connects and says nothing must not keep the next
// press out.
const readWait = time.Second

// ErrNoRuntimeDir means the environment names no directory that is the
// user's alone, so there is no socket: the surface listens on none, and a
// press switches nothing.
var ErrNoRuntimeDir = errors.New("popup switch: XDG_RUNTIME_DIR is not set to an absolute path")

// Path is the socket's file: revier-popup.sock in the runtime directory. A
// relative XDG_RUNTIME_DIR names none, as it would resolve where every user
// can enter.
func Path() (string, error) {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if !filepath.IsAbs(dir) {
		return "", ErrNoRuntimeDir
	}
	return filepath.Join(dir, "revier-popup.sock"), nil
}

// Listen binds the socket at path, readable and writable by the user alone.
// Closing the listener removes the file. A file left by a surface that was
// killed is taken over; one a surface still answers on is not, and Listen
// fails, so two popups never take each other's presses.
func Listen(path string) (*net.UnixListener, error) {
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil && errors.Is(err, syscall.EADDRINUSE) {
		if live, dialErr := net.DialTimeout("unix", path, readWait); dialErr == nil {
			_ = live.Close()
			return nil, fmt.Errorf("popup switch: %s: a surface listens on it already", path)
		}
		if rmErr := os.Remove(path); rmErr != nil {
			return nil, fmt.Errorf("popup switch: remove the stale %s: %w", path, rmErr)
		}
		ln, err = net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	}
	if err != nil {
		return nil, fmt.Errorf("popup switch: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = ln.Close()
		return nil, fmt.Errorf("popup switch: %w", err)
	}
	return ln, nil
}

// Await waits for the next press on ln, and returns once one said switch.
// A connection that says anything else, or nothing, is dropped and the wait
// goes on. It returns the listener's error once ln is closed.
func Await(ln net.Listener) error {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return err
		}
		_ = conn.SetReadDeadline(time.Now().Add(readWait))
		line, _ := bufio.NewReader(conn).ReadString('\n')
		_ = conn.Close()
		if strings.TrimSpace(line) == message {
			return nil
		}
	}
}

// Switch tells the surface that listens at path to switch its list. It fails
// when none listens there: a popup that an older revier started, or one that
// found no runtime directory.
func Switch(ctx context.Context, path string) error {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", path)
	if err != nil {
		return fmt.Errorf("popup switch: %w", err)
	}
	defer func() { _ = conn.Close() }()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetWriteDeadline(deadline)
	}
	if _, err := conn.Write([]byte(message + "\n")); err != nil {
		return fmt.Errorf("popup switch: %w", err)
	}
	return nil
}
