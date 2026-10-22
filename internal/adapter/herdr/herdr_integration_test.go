//go:build integration

// Layer L3: the session.snapshot parser, against output recorded from a
// private herdr 0.9.5 server and served on a socket of the test's own. No
// herdr, no display. The fixture holds a workspace revier named, with a label
// that carries a pipe and a newline, a workspace made by hand, and a tab whose
// panes carry revier's tokens.
package herdr_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
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
	sock := serve(t, func(map[string]any) string { return line.String() })
	h := &herdr.Host{Socket: sock}
	h.SetProcRoot(t.TempDir())

	got, err := h.Instances(context.Background())
	if err != nil {
		t.Fatalf("Instances: %v", err)
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
			agent = append(agent, string(p.ID)+" "+p.Tab+" "+p.Vars["revier_home"])
		}
	}
	if len(named.Panels) != 3 || len(agent) != 1 || agent[0] != "w1:p2 w1:t2 home" {
		t.Errorf("panels = %+v, want three, one of them w1:p2 in w1:t2 with its tokens", named.Panels)
	}

	focused, err := h.Focused(context.Background())
	if err != nil || focused != named.Ref {
		t.Errorf("Focused = %+v, %v; want %+v", focused, err, named.Ref)
	}
}
