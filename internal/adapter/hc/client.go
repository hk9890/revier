package hc

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/hk9890/revier/internal/adapter/proc"
)

// generation is the protocol generation this host speaks.
const generation = 1

// needed is what a core must offer this host: the methods it calls, and the
// features SendText types with: framed mode, the attach, and
// AUTOMATIC_INPUT, the one way to type text with no key added.
var (
	neededMethods  = []string{"hello", "terminal.run", "terminal.list", "terminal.get", "terminal.kill", "terminal.rm", "terminal.read", "terminal.attach"}
	neededFeatures = []string{"framed", "attach", "automatic_input"}
)

// socketPath is the socket of the core hc's own CLI would talk to from this
// environment: HC_SOCKET, else the session's socket in the runtime
// directory, else beside the session's state. The session is HC_SESSION, or
// "default". A socket set in hc's config.toml is not read: revier would have
// to parse hc's configuration for a setting a user can give as HC_SOCKET.
func socketPath() string {
	if path := os.Getenv("HC_SOCKET"); path != "" {
		return path
	}
	session := strings.TrimSpace(os.Getenv("HC_SESSION"))
	if session == "" {
		session = "default"
	}
	name := session + ".sock"
	if run := os.Getenv("XDG_RUNTIME_DIR"); run != "" {
		return filepath.Join(run, "herdr-core", name)
	}
	if run := fmt.Sprintf("/run/user/%d", os.Getuid()); isDir(run) {
		return filepath.Join(run, "herdr-core", name)
	}
	state := os.Getenv("XDG_STATE_HOME")
	if state == "" {
		home, _ := os.UserHomeDir()
		state = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(state, "herdr-core", session, name)
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// errNoServer is a socket nobody listens on: no core runs yet.
var errNoServer = errors.New("hc: no core running")

// apiError is hc's JSON-RPC error. Kind is the stable part, such as
// spawn_failed; Message is for a person.
type apiError struct {
	Method  string `json:"-"`
	Message string `json:"message"`
	Data    struct {
		Kind string `json:"kind"`
	} `json:"data"`
}

func (e *apiError) Error() string {
	return fmt.Sprintf("hc %s: %s (%s)", e.Method, e.Message, e.Data.Kind)
}

// isNotFound reports whether err is hc's not_found: no terminal by that
// address or name.
func isNotFound(err error) bool {
	var e *apiError
	return errors.As(err, &e) && e.Data.Kind == "not_found"
}

// session is one connection to the core, past its hello. It carries any
// number of calls, one after another, plain or framed as its hello asked.
type session struct {
	conn   *net.UnixConn
	r      *bufio.Reader
	ctx    context.Context
	stop   func() bool
	next   int
	hello  helloResult
	pid    int // the core's, as the kernel reports the socket's peer
	framed bool
}

// dial connects to the core and says hello, framed or plain. A core that
// speaks another generation, or lacks a method or the feature this host
// needs, is refused. The end of ctx, by deadline or by cancel, ends every
// exchange on the session: a core that accepts and never answers must not
// hold the caller.
func (h *Host) dial(ctx context.Context, framed bool) (*session, error) {
	conn, err := connect(ctx, h.socket())
	if err != nil {
		return nil, err
	}
	uc := conn.(*net.UnixConn)
	s := &session{conn: uc, r: bufio.NewReader(uc), ctx: ctx}
	s.stop = context.AfterFunc(ctx, func() { _ = uc.SetDeadline(time.Now()) })
	if s.pid, err = proc.PeerPID(uc); err != nil {
		s.close()
		return nil, s.fail("hello", err)
	}
	params := map[string]any{"generation": generation, "framed": framed}
	if err := s.call("hello", params, &s.hello); err != nil {
		s.close()
		return nil, err
	}
	if err := s.hello.supported(); err != nil {
		s.close()
		return nil, err
	}
	s.framed = framed
	return s, nil
}

// connect connects to the socket. No socket file, or one that refuses, is
// errNoServer: a core that stops or restarts removes its socket first, and a
// file that refuses is one a killed core left behind.
func connect(ctx context.Context, socket string) (net.Conn, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", socket)
	switch {
	case err == nil:
		return conn, nil
	case errors.Is(err, os.ErrNotExist), errors.Is(err, syscall.ECONNREFUSED):
		return nil, errNoServer
	default:
		return nil, fmt.Errorf("hc: %w", err)
	}
}

func (s *session) close() {
	s.stop()
	_ = s.conn.Close()
}

// call sends one request and reads its answer, as callFramed on a framed
// session. On a plain one, the request is a line and its answer the next
// line. A hello that asks for framed mode is answered as a line too; the
// frames start after it.
func (s *session) call(method string, params, out any) error {
	if s.framed {
		return s.callFramed(method, params, out)
	}
	s.next++
	line, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": s.next, "method": method, "params": params})
	if err != nil {
		return s.fail(method, err)
	}
	if _, err := s.conn.Write(append(line, '\n')); err != nil {
		return s.fail(method, err)
	}
	reply, err := s.r.ReadBytes('\n')
	if err != nil {
		return s.fail(method, err)
	}
	return decode(method, reply, out)
}

// decode reads one JSON-RPC answer into out, or its error.
func decode(method string, reply []byte, out any) error {
	var resp struct {
		Result json.RawMessage `json:"result"`
		Error  *apiError       `json:"error"`
	}
	if err := json.Unmarshal(reply, &resp); err != nil {
		return fmt.Errorf("hc %s: %w", method, err)
	}
	if resp.Error != nil {
		resp.Error.Method = method
		return resp.Error
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(resp.Result, out); err != nil {
		return fmt.Errorf("hc %s: %w", method, err)
	}
	return nil
}

// fail words a broken exchange, as the end of ctx when that is what broke it.
func (s *session) fail(method string, err error) error {
	if s.ctx.Err() != nil {
		err = s.ctx.Err()
	}
	return fmt.Errorf("hc %s: %w", method, err)
}

// The frame kinds of framed mode this host sends or reads.
const (
	frameData      = 0
	frameCredit    = 1
	frameClose     = 2
	frameAutomatic = 6
)

// maxPayload is the most bytes one frame carries.
const maxPayload = 16 << 20

// writeFrame sends one frame: [channel u32 LE][kind u8][len u32 LE][payload].
func (s *session) writeFrame(channel uint32, kind byte, payload []byte) error {
	head := make([]byte, 9, 9+len(payload))
	binary.LittleEndian.PutUint32(head[0:], channel)
	head[4] = kind
	binary.LittleEndian.PutUint32(head[5:], uint32(len(payload)))
	_, err := s.conn.Write(append(head, payload...))
	return err
}

func (s *session) readFrame() (channel uint32, kind byte, payload []byte, err error) {
	head := make([]byte, 9)
	if _, err := io.ReadFull(s.r, head); err != nil {
		return 0, 0, nil, err
	}
	n := binary.LittleEndian.Uint32(head[5:])
	if n > maxPayload {
		return 0, 0, nil, fmt.Errorf("frame of %d bytes, over the %d hc sends", n, maxPayload)
	}
	payload = make([]byte, n)
	if _, err := io.ReadFull(s.r, payload); err != nil {
		return 0, 0, nil, err
	}
	return binary.LittleEndian.Uint32(head[0:]), head[4], payload, nil
}

// callFramed is call on a framed session: the request goes as DATA on
// channel 0, and the answer is the DATA on channel 0 that carries its id.
// Stream bytes that arrive meanwhile are taken and credited.
func (s *session) callFramed(method string, params, out any) error {
	s.next++
	id := s.next
	req, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	if err != nil {
		return s.fail(method, err)
	}
	if err := s.writeFrame(0, frameData, req); err != nil {
		return s.fail(method, err)
	}
	for {
		channel, kind, payload, err := s.readFrame()
		if err != nil {
			return s.fail(method, err)
		}
		if channel != 0 {
			if err := s.credit(channel, kind, payload); err != nil {
				return s.fail(method, err)
			}
			continue
		}
		var answer struct {
			ID int `json:"id"`
		}
		if kind == frameData && json.Unmarshal(payload, &answer) == nil && answer.ID == id {
			return decode(method, payload, out)
		}
	}
}

// credit gives the core back the room a stream's bytes took: a stream that
// runs out of credit stops, and its CLOSE with it.
func (s *session) credit(channel uint32, kind byte, payload []byte) error {
	if kind != frameData || len(payload) == 0 {
		return nil
	}
	return s.writeFrame(channel, frameCredit, binary.LittleEndian.AppendUint32(nil, uint32(len(payload))))
}

// stream is a channel's output, read on a goroutine of its own while the
// caller writes. The reader never writes: a core that stops reading until its
// output is read would hold a reader that waits on a write. The credit the
// output takes is owed instead, and the writer pays it.
type stream struct {
	channel uint32
	owed    atomic.Uint32
	more    chan struct{} // a send: there is credit owed
	closed  chan error    // the core closed the channel, or the read failed
}

// watch starts reading channel's output. Nothing else may read the session
// until the stream is closed.
func (s *session) watch(channel uint32) *stream {
	st := &stream{channel: channel, more: make(chan struct{}, 1), closed: make(chan error, 1)}
	go func() { st.closed <- st.read(s) }()
	return st
}

func (st *stream) read(s *session) error {
	for {
		ch, kind, payload, err := s.readFrame()
		if err != nil {
			return err
		}
		switch {
		case ch != st.channel:
		case kind == frameClose:
			return nil
		case kind == frameData && len(payload) > 0:
			st.owed.Add(uint32(len(payload)))
			select {
			case st.more <- struct{}{}:
			default:
			}
		}
	}
}

// pay gives the core back the credit the stream owes: a stream that runs out
// of credit stops, and its CLOSE with it.
func (s *session) pay(st *stream) error {
	if n := st.owed.Swap(0); n > 0 {
		return s.writeFrame(st.channel, frameCredit, binary.LittleEndian.AppendUint32(nil, n))
	}
	return nil
}

// errClosedEarly is the core's CLOSE of a stream before the viewer's own: the
// core may have left bytes sent before it untaken.
var errClosedEarly = errors.New("the core closed the attach before it took all the text")

// open fails when the core closed the stream, or its read failed, already.
func (st *stream) open() error {
	select {
	case err := <-st.closed:
		if err == nil {
			return errClosedEarly
		}
		return err
	default:
		return nil
	}
}

// untilClosed pays the stream's credit until the core closes it.
func (s *session) untilClosed(st *stream) error {
	for {
		select {
		case err := <-st.closed:
			return err
		case <-st.more:
			if err := s.pay(st); err != nil {
				return err
			}
		}
	}
}

// supported refuses a core this host cannot work with.
func (r helloResult) supported() error {
	if r.Generation != generation {
		return fmt.Errorf("hc %s speaks protocol generation %d; revier speaks %d", r.Version, r.Generation, generation)
	}
	for _, m := range neededMethods {
		if !slices.Contains(r.Methods, m) {
			return fmt.Errorf("hc %s has no %s method, which revier calls", r.Version, m)
		}
	}
	for _, f := range neededFeatures {
		if !slices.Contains(r.Features, f) {
			return fmt.Errorf("hc %s lacks the %s feature, which revier types with", r.Version, f)
		}
	}
	return nil
}

func (h *Host) socket() string {
	if h.Socket != "" {
		return h.Socket
	}
	return socketPath()
}
