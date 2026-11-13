//go:build integration

// Layer L3: the hello and terminal.list readers, against answers recorded
// from a private hc 0.1.21 core and served on a socket of the test's own. No
// hc, no display. The recording holds an instance revier opened, two
// terminals whose name carries a pipe, a newline and an '=', one running a
// program and one the core's shell; a terminal made by hand; and a finished
// one, which hc leaves out of the list.
package hc_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/hk9890/revier/internal/adapter/hc"
	"github.com/hk9890/revier/pkg/revier"
)

func TestInstancesReadARecordedList(t *testing.T) {
	sock, asked := serveRecorded(t, map[string]string{
		"hello":         "testdata/hc-0.1.21/hello.json",
		"terminal.list": "testdata/hc-0.1.21/list.json",
	})

	got, err := (&hc.Host{Socket: sock}).Instances(t.Context())
	if err != nil {
		t.Fatalf("Instances: %v", err)
	}
	if calls := asked(); !slices.Equal(calls, []string{"hello", "terminal.list"}) {
		t.Errorf("hc asked %q, want one hello and one terminal.list", calls)
	}
	if len(got) != 2 {
		t.Fatalf("got %d instances, want revier's and the one by hand: %+v", len(got), got)
	}
	const core = "750423e70037fecb714452418eb1d970"
	named, byHand := got[0], got[1]
	if want := (revier.TargetRef{Host: "hc", ID: core + "/0a1b2c3d4e5f6a7b", Title: "demo | pipe\nline=ü"}); named.Ref != want {
		t.Errorf("named = %+v, want %+v", named.Ref, want)
	}
	if byHand.Ref.ID != core+"/t3" || byHand.Title != "scratch" {
		t.Errorf("by hand = %+v, want t3 titled by its name", byHand.Ref)
	}
	if len(named.Panels) != 2 {
		t.Fatalf("panels = %+v, want two", named.Panels)
	}
	agent, shell := named.Panels[0], named.Panels[1]
	if agent.ID != "t1" || agent.Kind != revier.PanelTool || agent.PID != 867963 || !slices.Equal(agent.Command, []string{"sleep", "300"}) ||
		agent.Dir != "/tmp/hcs.rvrec/state" || agent.Vars["revier_target"] != "home" || len(agent.Vars) != 1 {
		t.Errorf("agent = %+v, want t1 running sleep 300 as 867963, with its var", agent)
	}
	if shell.ID != "t2" || shell.Kind != revier.PanelShell || shell.PID != 867967 {
		t.Errorf("shell = %+v, want t2, the core's shell, as 867967", shell)
	}
}

// serveRecorded answers each request line with the recorded answer to its
// method, given its id, on one connection. asked waits for the connection to
// end and returns the methods it was asked.
func serveRecorded(t *testing.T, answers map[string]string) (sock string, asked func() []string) {
	t.Helper()
	recorded := map[string]map[string]any{}
	for method, file := range answers {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read fixture: %v", err)
		}
		var msg map[string]any
		if err := json.Unmarshal(raw, &msg); err != nil {
			t.Fatalf("parse fixture %s: %v", file, err)
		}
		recorded[method] = msg
	}
	dir, err := os.MkdirTemp("", "rvhc")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sock = filepath.Join(dir, "s.sock")
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })
	var methods []string
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := l.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		r := bufio.NewReader(conn)
		for {
			line, err := r.ReadBytes('\n')
			if err != nil {
				return
			}
			var req request
			_ = json.Unmarshal(line, &req)
			methods = append(methods, req.Method)
			msg := recorded[req.Method]
			msg["id"] = req.ID
			var out bytes.Buffer
			_ = json.NewEncoder(&out).Encode(msg)
			_, _ = conn.Write(out.Bytes())
		}
	}()
	return sock, func() []string {
		<-done
		return methods
	}
}
