// Layer L2: a host that cannot list costs its own targets and no other's
// (decisions.md D89), which is a decision the core makes from what each host
// reports, so the fake host reports the failure.
package core_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/pkg/revier"
)

// degraded is a runtime holding home, a window host holding the editor, and
// a served host, with the named one failing to list.
func degraded(t *testing.T, failing string) (*core.Core, []core.Project) {
	t.Helper()
	rt := hosttest.NewRuntime("rt")
	rt.Add("session:revier", "kitty")
	wm := hosttest.New("wm")
	wm.Add("Visual Studio Code", "code")
	served := hosttest.New("served")
	fail := errors.New("went away")
	for _, h := range []*hosttest.Fake{rt.Fake, wm, served} {
		if h.Name() == failing {
			h.InstancesErr = fail
		}
	}
	return &core.Core{Runtime: rt, Window: wm, Served: served}, []core.Project{prepared(t, project())}
}

// A survey over a host that cannot list still stands: the other host's
// targets are as they are, the failed host's are unknown with the reason,
// and the failed host is not among the hosts that answered, so nothing bound
// to it is taken as gone.
func TestASurveyDegradesToTheHostsThatAnswered(t *testing.T) {
	for _, tc := range []struct {
		failing          string
		unknown, running string
		hosts            []string
	}{
		{"rt", "home", "editor", []string{"wm"}},
		{"wm", "editor", "home", []string{"rt"}},
		{"served", "", "home", []string{"wm", "rt"}},
	} {
		t.Run(tc.failing, func(t *testing.T) {
			c, projects := degraded(t, tc.failing)
			report, err := c.Survey(context.Background(), projects, nil, nil)
			if err != nil {
				t.Fatalf("Survey: %v, want the view over the hosts that answered", err)
			}
			if !slices.Equal(report.Hosts, tc.hosts) {
				t.Errorf("hosts = %v, want %v: the failed host did not answer", report.Hosts, tc.hosts)
			}
			if err := report.HostErr(); err == nil || !strings.Contains(err.Error(), tc.failing+": instances: went away") {
				t.Errorf("HostErr = %v, want the failed host named", err)
			}
			for _, tv := range report.Views[0].Targets {
				switch tv.Name {
				case revier.TargetName(tc.unknown):
					if tv.Unknown == "" || !tv.Available || !tv.Ref.IsZero() {
						t.Errorf("%s = %+v, want unknown with the reason", tv.Name, tv)
					}
				case revier.TargetName(tc.running):
					if tv.Unknown != "" || tv.Ref.IsZero() {
						t.Errorf("%s = %+v, want running as before", tv.Name, tv)
					}
				}
			}
		})
	}
}

// A press on a target of the host that cannot list is refused with the
// reason: a launch over a listing it is missing from would open a second
// copy. A target of the other host still goes.
func TestGoRefusesATargetOfAHostThatCannotList(t *testing.T) {
	c, projects := degraded(t, "rt")
	if _, err := c.Go(context.Background(), projects[0], "home", nil); err == nil || !strings.Contains(err.Error(), "rt: instances: went away") {
		t.Errorf("Go(home) = %v, want the runtime's failure", err)
	}
	if _, err := c.Go(context.Background(), projects[0], "editor", nil); err != nil {
		t.Errorf("Go(editor) = %v, want the window host's target raised", err)
	}
	if _, err := c.Running(context.Background(), projects[0], "home", nil); err == nil {
		t.Error("Running(home) = nil, want the runtime's failure")
	}
}
