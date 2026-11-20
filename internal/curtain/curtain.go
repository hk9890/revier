// Package curtain is the socket between the surface that runs a shutdown or
// a restore and its curtains, one on each monitor (decisions.md D192). The
// surface listens and sends the state of the walk; each curtain connects,
// draws the latest state, and sends back what the user pressed.
//
// The socket is a file in the runtime directory, which only the user can
// enter, named after the surface's pid. Each message is one JSON line. A
// curtain lives as long as its connection: when the surface closes the
// socket, or ends, the curtain ends with it.
package curtain

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"
)

// RowState is how far the walk came with one step.
type RowState string

const (
	RowWaiting RowState = "waiting"
	RowRunning RowState = "running"
	RowDone    RowState = "done"
	RowFailed  RowState = "failed"
	// RowSkipped is a step left open, or one the walk did not reach.
	RowSkipped RowState = "skipped"
)

// Row is one step of the walk: a target of a project.
type Row struct {
	Project string   `json:"project"`
	Label   string   `json:"label"`
	State   RowState `json:"state"`
	Note    string   `json:"note,omitempty"`
}

// End is how the walk ended.
type End struct {
	OK bool `json:"ok"`
	// Title names the end, as "Restore done" or "Shutdown cancelled".
	Title string `json:"title"`
	// Summary counts the steps, as "7 opened · 1 failed".
	Summary string `json:"summary"`
}

// State is the whole of what a curtain draws.
type State struct {
	// Mode is "restore" or "shutdown".
	Mode string `json:"mode"`
	// Line says what runs now, as "opening 3 of 8: editor".
	Line string `json:"line"`
	// N is the steps done, Of the steps in the walk.
	N          int   `json:"n"`
	Of         int   `json:"of"`
	Rows       []Row `json:"rows"`
	Cancelling bool  `json:"cancelling,omitempty"`
	// End is nil while the walk runs.
	End *End `json:"end,omitempty"`
}

// Command is what the user pressed on a curtain.
type Command string

const (
	// Cancel stops the walk after the step that runs.
	Cancel Command = "cancel"
	// Quit is the second ctrl+c: the surface quits at once.
	Quit Command = "quit"
	// Hide uncovers the desktop while the walk goes on.
	Hide Command = "hide"
	// Close is the summary's Close: the curtains end.
	Close Command = "close"
	// Gone is the server's own, never a curtain's: the last curtain
	// connected has gone, and none is left to read the walk.
	Gone Command = "gone"
)

// dialWait bounds the dial that tells a live socket from a stale file.
const dialWait = time.Second

// ErrNoRuntimeDir means the environment names no directory that is the
// user's alone, so there is no socket and no curtain.
var ErrNoRuntimeDir = errors.New("curtain: XDG_RUNTIME_DIR is not set to an absolute path")

// Path is the socket's file: revier-curtain-<pid>.sock in the runtime
// directory. A relative XDG_RUNTIME_DIR names none, as it would resolve where
// every user can enter.
func Path() (string, error) {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if !filepath.IsAbs(dir) {
		return "", ErrNoRuntimeDir
	}
	return filepath.Join(dir, "revier-curtain-"+strconv.Itoa(os.Getpid())+".sock"), nil
}

// Server is the surface's end of the socket. It is safe for concurrent use.
type Server struct {
	ln       *net.UnixListener
	path     string
	commands chan Command
	done     chan struct{}
	wg       sync.WaitGroup

	mu     sync.Mutex
	latest []byte // the latest state, encoded; nil before the first Send
	seq    int    // counts the Sends, so a writer skips a state it wrote
	conns  map[*serverConn]struct{}
	closed bool
	joined int           // the connections accepted, gone ones included
	join   chan struct{} // closed and replaced at each one accepted
}

// serverConn is one curtain's connection. Its writer wakes on each Send and
// writes the state that is latest then: a curtain that reads slowly misses
// the states between, and never holds Send.
type serverConn struct {
	conn net.Conn
	wake chan struct{}
}

// Listen binds the socket at Path, readable and writable by the user alone.
// A file left by a surface that was killed with the same pid is taken over;
// one a surface still answers on is not.
func Listen() (*Server, error) {
	path, err := Path()
	if err != nil {
		return nil, err
	}
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil && errors.Is(err, syscall.EADDRINUSE) {
		if live, dialErr := net.DialTimeout("unix", path, dialWait); dialErr == nil {
			_ = live.Close()
			return nil, fmt.Errorf("curtain: %s: a surface listens on it already", path)
		}
		if rmErr := os.Remove(path); rmErr != nil {
			return nil, fmt.Errorf("curtain: remove the stale %s: %w", path, rmErr)
		}
		ln, err = net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	}
	if err != nil {
		return nil, fmt.Errorf("curtain: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = ln.Close()
		return nil, fmt.Errorf("curtain: %w", err)
	}
	s := &Server{
		ln:       ln,
		path:     path,
		commands: make(chan Command, 16),
		done:     make(chan struct{}),
		conns:    map[*serverConn]struct{}{},
		join:     make(chan struct{}),
	}
	s.wg.Add(1)
	go s.accept()
	return s, nil
}

// Path is the socket's file, which a curtain dials.
func (s *Server) Path() string { return s.path }

// Commands delivers what the curtains' users pressed. It is closed once the
// server is.
func (s *Server) Commands() <-chan Command { return s.commands }

// Send makes st the latest state, and writes it to every curtain connected.
// It never waits for a curtain.
func (s *Server) Send(st State) {
	// A State holds strings, ints and bools alone: it always encodes.
	line, _ := json.Marshal(st)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.latest = append(line, '\n')
	s.seq++
	for c := range s.conns {
		c.signal()
	}
}

// Close closes the socket and every connection, and removes the file: every
// curtain ends. It returns once nothing of the server runs.
func (s *Server) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	close(s.done)
	_ = s.ln.Close()
	for c := range s.conns {
		_ = c.conn.Close()
	}
	s.mu.Unlock()
	s.wg.Wait()
	close(s.commands)
}

// AwaitJoined waits until n curtains have connected, the ones gone since
// included. It fails once ctx ends or the server is closed first.
func (s *Server) AwaitJoined(ctx context.Context, n int) error {
	for {
		s.mu.Lock()
		joined, join := s.joined, s.join
		s.mu.Unlock()
		if joined >= n {
			return nil
		}
		select {
		case <-join:
		case <-s.done:
			return errors.New("curtain: the server closed")
		case <-ctx.Done():
			return fmt.Errorf("curtain: %d of %d curtains connected: %w", joined, n, ctx.Err())
		}
	}
}

func (s *Server) accept() {
	defer s.wg.Done()
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		c := &serverConn{conn: conn, wake: make(chan struct{}, 1)}
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			_ = conn.Close()
			return
		}
		s.conns[c] = struct{}{}
		s.joined++
		close(s.join)
		s.join = make(chan struct{})
		if s.latest != nil {
			c.signal()
		}
		s.mu.Unlock()
		s.wg.Add(2)
		go s.write(c)
		go s.read(c)
	}
}

// signal wakes the writer, unless a wake is pending already.
func (c *serverConn) signal() {
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

func (s *Server) write(c *serverConn) {
	defer s.wg.Done()
	wrote := 0
	for {
		select {
		case <-s.done:
			return
		case <-c.wake:
		}
		s.mu.Lock()
		line, seq := s.latest, s.seq
		s.mu.Unlock()
		if seq == wrote {
			continue
		}
		if _, err := c.conn.Write(line); err != nil {
			return
		}
		wrote = seq
	}
}

func (s *Server) read(c *serverConn) {
	defer s.wg.Done()
	defer s.drop(c)
	scanner := bufio.NewScanner(c.conn)
	for scanner.Scan() {
		var cmd Command
		if err := json.Unmarshal(scanner.Bytes(), &cmd); err != nil {
			continue
		}
		select {
		case s.commands <- cmd:
		case <-s.done:
			return
		}
	}
}

// drop forgets a connection that ended, and says Gone when it was the last
// one of a server still open.
func (s *Server) drop(c *serverConn) {
	s.mu.Lock()
	delete(s.conns, c)
	gone := len(s.conns) == 0 && !s.closed
	s.mu.Unlock()
	_ = c.conn.Close()
	if gone {
		select {
		case s.commands <- Gone:
		case <-s.done:
		}
	}
}

// Client is a curtain's end of the socket.
type Client struct {
	conn   net.Conn
	states chan State
	done   chan struct{}
	once   sync.Once
	mu     sync.Mutex // one Send writes at a time
}

// Dial connects to the server whose socket is at path.
func Dial(path string) (*Client, error) {
	conn, err := net.DialTimeout("unix", path, dialWait)
	if err != nil {
		return nil, fmt.Errorf("curtain: %w", err)
	}
	c := &Client{conn: conn, states: make(chan State), done: make(chan struct{})}
	go c.read()
	return c, nil
}

// States delivers each state the server sent. It is closed once the
// connection ends: the server closed it, or the surface ended.
func (c *Client) States() <-chan State { return c.states }

// Send tells the server what the user pressed.
func (c *Client) Send(cmd Command) error {
	// A Command is a string: it always encodes.
	line, _ := json.Marshal(cmd)
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, err := c.conn.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("curtain: %w", err)
	}
	return nil
}

// Close ends the connection.
func (c *Client) Close() {
	c.once.Do(func() { close(c.done) })
	_ = c.conn.Close()
}

func (c *Client) read() {
	defer close(c.states)
	scanner := bufio.NewScanner(c.conn)
	scanner.Buffer(nil, 1<<20)
	for scanner.Scan() {
		var st State
		if err := json.Unmarshal(scanner.Bytes(), &st); err != nil {
			continue
		}
		select {
		case c.states <- st:
		case <-c.done:
			return
		}
	}
}
