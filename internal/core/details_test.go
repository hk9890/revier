package core_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/pkg/revier"
)

// detailWorld is one project whose home holds n agent panels, with ids "1"
// to n, read by the given probe, surveyed once. It returns the core, its
// runtime, and the agents the survey found.
func detailWorld(t *testing.T, probe revier.AgentProbe, n int) (*core.Core, *hosttest.FakeRuntime, []revier.AgentView) {
	t.Helper()
	rt := hosttest.NewRuntime("rt")
	panels := make([]revier.Panel, n)
	for i := range panels {
		panels[i] = revier.Panel{ID: revier.PanelID(fmt.Sprint(i + 1)), Kind: revier.PanelTool, Title: fmt.Sprintf("claude %d", i+1)}
	}
	rt.Add("session:demo", "kitty", panels...)
	projects := core.Prepare([]revier.Project{{Name: "demo", Path: "/p/demo", Targets: []revier.Target{
		{Name: "home", Home: true, Runtime: &revier.Realization{
			Name: "session:demo", Launch: []string{"x"}, Match: revier.Match{Title: "^session:demo$"}}},
	}}})
	c := &core.Core{Runtime: rt, View: rt, Probes: []revier.AgentProbe{probe}}
	r, err := c.Survey(t.Context(), projects)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Views) != 1 || len(r.Views[0].Agents) != n {
		t.Fatalf("survey found %+v, want one project with %d agents", r.Views, n)
	}
	return c, rt, r.Views[0].Agents
}

// Details is what each agent said, in the views' order, read from the panels
// the survey listed: a detail asks no host anything.
func TestDetailsAreWhatEachAgentSaidAndAskNoHost(t *testing.T) {
	probe := hosttest.NewDetailedProbe("claude", "claude")
	at := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	probe.Said["1"] = revier.AgentDetail{Message: "one said this", At: at}
	probe.Said["2"] = revier.AgentDetail{Message: "two said that"}
	c, rt, agents := detailWorld(t, probe, 2)

	listed := rt.InstancesCalls
	got, _ := c.Details(context.Background(), agents)
	if len(got) != 2 || got[0].Message != "one said this" || !got[0].At.Equal(at) || got[1].Message != "two said that" {
		t.Errorf("Details = %+v, want each agent's own, in order", got)
	}
	if rt.InstancesCalls != listed {
		t.Errorf("Details listed the host %d times, want none", rt.InstancesCalls-listed)
	}
	if probe.DetailCalls() != 2 {
		t.Errorf("the probe was asked %d times, want once per agent", probe.DetailCalls())
	}
}

// A detail is display: a probe that cannot say, and one that fails, leave it
// empty, and neither is an error to the caller.
func TestDetailsAreEmptyWhenTheProbeCannotSay(t *testing.T) {
	failing := hosttest.NewDetailedProbe("claude", "claude")
	failing.DetailErr = errors.New("transcript format changed")
	for name, probe := range map[string]revier.AgentProbe{
		"a probe with no Detail": &hosttest.FakeProbe{Harness: "claude", Marker: "claude"},
		"a probe that fails":     failing,
	} {
		t.Run(name, func(t *testing.T) {
			c, _, agents := detailWorld(t, probe, 2)
			details, _ := c.Details(context.Background(), agents)
			for i, d := range details {
				if !d.IsZero() {
					t.Errorf("detail %d = %+v, want it empty", i, d)
				}
			}
		})
	}
}

// listingProbe reads each detail through one shared source. With slow set, the
// source outlasts the read's ctx: the first read is killed at the deadline, and
// every later one fails at once, each with an error that wraps the ctx's. With
// late set, the first read waits for the deadline and then answers. The panel
// named broken fails for a cause of its own.
type listingProbe struct {
	*hosttest.FakeDetailedProbe
	slow, late bool
	broken     revier.PanelID
	asked      int
}

func (p *listingProbe) Detail(ctx context.Context, panel revier.Panel) (revier.AgentDetail, error) {
	p.asked++
	if (p.slow || p.late) && p.asked == 1 {
		<-ctx.Done()
		if p.slow {
			return revier.AgentDetail{}, fmt.Errorf("%w: claude agents --json: signal: killed", ctx.Err())
		}
	}
	switch {
	case p.slow:
		return revier.AgentDetail{}, fmt.Errorf("claude agents --json: %w", ctx.Err())
	case panel.ID == p.broken:
		return revier.AgentDetail{}, errors.New("the transcript of agent 2 broke")
	}
	return p.Said[panel.ID], nil
}

// saidBy gives each of the n panels of a detailWorld a message of its own.
func saidBy(p *listingProbe, n int) {
	for i := 1; i <= n; i++ {
		p.Said[revier.PanelID(fmt.Sprint(i))] = revier.AgentDetail{Message: fmt.Sprintf("agent %d said this", i)}
	}
}

func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var logged bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logged, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return &logged
}

// A read that fails with the ctx's end cuts its probe: the probe is not asked
// again, its agents stay empty, and the cut is one WARN for the probe, not one
// per agent - four agents here, as one tab of a burst that wrote one WARN per
// agent held. The next walk that reads the probe in time writes one recovered
// line for it.
func TestDetailsCutByTheirDeadlineLogItOncePerProbe(t *testing.T) {
	logged := captureLog(t)
	probe := &listingProbe{FakeDetailedProbe: hosttest.NewDetailedProbe("claude", "claude"), slow: true}
	saidBy(probe, 4)
	c, _, agents := detailWorld(t, probe, 4)

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	got, _ := c.Details(ctx, agents)
	if len(got) != len(agents) {
		t.Fatalf("Details = %+v, want one detail per agent", got)
	}
	for i, d := range got {
		if !d.IsZero() {
			t.Errorf("detail %d = %+v, want it empty", i, d)
		}
	}
	if probe.asked != 1 {
		t.Errorf("the probe was asked %d times, want once: the cut read stops it", probe.asked)
	}
	if n := strings.Count(logged.String(), `"msg":"detail"`); n != 1 ||
		!strings.Contains(logged.String(), `"level":"WARN","msg":"detail","probe":"claude","read":0,"failed":0,"unread":4,"cut":"context deadline exceeded: claude agents --json: signal: killed"`) {
		t.Errorf("logged %d detail lines, want one WARN for the probe with its counts and the cut read:\n%s", n, logged.String())
	}

	probe.slow, probe.asked = false, 0
	got, _ = c.Details(t.Context(), agents)
	if len(got) != len(agents) {
		t.Fatalf("Details after the cut = %+v, want one detail per agent", got)
	}
	for i, d := range got {
		if want := fmt.Sprintf("agent %s said this", agents[i].Panel); d.Message != want {
			t.Errorf("detail %d after the cut = %+v, want %q", i, d, want)
		}
	}
	if n := strings.Count(logged.String(), `"msg":"detail: recovered","probe":"claude","read":4,"failed":0,"unread":0`); n != 1 {
		t.Errorf("a walk that read the probe in time wrote %d recovered lines for it, want one:\n%s", n, logged.String())
	}
}

// A read that answers after the ctx ended has not failed, and one that fails
// for a cause of its own is logged as its own: neither cuts the probe, and
// the reads after them, which need no ctx, still answer.
func TestDetailsReadOnPastTheDeadlineWhileReadsAnswer(t *testing.T) {
	logged := captureLog(t)
	probe := &listingProbe{FakeDetailedProbe: hosttest.NewDetailedProbe("claude", "claude"), late: true, broken: "2"}
	saidBy(probe, 4)
	c, _, agents := detailWorld(t, probe, 4)

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	got, _ := c.Details(ctx, agents)
	if len(got) != len(agents) {
		t.Fatalf("Details = %+v, want one detail per agent", got)
	}
	for i, d := range got {
		want := fmt.Sprintf("agent %s said this", agents[i].Panel)
		if agents[i].Panel == "2" {
			want = ""
		}
		if d.Message != want {
			t.Errorf("detail %d = %+v, want %q", i, d, want)
		}
	}
	if n := strings.Count(logged.String(), `"level":"WARN","msg":"detail"`); n != 1 ||
		!strings.Contains(logged.String(), `"panel":"2","err":"the transcript of agent 2 broke"`) {
		t.Errorf("logged %d detail WARNs, want one for the broken agent:\n%s", n, logged.String())
	}

	// The broken read recovers, so the next run of this test logs it again.
	probe.broken = ""
	c.Details(t.Context(), agents)
}

// A probe whose listing is not there yet leaves its agents unread: the walk
// says it is not complete, logs no failure, and writes no "recovered" line.
func TestDetailsOfAProbeWithNoListingYetAreNotComplete(t *testing.T) {
	logged := captureLog(t)
	probe := hosttest.NewDetailedProbe("claude", "claude")
	probe.Said["1"] = revier.AgentDetail{Message: "said"}
	c, _, agents := detailWorld(t, probe, 1)

	probe.DetailErr = fmt.Errorf("%w: no listing yet", revier.ErrDetailPending)
	got, complete := c.Details(t.Context(), agents)
	if complete || !got[0].IsZero() {
		t.Errorf("Details = %+v, complete = %v; want it empty and not complete", got, complete)
	}
	if strings.Contains(logged.String(), `"msg":"detail`) {
		t.Errorf("a pending read logged:\n%s", logged.String())
	}

	probe.DetailErr = nil
	if got, complete := c.Details(t.Context(), agents); !complete || got[0].Message != "said" {
		t.Errorf("Details = %+v, complete = %v; want the message and complete", got, complete)
	}
	if strings.Contains(logged.String(), `"msg":"detail`) {
		t.Errorf("a walk after a pending one logged:\n%s", logged.String())
	}
}

// A read cut by its deadline leaves the walk not complete.
func TestDetailsCutByTheirDeadlineAreNotComplete(t *testing.T) {
	captureLog(t)
	probe := &listingProbe{FakeDetailedProbe: hosttest.NewDetailedProbe("claude", "claude"), slow: true}
	c, _, agents := detailWorld(t, probe, 2)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if _, complete := c.Details(ctx, agents); complete {
		t.Error("a cut walk says it is complete")
	}
	probe.slow, probe.asked = false, 0
	c.Details(t.Context(), agents)
}

// Before any survey there is no listing to find a panel in, and an agent is
// given no detail rather than a host call.
func TestDetailsBeforeAnySurveyAreEmpty(t *testing.T) {
	probe := hosttest.NewDetailedProbe("claude", "claude")
	probe.Said["1"] = revier.AgentDetail{Message: "said"}
	rt := hosttest.NewRuntime("rt")
	c := &core.Core{Runtime: rt, View: rt, Probes: []revier.AgentProbe{probe}}
	got, _ := c.Details(context.Background(), []revier.AgentView{{Panel: "1", Ref: revier.TargetRef{Host: "rt", ID: "1"}}})
	if len(got) != 1 || !got[0].IsZero() {
		t.Errorf("Details = %+v, want one empty detail", got)
	}
	if rt.InstancesCalls != 0 || probe.DetailCalls() != 0 {
		t.Errorf("listed %d times and asked the probe %d times, want neither", rt.InstancesCalls, probe.DetailCalls())
	}
}
