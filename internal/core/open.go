package core

import (
	"errors"
	"fmt"
)

// ErrNoHome is a project with no home target: nothing to open, and nothing to
// return to.
var ErrNoHome = errors.New("no home target")

// Opening is what opening a project comes to: its home target is gone to, or
// its checkout is cloned first.
type Opening uint8

const (
	OpenGo Opening = iota
	OpenClone
)

// Open decides what `revier open` and Enter on a project do, so the command
// line and the surface cannot disagree (decisions.md D90). A project its file
// refused is neither cloned nor opened: the reason is the one thing to do
// about it, and a git_url the load refused must not reach git (D85). One with
// no home target is refused with ErrNoHome. A link is its host's to clone
// (D84), and a checkout that is here is gone to. One that is missing is
// cloned when the file records where from; else the home target is raised
// when it runs, since raising touches no directory, and a fresh start is
// refused rather than made in whatever directory the runtime falls back to.
// running is asked only then, because it costs a listing; when it cannot
// answer - its host could not list (D89) - the refusal carries that reason,
// not a missing git_url.
func Open(p Project, running func() (bool, error)) (Opening, error) {
	if p.Invalid != nil {
		return 0, p.Invalid
	}
	if _, ok := p.Home(); !ok {
		return 0, fmt.Errorf("project %q has %w", p.Name, ErrNoHome)
	}
	if p.Remote != nil || dirExists(p.Path) {
		return OpenGo, nil
	}
	if p.GitURL != "" {
		return OpenClone, nil
	}
	up, err := running()
	if err != nil {
		return 0, err
	}
	if up {
		return OpenGo, nil
	}
	return 0, fmt.Errorf("project %q: %s does not exist, and the project file records no git_url to clone it from", p.Name, p.Path)
}
