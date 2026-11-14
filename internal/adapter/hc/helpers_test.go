package hc_test

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"io"
	"maps"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/hk9890/revier/internal/testenv"
	"github.com/hk9890/revier/pkg/revier"
)

// fakeCore answers hc's JSON-RPC on a socket of its own, plain and framed,
// with terminals it keeps in memory, and records what it was asked.
type fakeCore struct {
	mu        sync.Mutex
	id        string
	features  []string
	terminals []*fakeTerminal
	runs      int
	calls     []string         // every method, in order
	params    []map[string]any // the params of each call, in order

	// failRun answers the run with this number, from 1, with runErr.
	failRun int
	runErr  map[string]any
	// exitRun ends the program of the run with this number, from 1, at
	// once: hc removes it, or leaves it finished when the run did not ask
	// for rm.
	exitRun int
	// panelOf gives the terminal of the run with this number, from 1, this
	// revier.panel label in place of the one the run asked for.
	panelOf map[int]string

	read    string         // what terminal.read answers
	readErr map[string]any // what terminal.read fails with, in its place

	input  []byte // the automatic input an attach carried
	closed bool   // the attach was closed by its viewer
	kinds  []byte // the kind of each frame a viewer sent on the attach

	// flood is how many bytes of output an attach streams after its answer,
	// written whole before the core reads the viewer again, and with no
	// regard for credit.
	flood int
	// closeEarly closes the attach after the first automatic input it takes,
	// before the viewer's own CLOSE, and reads nothing more for a moment.
	closeEarly bool
	// killDelay is how long each terminal.kill takes, as hc's does for a
	// program that saves on neither HUP nor TERM.
	killDelay time.Duration
	// idAfterList is the id the core answers with after its first
	// terminal.list, as a core started on the socket in between does.
	idAfterList string
}

type fakeTerminal struct {
	Address    string            `json:"address"`
	Name       string            `json:"name,omitempty"`
	State      string            `json:"state"`
	PID        int               `json:"pid,omitempty"`
	Argv       []string          `json:"argv"`
	Cwd        string            `json:"cwd"`
	Title      string            `json:"title"`
	Labels     map[string]string `json:"labels"`
	Foreground map[string]any    `json:"foreground,omitempty"`

	// rm removes the terminal when it exits, as hc's terminal.run rm with no
	// parent does: a kill answers not_found, as hc's does of the record it
	// removed meanwhile.
	rm bool
}

// attachChannel is the channel the fake core gives an attach.
const attachChannel = 7

// newCore is a core with id "core1" that offers every method and feature the
// host needs, served on a socket that the returned path names.
func newCore(t *testing.T) (*fakeCore, string) {
	t.Helper()
	sock := socketFile(t)
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	c := newFakeCore()
	c.serveOn(t, l)
	return c, sock
}

// newFakeCore is a core with id "core1" that offers every method and feature
// the host needs, served nowhere yet.
func newFakeCore() *fakeCore {
	return &fakeCore{id: "core1", features: []string{"framed", "attach", "automatic_input"}}
}

// socketFile is a path for a socket in a directory of the test's own. Not
// t.TempDir: a test's long name would take the path past the 107 bytes a
// Unix socket allows.
func socketFile(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "rvhc")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Join(dir, "s.sock")
}

// serveOn answers every connection l accepts, until the test ends.
func (c *fakeCore) serveOn(t *testing.T, l net.Listener) {
	t.Cleanup(func() { _ = l.Close() })
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			go c.serve(conn)
		}
	}()
}

// add puts a terminal into the core, as one started by hand or by revier.
func (c *fakeCore) add(t *fakeTerminal) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if t.Labels == nil {
		t.Labels = map[string]string{}
	}
	c.terminals = append(c.terminals, t)
}

func (c *fakeCore) called() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.calls...)
}

// paramsOf is the params of every call of the method, in order.
func (c *fakeCore) paramsOf(method string) []map[string]any {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []map[string]any
	for i, m := range c.calls {
		if m == method {
			out = append(out, c.params[i])
		}
	}
	return out
}

func (c *fakeCore) serve(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	r := bufio.NewReader(conn)
	for {
		line, err := r.ReadBytes('\n')
		if err != nil {
			return
		}
		var req request
		_ = json.Unmarshal(line, &req)
		_, _ = conn.Write(append(c.answer(req), '\n'))
		if req.Method == "hello" && req.Params["framed"] == true {
			c.serveFrames(conn, r)
			return
		}
	}
}

// serveFrames is the connection after a framed hello: calls on channel 0,
// and one attach's stream on attachChannel.
func (c *fakeCore) serveFrames(conn net.Conn, r *bufio.Reader) {
	for {
		head := make([]byte, 9)
		if _, err := io.ReadFull(r, head); err != nil {
			return
		}
		channel, kind := binary.LittleEndian.Uint32(head[0:]), head[4]
		payload := make([]byte, binary.LittleEndian.Uint32(head[5:]))
		if _, err := io.ReadFull(r, payload); err != nil {
			return
		}
		if channel == 0 {
			var req request
			_ = json.Unmarshal(payload, &req)
			_, _ = conn.Write(frame(0, 0, c.answer(req)))
			if req.Method == "terminal.attach" {
				_, _ = conn.Write(frame(attachChannel, 0, []byte("first paint")))
				c.mu.Lock()
				flood := c.flood
				c.mu.Unlock()
				for chunk := make([]byte, 64<<10); flood > 0; flood -= len(chunk) {
					if _, err := conn.Write(frame(attachChannel, 0, chunk)); err != nil {
						return
					}
				}
			}
			continue
		}
		c.mu.Lock()
		c.kinds = append(c.kinds, kind)
		closeNow := false
		switch kind {
		case 6:
			closeNow = c.closeEarly && len(c.input) == 0
			c.input = append(c.input, payload...)
		case 2:
			c.closed = true
		}
		c.mu.Unlock()
		if kind == 2 || closeNow {
			_, _ = conn.Write(frame(attachChannel, 2, nil))
		}
		if closeNow {
			time.Sleep(100 * time.Millisecond)
		}
	}
}

func frame(channel uint32, kind byte, payload []byte) []byte {
	out := binary.LittleEndian.AppendUint32(nil, channel)
	out = append(out, kind)
	out = binary.LittleEndian.AppendUint32(out, uint32(len(payload)))
	return append(out, payload...)
}

type request struct {
	ID     int            `json:"id"`
	Method string         `json:"method"`
	Params map[string]any `json:"params"`
}

// answer is the JSON-RPC answer to one request.
func (c *fakeCore) answer(req request) []byte {
	if req.Method == "terminal.kill" {
		c.mu.Lock()
		delay := c.killDelay
		c.mu.Unlock()
		time.Sleep(delay)
	}
	result, fail := c.dispatch(req)
	msg := map[string]any{"jsonrpc": "2.0", "id": req.ID}
	if fail != nil {
		msg["error"] = fail
	} else {
		msg["result"] = result
	}
	out, _ := json.Marshal(msg)
	return out
}

func (c *fakeCore) dispatch(req request) (any, map[string]any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, req.Method)
	c.params = append(c.params, req.Params)
	switch req.Method {
	case "hello":
		return map[string]any{
			"generation": 1, "version": "0.1.21", "core": map[string]string{"id": c.id, "name": "here"},
			"framed": req.Params["framed"] == true, "features": c.features,
			"methods": []string{"hello", "terminal.run", "terminal.list", "terminal.get", "terminal.kill", "terminal.rm", "terminal.read", "terminal.attach"},
		}, nil
	case "terminal.run":
		c.runs++
		if c.runs == c.failRun {
			return nil, c.runErr
		}
		t := &fakeTerminal{Address: "t" + strconv.Itoa(c.runs), State: "live", PID: 100 + c.runs, Labels: map[string]string{}}
		t.rm = req.Params["rm"] == true
		t.Name, _ = req.Params["name"].(string)
		t.Cwd, _ = req.Params["cwd"].(string)
		for _, a := range asList(req.Params["argv"]) {
			t.Argv = append(t.Argv, a.(string))
		}
		for k, v := range asMap(req.Params["labels"]) {
			t.Labels[k] = v.(string)
		}
		if panel, ok := c.panelOf[c.runs]; ok {
			t.Labels["revier.panel"] = panel
		}
		if c.runs == c.exitRun {
			t.State = "finished"
			if t.rm {
				return t, nil
			}
		}
		c.terminals = append(c.terminals, t)
		return t, nil
	case "terminal.list":
		all := req.Params["all"] == true
		filter := asMap(req.Params["labels"])
		out := []*fakeTerminal{}
	terminals:
		for _, t := range c.terminals {
			if !all && t.State != "live" {
				continue
			}
			for k, v := range filter {
				if t.Labels[k] != v {
					continue terminals
				}
			}
			out = append(out, t)
		}
		if c.idAfterList != "" {
			c.id = c.idAfterList
		}
		return map[string]any{"terminals": out, "hidden_finished": 0, "machines": []any{}}, nil
	case "terminal.get":
		t := c.find(req.Params["terminal"])
		if t == nil {
			return nil, notFound
		}
		return t, nil
	case "terminal.kill":
		t := c.find(req.Params["terminal"])
		if t == nil {
			return nil, notFound
		}
		t.State = "finished"
		if t.rm {
			c.remove(t)
			return nil, notFound
		}
		return t, nil
	case "terminal.rm":
		t := c.find(req.Params["terminal"])
		if t == nil {
			return nil, notFound
		}
		c.remove(t)
		return map[string]string{"removed": t.Address}, nil
	case "terminal.read":
		if c.readErr != nil {
			return nil, c.readErr
		}
		return map[string]any{"text": c.read, "complete": true, "how": "screen"}, nil
	case "terminal.attach":
		return map[string]any{"channel": attachChannel, "automatic_input": true, "resize_notifications": false, "terminal": map[string]any{}}, nil
	}
	return nil, map[string]any{"code": -32601, "message": "no method " + req.Method}
}

var notFound = map[string]any{"code": -32000, "message": "no terminal", "data": map[string]string{"kind": "not_found"}}

// remove forgets the terminal; c.mu is held.
func (c *fakeCore) remove(t *fakeTerminal) {
	c.terminals = slices.DeleteFunc(c.terminals, func(x *fakeTerminal) bool { return x == t })
}

func (c *fakeCore) find(address any) *fakeTerminal {
	for _, t := range c.terminals {
		if t.Address == address || t.Name == address {
			return t
		}
	}
	return nil
}

// records is the addresses of every terminal the core holds, live or
// finished.
func (c *fakeCore) records() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []string
	for _, t := range c.terminals {
		out = append(out, t.Address)
	}
	return out
}

// live is the addresses of the core's live terminals.
func (c *fakeCore) live() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []string
	for _, t := range c.terminals {
		if t.State == "live" {
			out = append(out, t.Address)
		}
	}
	return out
}

// fakeBin is a directory that holds an executable called name, which leaves
// a file called ran beside it and fails if run: for a test that looks the
// binary up and must never start it.
func fakeBin(t *testing.T, name string) string {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\n: > \"$(dirname \"$0\")/ran\"\nexit 1\n"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// hcLink is fakeBin of herdr-core in one directory and, in another, an hc
// that links to it, as hc's install makes. It returns the directory of the
// link.
func hcLink(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Symlink(filepath.Join(fakeBin(t, "herdr-core"), "herdr-core"), filepath.Join(dir, "hc")); err != nil {
		t.Fatal(err)
	}
	return dir
}

// ran reports whether the fakeBin in dir was run.
func ran(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, "ran"))
	return err == nil
}

func TestMain(m *testing.M) { os.Exit(testenv.Run(m)) }

func asList(v any) []any {
	l, _ := v.([]any)
	return l
}

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return maps.Clone(m)
}

// matched reports whether the realization's match finds the instance ref
// names among instances.
func matched(t *testing.T, m revier.Match, instances []revier.Instance, ref revier.TargetRef) bool {
	t.Helper()
	compiled, err := m.Compile()
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	return slices.ContainsFunc(instances, func(in revier.Instance) bool { return in.Ref == ref && compiled.Matches(in) })
}
