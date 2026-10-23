//go:build integration

// Layer L3: the session.snapshot parser, against output recorded from a
// private herdr 0.9.5 server and served on a socket of the test's own. No
// herdr, no display. The fixture holds a workspace revier named, with a label
// that carries a pipe and a newline, a workspace made by hand, and a tab whose
// panes carry revier's tokens. Each pane's process is in a fake /proc, as
// herdr started it, so the listing is the one request it costs.
package herdr_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync/atomic"
	"testing"

	"github.com/hk9890/revier/internal/adapter/herdr"
)

func TestInstancesReadARecordedSnapshot(t *testing.T) {
	raw, err := os.ReadFile("testdata/herdr-0.9.5/snapshot.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var line bytes.Buffer
	if err := json.Compact(&line, raw); err != nil {
		t.Fatalf("compact fixture: %v", err)
	}
	var requests atomic.Int32
	sock := serve(t, func(map[string]any) string {
		requests.Add(1)
		return line.String()
	})
	h := &herdr.Host{Socket: sock}
	root := t.TempDir()
	// The server is the test's own process, the socket's peer.
	for pid, pane := range map[int]string{101: "w1:p1", 102: "w1:p2", 103: "w1:p3", 104: "w2:p1"} {
		writeProc(t, root, pid, os.Getpid(), "HERDR_PANE_ID="+pane, "HERDR_SOCKET_PATH="+sock)
	}
	h.SetProcRoot(root)

	got, err := h.Instances(context.Background())
	if err != nil {
		t.Fatalf("Instances: %v", err)
	}
	if n := requests.Load(); n != 1 {
		t.Errorf("herdr asked %d times, want 1", n)
	}
	if len(got) != 2 {
		t.Fatalf("got %d instances, want 2: %+v", len(got), got)
	}
	named, byHand := got[0], got[1]
	if want := fmt.Sprintf("%d/w1", os.Getpid()); named.Ref.ID != want || named.Title != "demo|pipe" {
		t.Errorf("named = %+v, want id %s titled by its token", named.Ref, want)
	}
	if byHand.Title != "by hand | with\ttab" {
		t.Errorf("by hand = %q, want its label as written", byHand.Title)
	}

	var agent []string
	for _, p := range named.Panels {
		if p.Vars["revier_target"] == "agent" {
			agent = append(agent, fmt.Sprintf("%s %s %s %d", p.ID, p.Tab, p.Vars["revier_home"], p.PID))
		}
	}
	if len(named.Panels) != 3 || len(agent) != 1 || agent[0] != "w1:p2 w1:t2 home 102" {
		t.Errorf("panels = %+v, want three, one of them w1:p2 in w1:t2 with its tokens, running 102", named.Panels)
	}

	focused, err := h.Focused(context.Background())
	if err != nil || focused != named.Ref {
		t.Errorf("Focused = %+v, %v; want %+v", focused, err, named.Ref)
	}
}
