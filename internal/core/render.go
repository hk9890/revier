package core

import (
	"fmt"
	"strings"
	"text/template"

	"github.com/hk9890/revier/pkg/revier"
)

// Render expands every template in a project's realizations against the
// project itself. A host receives literal argv and literal match patterns and
// never sees a `{{ }}`, which is what keeps templating out of every adapter.
//
// The fields available are those of revier.Project: {{.Name}}, {{.Path}}, and
// {{.Vars.<key>}}.
func Render(p revier.Project) (revier.Project, error) {
	out := p
	out.Targets = make([]revier.Target, len(p.Targets))
	copy(out.Targets, p.Targets)

	for i := range out.Targets {
		for _, slot := range []**revier.Realization{&out.Targets[i].Window, &out.Targets[i].Runtime} {
			if *slot == nil {
				continue
			}
			r, err := renderRealization(p, **slot)
			if err != nil {
				return revier.Project{}, fmt.Errorf("target %q: %w", out.Targets[i].Name, err)
			}
			*slot = &r
		}
	}
	return out, nil
}

func renderRealization(p revier.Project, r revier.Realization) (revier.Realization, error) {
	out := r
	var err error

	if out.Name, err = expand(p, r.Name); err != nil {
		return revier.Realization{}, fmt.Errorf("name: %w", err)
	}
	if out.Match.Class, err = expand(p, r.Match.Class); err != nil {
		return revier.Realization{}, fmt.Errorf("match class: %w", err)
	}
	if out.Match.Title, err = expand(p, r.Match.Title); err != nil {
		return revier.Realization{}, fmt.Errorf("match title: %w", err)
	}
	if len(r.Launch) > 0 {
		out.Launch = make([]string, len(r.Launch))
		for i, arg := range r.Launch {
			if out.Launch[i], err = expand(p, arg); err != nil {
				return revier.Realization{}, fmt.Errorf("launch[%d]: %w", i, err)
			}
		}
	}
	if len(r.Panels) > 0 {
		out.Panels = make([]revier.PanelSpec, len(r.Panels))
		copy(out.Panels, r.Panels)
		for i, spec := range r.Panels {
			if len(spec.Command) == 0 {
				continue
			}
			cmd := make([]string, len(spec.Command))
			for j, arg := range spec.Command {
				if cmd[j], err = expand(p, arg); err != nil {
					return revier.Realization{}, fmt.Errorf("panel[%d] command[%d]: %w", i, j, err)
				}
			}
			out.Panels[i].Command = cmd
		}
	}
	return out, nil
}

// expand renders one string. A template referring to a missing key is an
// error rather than an empty string: a launch argv silently losing an argument
// is far harder to diagnose than a refusal at load.
func expand(p revier.Project, s string) (string, error) {
	if !strings.Contains(s, "{{") {
		return s, nil
	}
	t, err := template.New("realization").Option("missingkey=error").Parse(s)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	if err := t.Execute(&b, p); err != nil {
		return "", err
	}
	return b.String(), nil
}
