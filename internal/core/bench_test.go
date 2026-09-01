package core_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/pkg/revier"
)

// benchProject mirrors the real shape: five targets, templated names, matches
// and launch argv, two panels on home.
func benchProject(i int) revier.Project {
	name := revier.ProjectName(fmt.Sprintf("project-%03d", i))
	return revier.Project{
		Name: name,
		Path: "/home/hans/dev/" + string(name),
		Vars: map[string]string{"url": "https://example.invalid/" + string(name)},
		Targets: []revier.Target{
			{Name: "home", Home: true, Key: "ctrl-shift-h", Runtime: &revier.Realization{
				Name: "{{.Name}}", Launch: []string{"sh", "-c", "sleep 600"},
				Match: revier.Match{Title: "^{{.Name}}$"},
				Panels: []revier.PanelSpec{
					{Kind: revier.PanelAgent, Command: []string{"claude"}},
					{Kind: revier.PanelShell},
				},
			}},
			{Name: "editor", Key: "ctrl-o", Window: &revier.Realization{
				Launch: []string{"code", "{{.Path}}"},
				Match:  revier.Match{Class: "^code$", Title: "{{.Name}}"},
			}},
			{Name: "pulls", Key: "ctrl-g", Window: &revier.Realization{
				Launch: []string{"chrome", "--app={{.Vars.url}}", "--class=revier-{{.Name}}"},
				Match:  revier.Match{Class: "^revier-{{.Name}}$"},
			}},
			{Name: "tickets", Key: "ctrl-t", Runtime: &revier.Realization{
				Name: "tickets-{{.Name}}", Launch: []string{"taskmgr-ui"},
				Match: revier.Match{Title: "^tickets-{{.Name}}$"},
			}},
			{Name: "diff", Key: "ctrl-shift-d", Runtime: &revier.Realization{
				Name: "diff-{{.Name}}", Launch: []string{"nvim", "-d"},
				Match: revier.Match{Title: "^diff-{{.Name}}$"},
			}},
		},
	}
}

func benchProjects(n int) []revier.Project {
	out := make([]revier.Project, n)
	for i := range out {
		out[i] = benchProject(i)
	}
	return out
}

// benchCore builds a core whose hosts hold a realistic number of live
// instances: a workspace per project plus desktop windows.
func benchCore(n int) *core.Core {
	rt := hosttest.NewRuntime("rt")
	wm := hosttest.New("wm")
	for i := 0; i < n; i++ {
		rt.Add(fmt.Sprintf("project-%03d", i), "kitty",
			revier.Panel{ID: "1", Kind: revier.PanelAgent, Title: "⠧ Working", Vars: map[string]string{"CS_TAB": "1"}},
			revier.Panel{ID: "2", Kind: revier.PanelShell, Title: "zsh"},
		)
	}
	for i := 0; i < 40; i++ {
		wm.Add(fmt.Sprintf("Some Window %d", i), "other")
	}
	return &core.Core{Runtime: rt, Window: wm}
}

func benchmarkSurvey(b *testing.B, n int) {
	b.Helper()
	c := benchCore(n)
	// Preparation is load-time work and stays outside the timed loop: the
	// benchmark measures what a refresh costs, and a refresh prepares nothing.
	projects, err := core.Prepare(benchProjects(n))
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := c.Survey(ctx, projects); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSurvey1(b *testing.B)   { benchmarkSurvey(b, 1) }
func BenchmarkSurvey90(b *testing.B)  { benchmarkSurvey(b, 90) }
func BenchmarkSurvey360(b *testing.B) { benchmarkSurvey(b, 360) }

// Prepare is the load-time cost per project: one render plus one compile per
// realization. It is paid once per process, never per refresh.
func BenchmarkPrepare(b *testing.B) {
	p := benchProject(0)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := core.PrepareProject(p); err != nil {
			b.Fatal(err)
		}
	}
}

// Render is the templating half of Prepare.
func BenchmarkRender(b *testing.B) {
	p := benchProject(0)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := core.Render(p); err != nil {
			b.Fatal(err)
		}
	}
}

// Compile is the pattern half of Prepare, once per realization at load.
func BenchmarkMatchCompile(b *testing.B) {
	m := revier.Match{Class: "^revier-project-000$", Title: "^project-000$"}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := m.Compile(); err != nil {
			b.Fatal(err)
		}
	}
}
