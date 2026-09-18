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
// {{.Vars.<key>}}. A realization that names no Dir gets the project path, and
// every panel gets the realization's Dir.
//
// A target whose templates do not render is returned as it was written, and
// its error sits at the same index of the returned slice. The other targets
// are rendered and usable: one launch argv that cannot be built is that
// target's problem and no other's (decisions.md D85).
func Render(p revier.Project) (revier.Project, []error) {
	out := p
	out.Targets = make([]revier.Target, len(p.Targets))
	copy(out.Targets, p.Targets)
	errs := make([]error, len(p.Targets))

	for i := range out.Targets {
		for _, slot := range []**revier.Realization{&out.Targets[i].Window, &out.Targets[i].Runtime} {
			if *slot == nil {
				continue
			}
			r, err := renderRealization(p, **slot)
			if err != nil {
				errs[i] = fmt.Errorf("target %q: %w", out.Targets[i].Name, err)
				break
			}
			*slot = &r
		}
	}
	return out, errs
}

func renderRealization(p revier.Project, r revier.Realization) (revier.Realization, error) {
	out := r
	var err error

	if out.Name, err = expand(p, r.Name); err != nil {
		return revier.Realization{}, fmt.Errorf("name: %w", err)
	}
	if out.Dir, err = expand(p, r.Dir); err != nil {
		return revier.Realization{}, fmt.Errorf("dir: %w", err)
	}
	// A remote project's path is on its host, and a local launch started in
	// it would fail before reaching the host; it starts where revier did.
	if out.Dir == "" && p.Remote == nil {
		out.Dir = p.Path
	}
	if out.Match.Class, err = expand(p, r.Match.Class); err != nil {
		return revier.Realization{}, fmt.Errorf("match class: %w", err)
	}
	if out.Match.Title, err = expand(p, r.Match.Title); err != nil {
		return revier.Realization{}, fmt.Errorf("match title: %w", err)
	}
	if out.Place, err = expand(p, r.Place); err != nil {
		return revier.Realization{}, fmt.Errorf("place: %w", err)
	}
	if len(r.Launch) > 0 {
		if out.Launch, err = expandArgv(p, r.Launch); err != nil {
			return revier.Realization{}, fmt.Errorf("launch%w", err)
		}
	}
	if len(r.Panels) > 0 {
		out.Panels = make([]revier.PanelSpec, len(r.Panels))
		copy(out.Panels, r.Panels)
		for i, spec := range r.Panels {
			if out.Panels[i].Title, err = expand(p, spec.Title); err != nil {
				return revier.Realization{}, fmt.Errorf("panel[%d] title: %w", i, err)
			}
			// Every panel starts where the realization does unless a restore
			// says otherwise, and that fallback is decided once, here, rather
			// than in every runtime.
			if out.Panels[i].Dir == "" {
				out.Panels[i].Dir = out.Dir
			}
			if len(spec.Command) == 0 {
				continue
			}
			cmd, err := expandArgv(p, spec.Command)
			if err != nil {
				return revier.Realization{}, fmt.Errorf("panel[%d] command%w", i, err)
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

// expandArgv renders an argv list, and refuses an argument that renders to
// nothing (decisions.md D83). A template over a value the project leaves
// empty - a link with no path recorded, a var written blank - hands the
// program an empty argument, which it reads as the current directory, an
// empty pattern or a missing operand, and nothing says why. The error names
// the index, because an argv is read by position. A word written empty is
// not rendered and stays: the file says so, and `-m ""` is an argument.
func expandArgv(p revier.Project, argv []string) ([]string, error) {
	out := make([]string, len(argv))
	for i, arg := range argv {
		var err error
		if out[i], err = expand(p, arg); err != nil {
			return nil, fmt.Errorf("[%d]: %w", i, err)
		}
		if out[i] == "" && arg != "" {
			return nil, fmt.Errorf("[%d]: %q renders to an empty argument", i, arg)
		}
	}
	return out, nil
}

// RenderArgv expands an argv list against a project, for a configured action.
// It is the one templating route out of this package: an action's run argv
// and a realization's launch argv are rendered by the same rules, so a user
// learns one template language.
func RenderArgv(p revier.Project, argv []string) ([]string, error) {
	out, err := expandArgv(p, argv)
	if err != nil {
		return nil, fmt.Errorf("argv%w", err)
	}
	return out, nil
}
