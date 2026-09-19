// Layer L1: what opening a project comes to is decided from the project and
// whether its home runs, with no host asked.
package core_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/pkg/revier"
)

func TestOpenDecidesFromTheProjectAndWhetherItRuns(t *testing.T) {
	here := t.TempDir()
	home := revier.Target{Name: "home", Home: true, Runtime: &revier.Realization{Name: "s", Launch: []string{"x"}, Match: revier.Match{Title: "^s$"}}}
	never := func() (bool, error) { t.Error("running asked where the answer is known without it"); return false, nil }
	answer := func(up bool, err error) func() (bool, error) { return func() (bool, error) { return up, err } }
	for name, tc := range map[string]struct {
		project revier.Project
		running func() (bool, error)
		want    core.Opening
		refused string
	}{
		"a checkout here":                   {revier.Project{Name: "p", Path: here, Targets: []revier.Target{home}}, never, core.OpenGo, ""},
		"a link":                            {revier.Project{Name: "p", Path: "/on/the/host", Remote: &revier.Link{Host: "box"}, Targets: []revier.Target{home}}, never, core.OpenGo, ""},
		"a missing checkout with a git_url": {revier.Project{Name: "p", Path: "/gone", GitURL: "git@x:p.git", Targets: []revier.Target{home}}, never, core.OpenClone, ""},
		"a missing checkout that runs":      {revier.Project{Name: "p", Path: "/gone", Targets: []revier.Target{home}}, answer(true, nil), core.OpenGo, ""},
		"a missing checkout, stopped":       {revier.Project{Name: "p", Path: "/gone", Targets: []revier.Target{home}}, answer(false, nil), 0, "git_url"},
		// Whether it runs is not known: the refusal is the host's reason, not
		// a git_url the user would go and add for nothing.
		"a missing checkout, host not listing": {revier.Project{Name: "p", Path: "/gone", Targets: []revier.Target{home}}, answer(false, errors.New("rt: instances: went away")), 0, "went away"},
		"no home target":                       {revier.Project{Name: "p", Path: here, Targets: []revier.Target{{Name: "editor", Window: &revier.Realization{Launch: []string{"code"}, Match: revier.Match{Class: "^code$"}}}}}, never, 0, "no home target"},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := core.Open(core.PrepareProject(tc.project), tc.running)
			if tc.refused != "" {
				if err == nil || !strings.Contains(err.Error(), tc.refused) {
					t.Fatalf("Open = %v, %v; want a refusal saying %q", got, err, tc.refused)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Errorf("Open = %v, %v; want %v", got, err, tc.want)
			}
		})
	}
	p := core.PrepareProject(revier.Project{Name: "p", Path: here, Targets: []revier.Target{home}})
	p.Invalid = errors.New("its file refused it")
	if _, err := core.Open(p, never); !errors.Is(err, p.Invalid) {
		t.Errorf("Open of an invalid project = %v, want its reason", err)
	}
	homeless := core.PrepareProject(revier.Project{Name: "p", Path: here})
	if _, err := core.Open(homeless, never); !errors.Is(err, core.ErrNoHome) {
		t.Errorf("Open without a home = %v, want ErrNoHome", err)
	}
}
