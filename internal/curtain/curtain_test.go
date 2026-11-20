package curtain_test

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/hk9890/revier/internal/curtain"
)

// wait bounds every wait of a test for what the other end sent.
const wait = 5 * time.Second

// runtimeDir is a runtime directory of the test's own, short enough for a
// socket path.
func runtimeDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "rc")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	t.Setenv("XDG_RUNTIME_DIR", dir)
	return dir
}

// listen starts a server in a runtime directory of the test's own and closes
// it at the end.
func listen(t *testing.T) *curtain.Server {
	t.Helper()
	runtimeDir(t)
	s, err := curtain.Listen()
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	t.Cleanup(s.Close)
	return s
}

// dial connects a client to s and closes it at the end.
func dial(t *testing.T, s *curtain.Server) *curtain.Client {
	t.Helper()
	c, err := curtain.Dial(s.Path())
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(c.Close)
	return c
}

// next is the next state the client got, or a failed test when none comes.
func next(t *testing.T, c *curtain.Client) curtain.State {
	t.Helper()
	select {
	case st, ok := <-c.States():
		if !ok {
			t.Fatal("States closed, want a state")
		}
		return st
	case <-time.After(wait):
		t.Fatal("no state in time")
	}
	return curtain.State{}
}

// running is a state of a walk half done.
var running = curtain.State{
	Mode: "restore", Line: "opening 2 of 3: editor", N: 1, Of: 3,
	Rows: []curtain.Row{
		{Project: "alpha", Label: "home", State: curtain.RowDone},
		{Project: "alpha", Label: "editor", State: curtain.RowRunning},
		{Project: "bravo", Label: "home", State: curtain.RowWaiting},
	},
}

// The socket is in the runtime directory and named after the surface's pid;
// there is none where the environment names no absolute directory.
func TestPathIsInTheRuntimeDirectory(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "/run/user/1000")
	want := "/run/user/1000/revier-curtain-" + strconv.Itoa(os.Getpid()) + ".sock"
	if path, err := curtain.Path(); err != nil || path != want {
		t.Errorf("Path = %q, %v; want %q", path, err, want)
	}
	for _, dir := range []string{"", "run/user/1000"} {
		t.Setenv("XDG_RUNTIME_DIR", dir)
		if path, err := curtain.Path(); !errors.Is(err, curtain.ErrNoRuntimeDir) {
			t.Errorf("XDG_RUNTIME_DIR=%q: Path = %q, %v; want ErrNoRuntimeDir", dir, path, err)
		}
	}
}

// A state the server sends reaches the curtain whole, and the socket is the
// user's alone.
func TestAStateReachesTheCurtain(t *testing.T) {
	s := listen(t)
	if info, err := os.Stat(s.Path()); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("socket = %v, %v; want mode 0600", info, err)
	}
	c := dial(t, s)
	s.Send(running)
	if got := next(t, c); !reflect.DeepEqual(got, running) {
		t.Errorf("state = %+v, want %+v", got, running)
	}
	end := running
	end.End = &curtain.End{OK: false, Title: "Restore finished with problems", Summary: "1 opened · 1 failed"}
	s.Send(end)
	if got := next(t, c); !reflect.DeepEqual(got, end) {
		t.Errorf("state = %+v, want %+v", got, end)
	}
}

// A curtain that connects late gets the latest state at once, not the ones
// before it.
func TestALateCurtainGetsTheLatestState(t *testing.T) {
	s := listen(t)
	first := running
	first.N = 0
	s.Send(first)
	s.Send(running)
	if got := next(t, dial(t, s)); !reflect.DeepEqual(got, running) {
		t.Errorf("state = %+v, want the latest %+v", got, running)
	}
}

// What the user presses on a curtain reaches the server, in order.
func TestCommandsReachTheServer(t *testing.T) {
	s := listen(t)
	c := dial(t, s)
	for _, cmd := range []curtain.Command{curtain.Cancel, curtain.Quit, curtain.Hide, curtain.Close} {
		if err := c.Send(cmd); err != nil {
			t.Fatalf("Send(%s): %v", cmd, err)
		}
		select {
		case got := <-s.Commands():
			if got != cmd {
				t.Errorf("command = %q, want %q", got, cmd)
			}
		case <-time.After(wait):
			t.Fatalf("command %q did not arrive", cmd)
		}
	}
}

// Closing the server ends every curtain's connection, which ends the
// curtain, and removes the socket's file.
func TestCloseEndsTheCurtain(t *testing.T) {
	s := listen(t)
	c := dial(t, s)
	s.Send(running)
	next(t, c)
	s.Close()
	select {
	case _, ok := <-c.States():
		if ok {
			t.Error("States delivered a state after the close, want it closed")
		}
	case <-time.After(wait):
		t.Fatal("States still open after the close")
	}
	if _, ok := <-s.Commands(); ok {
		t.Error("Commands delivered a command after the close, want it closed")
	}
	if _, err := os.Stat(s.Path()); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("socket file after the close: %v, want it removed", err)
	}
}

// A curtain that stopped reading never holds the walk: Send returns at once,
// whatever the curtain left unread.
func TestSendDoesNotWaitForACurtainThatStoppedReading(t *testing.T) {
	s := listen(t)
	conn, err := net.Dial("unix", s.Path())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	big := running
	for range 200 {
		big.Rows = append(big.Rows, curtain.Row{Project: "project-with-a-long-name", Label: "a target with a long label", State: curtain.RowWaiting, Note: "a note that takes room"})
	}
	done := make(chan struct{})
	go func() {
		for i := range 500 {
			big.N = i
			s.Send(big)
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(wait):
		t.Fatal("Send waited for a curtain that reads nothing")
	}
}

// The file a killed surface of the same pid left is taken over; a socket a
// surface still answers on is not.
func TestListenTakesOverAStaleSocketAndNoLiveOne(t *testing.T) {
	dir := runtimeDir(t)
	path := filepath.Join(dir, "revier-curtain-"+strconv.Itoa(os.Getpid())+".sock")
	stale, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	stale.SetUnlinkOnClose(false)
	_ = stale.Close()
	s, err := curtain.Listen()
	if err != nil {
		t.Fatalf("Listen over a stale socket: %v", err)
	}
	t.Cleanup(s.Close)
	if second, err := curtain.Listen(); err == nil {
		second.Close()
		t.Fatal("a second Listen took over a live socket")
	}
	c := dial(t, s)
	s.Send(running)
	next(t, c)
}

// The server says Gone once the last curtain connected has gone, and not
// while one is left.
func TestTheLastCurtainGoneIsSaid(t *testing.T) {
	s := listen(t)
	first, second := dial(t, s), dial(t, s)
	s.Send(running)
	next(t, first)
	next(t, second)
	command := func(want curtain.Command) {
		t.Helper()
		select {
		case got := <-s.Commands():
			if got != want {
				t.Fatalf("command = %q, want %q", got, want)
			}
		case <-time.After(wait):
			t.Fatalf("command %q did not arrive", want)
		}
	}
	first.Close()
	if err := second.Send(curtain.Hide); err != nil {
		t.Fatalf("Send: %v", err)
	}
	command(curtain.Hide)
	second.Close()
	command(curtain.Gone)
}

// AwaitJoined returns once the curtains it waits for have connected, a gone
// one included, and fails when they do not connect in time.
func TestAwaitJoinedWaitsForEveryCurtain(t *testing.T) {
	s := listen(t)
	dial(t, s).Close()
	short, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if err := s.AwaitJoined(short, 2); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("AwaitJoined with 1 of 2 connected = %v, want the deadline", err)
	}
	joined := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(t.Context(), wait)
		defer cancel()
		joined <- s.AwaitJoined(ctx, 2)
	}()
	dial(t, s)
	if err := <-joined; err != nil {
		t.Fatalf("AwaitJoined with 2 connected = %v, want nil", err)
	}
}
