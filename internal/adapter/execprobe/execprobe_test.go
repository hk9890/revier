package execprobe_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hk9890/revier/internal/adapter/execprobe"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/pkg/revier"
)

// script writes an executable probe into a scratch directory.
func script(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "probe")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestMatchIsTheHarnessCommand(t *testing.T) {
	p := execprobe.New("aider", "/nonexistent")
	if !p.Match(revier.Panel{Command: []string{"/usr/bin/aider", "--model", "x"}}) {
		t.Error("a panel running aider should match")
	}
	if p.Match(revier.Panel{Command: []string{"claude"}}) {
		t.Error("a claude panel must not cost the aider probe a process")
	}
}

// The wire form: the panel in, the state out, status as its name.
func TestInspectRoundTrip(t *testing.T) {
	path := script(t, `read -r panel; printf '{"harness":"aider","status":"running","activity":"got %s"}' "$(printf %s "$panel" | sed 's/.*"title":"\([^"]*\)".*/\1/')"`)
	p := execprobe.New("aider", path)
	got, err := p.Inspect(context.Background(), revier.Panel{ID: "3", Kind: revier.PanelAgent, Title: "refactoring", Command: []string{"aider"}})
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if got.Status != revier.StatusRunning || got.Harness != "aider" || got.Activity != "got refactoring" {
		t.Errorf("state = %+v", got)
	}
}

// Every failure mode is an error, never a state: the core maps it to unknown.
func TestInspectFailures(t *testing.T) {
	cases := map[string]string{
		"non-zero exit":  `exit 1`,
		"malformed json": `echo '{"status": nope'`,
		"unknown status": `echo '{"status":"tired"}'`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			p := execprobe.New("aider", script(t, body))
			if _, err := p.Inspect(context.Background(), revier.Panel{Command: []string{"aider"}}); err == nil {
				t.Fatal("want an error")
			}
		})
	}
}

// A hanging probe is cut off well under the refresh interval.
func TestInspectEnforcesTheTimeout(t *testing.T) {
	p := execprobe.New("aider", script(t, `sleep 5; echo '{"status":"idle"}'`)).WithTimeout(100 * time.Millisecond)
	start := time.Now()
	_, err := p.Inspect(context.Background(), revier.Panel{Command: []string{"aider"}})
	if err == nil {
		t.Fatal("want a timeout error")
	}
	if took := time.Since(start); took > time.Second {
		t.Errorf("Inspect took %s, want the 100ms bound to hold", took)
	}
}

// The whole path: a survey over a fake runtime with an aider pane reaches the
// script and shows its answer, and a broken probe leaves other panels intact.
func TestSurveyThroughAScriptProbe(t *testing.T) {
	good := execprobe.New("aider", script(t, `echo '{"status":"attention","activity":"waiting for you"}'`))
	broken := execprobe.New("goose", script(t, `exit 2`))
	rt := hosttest.NewRuntime("rt")
	rt.Add("session:p", "kitty",
		revier.Panel{ID: "1", Kind: revier.PanelAgent, Title: "x", Command: []string{"aider"}},
		revier.Panel{ID: "2", Kind: revier.PanelAgent, Title: "y", Command: []string{"goose"}},
	)
	c := &core.Core{Runtime: rt, Probes: []revier.AgentProbe{good, broken}}
	p, err := core.PrepareProject(revier.Project{Name: "p", Targets: []revier.Target{{Name: "home", Home: true,
		Runtime: &revier.Realization{Name: "session:p", Launch: []string{"x"}, Match: revier.Match{Title: "^session:p$"}}}}})
	if err != nil {
		t.Fatal(err)
	}
	views, err := c.Survey(context.Background(), []core.Project{p})
	if err != nil {
		t.Fatalf("Survey: %v", err)
	}
	agents := views[0].Agents
	if len(agents) != 2 {
		t.Fatalf("got %d agents, want 2", len(agents))
	}
	if a := agents[0]; a.State.Status != revier.StatusAttention || a.State.Harness != "aider" || a.State.Activity != "waiting for you" {
		t.Errorf("aider = %+v", a.State)
	}
	if b := agents[1]; b.State.Status != revier.StatusUnknown || b.State.Harness != "goose" {
		t.Errorf("goose = %+v, want unknown with the harness named", b.State)
	}
	if !views[0].Attention() {
		t.Error("the aider panel's attention should reach the project")
	}
}
