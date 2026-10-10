package core_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/pkg/revier"
)

// benchCore builds a core whose hosts hold a realistic number of live
// instances: a workspace per project plus desktop windows.
func benchCore(n int) *core.Core {
	rt := hosttest.NewRuntime("rt")
	wm := hosttest.New("wm")
	for i := 0; i < n; i++ {
		rt.Add(fmt.Sprintf("project-%03d", i), "kitty",
			revier.Panel{ID: "1", Kind: revier.PanelTool, Title: "⠧ Working", Vars: map[string]string{"CS_TAB": "1"}},
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
	projects := core.Prepare(benchProjects(n))
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
		core.PrepareProject(p)
	}
}

// Render is the templating half of Prepare.
func BenchmarkRender(b *testing.B) {
	p := benchProject(0)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, errs := core.Render(p); errors.Join(errs...) != nil {
			b.Fatal(errors.Join(errs...))
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
