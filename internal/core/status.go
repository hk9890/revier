package core

import (
	"fmt"
	"time"
)

// StatusPlace is where on the surface a status command's output stands
// (decisions.md D134).
type StatusPlace string

// The places. The two on the rule belong to the screen and run once; the two
// in the pane belong to the selected project and run for it alone.
const (
	StatusRuleLeft    StatusPlace = "rule-left"
	StatusRuleRight   StatusPlace = "rule-right"
	StatusProjectLine StatusPlace = "project-line"
	StatusProject     StatusPlace = "project"
)

// StatusPlaces is every place, in the order a message lists them.
var StatusPlaces = []StatusPlace{StatusRuleLeft, StatusRuleRight, StatusProjectLine, StatusProject}

// OfProject reports a place in the pane, whose command runs for the selected
// project.
func (p StatusPlace) OfProject() bool {
	return p == StatusProjectLine || p == StatusProject
}

// Status is one command and the place its output stands. A field left zero
// is one the table did not set, which is what lets a project's table change
// one field of a shared one.
type Status struct {
	At    StatusPlace
	Title string
	Run   []string
	Every time.Duration
}

// StatusOf is the pane statuses of a project: each shared one merged with
// the project's own at the same place, then the project's own at a place no
// shared one fills. A link takes its own alone: a shared command is written
// against a checkout here, and a link's checkout is on its host
// (decisions.md D134). The errors are the merged statuses that still run
// nothing; each is left out.
func (p Project) StatusOf(shared []Status) ([]Status, []error) {
	var merged []Status
	if p.Remote == nil {
		for _, s := range shared {
			if s.At.OfProject() {
				merged = append(merged, s)
			}
		}
	}
	for _, own := range p.Status {
		i := statusAt(merged, own.At)
		if i < 0 {
			merged = append(merged, own)
			continue
		}
		merged[i] = merged[i].with(own)
	}
	out := merged[:0]
	var errs []error
	for _, s := range merged {
		switch {
		case len(s.Run) == 0 && p.Remote != nil:
			errs = append(errs, fmt.Errorf("status %q: runs nothing; a link takes no status from config.toml, so its table gives run itself", s.At))
			continue
		case len(s.Run) == 0:
			errs = append(errs, fmt.Errorf("status %q: runs nothing, and config.toml has no status there for it to change", s.At))
			continue
		}
		out = append(out, s)
	}
	return out, errs
}

// with is s with every field over sets written onto it.
func (s Status) with(over Status) Status {
	if over.Title != "" {
		s.Title = over.Title
	}
	if len(over.Run) > 0 {
		s.Run = over.Run
	}
	if over.Every != 0 {
		s.Every = over.Every
	}
	return s
}

func statusAt(statuses []Status, at StatusPlace) int {
	for i, s := range statuses {
		if s.At == at {
			return i
		}
	}
	return -1
}
