package config

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/hk9890/revier/internal/core"
)

// StatusTable is one [[status]] as config.toml or a project file writes it.
type StatusTable struct {
	At    string   `toml:"at"`
	Title string   `toml:"title"`
	Run   []string `toml:"run"`
	Every string   `toml:"every"`
}

// MinStatusEvery is the shortest wait between two runs of one status
// command. A shorter one would run a command that may reach a network more
// often than anyone reads the line it draws.
const MinStatusEvery = 5 * time.Second

// Statuses is config.toml's [[status]] tables without the ones a rule
// refused: the rule's own, and the pane's shared with every local project.
func (c *Config) Statuses() []core.Status {
	return c.statuses
}

// validateStatuses is the tables a rule accepts, and one error per table it
// refuses (decisions.md D85): the table goes, and nothing else does. inProject
// is a project file's tables, which hold the pane's places alone and may
// leave run to the shared table they change.
func validateStatuses(tables []StatusTable, inProject bool) ([]core.Status, []error) {
	var out []core.Status
	var errs []error
	seen := map[core.StatusPlace]bool{}
	for _, t := range tables {
		s, err := statusOf(t, inProject)
		if err == nil && seen[s.At] {
			err = fmt.Errorf("status %q: two tables stand at this place, and a place shows one status", s.At)
		}
		if err != nil {
			errs = append(errs, err)
			continue
		}
		seen[s.At] = true
		out = append(out, s)
	}
	return out, errs
}

func statusOf(t StatusTable, inProject bool) (core.Status, error) {
	at := core.StatusPlace(t.At)
	if t.At == "" {
		return core.Status{}, errors.New("status: no at; give it one of " + placeList())
	}
	if !slices.Contains(core.StatusPlaces, at) {
		return core.Status{}, fmt.Errorf("status %q: at is one of %s", t.At, placeList())
	}
	if inProject && !at.OfProject() {
		return core.Status{}, fmt.Errorf("status %q: stands over the whole screen, so only config.toml declares it", t.At)
	}
	if t.Title != "" && at != core.StatusProject {
		return core.Status{}, fmt.Errorf("status %q: title names a section of the pane, and only status %q has one", t.At, core.StatusProject)
	}
	if !inProject && len(t.Run) == 0 {
		return core.Status{}, fmt.Errorf("status %q: runs nothing", t.At)
	}
	s := core.Status{At: at, Title: t.Title, Run: slices.Clone(t.Run)}
	// The program is expanded as a probe's exec is, so "~/bin/x" runs; its
	// arguments are the command's own.
	if len(s.Run) > 0 {
		s.Run[0] = ExpandHome(s.Run[0])
	}
	if t.Every != "" {
		every, err := time.ParseDuration(t.Every)
		if err != nil {
			return core.Status{}, fmt.Errorf("status %q: every: %w", t.At, err)
		}
		if every < MinStatusEvery {
			return core.Status{}, fmt.Errorf("status %q: every is %s, and the least is %s", t.At, every, MinStatusEvery)
		}
		s.Every = every
	}
	return s, nil
}

func placeList() string {
	names := make([]string, len(core.StatusPlaces))
	for i, p := range core.StatusPlaces {
		names[i] = string(p)
	}
	return strings.Join(names, ", ")
}

// projectStatuses is the [[status]] tables of a project file, and why each
// refused one is.
func projectStatuses(data []byte) ([]core.Status, []error) {
	var file struct {
		Status []StatusTable `toml:"status"`
	}
	if _, err := toml.Decode(string(data), &file); err != nil {
		return nil, []error{fmt.Errorf("status: %w", err)}
	}
	return validateStatuses(file.Status, true)
}
