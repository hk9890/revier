package core

import "github.com/hk9890/revier/pkg/revier"

// Skip says why `revier each` leaves a project out. The zero value means the
// project runs.
type Skip string

const (
	// SkipMissing: the project's path is not a directory on this machine, so
	// there is nowhere to run the command.
	SkipMissing Skip = "missing"
	// SkipDuplicate: an earlier project has the same directory. Running the
	// command there twice is at best wasted and at worst a second commit.
	SkipDuplicate Skip = "duplicate"
	// SkipFiltered: the --filter test failed in the project's directory.
	SkipFiltered Skip = "filtered"
	// SkipRemote: the project lives on another machine (decisions.md D40),
	// and its path is in that machine's terms.
	SkipRemote Skip = "remote"
)

// Pick is one project and whether a run in every project reaches it.
type Pick struct {
	Project Project
	Skip    Skip
	// SameAs is the project whose directory a duplicate shares.
	SameAs revier.ProjectName
}

// Select decides which projects a run in every project reaches, in the order
// given, and why each of the others is left out.
//
// resolve reports a path's real directory, or false when there is none; two
// paths that resolve alike are one directory, and the first project to claim
// it runs. keep is the user's filter and may be nil. It is asked last, and
// only about a directory that exists and nobody else has claimed, because it
// runs a process in that directory.
func Select(projects []Project, resolve func(path string) (string, bool), keep func(dir string) bool) []Pick {
	picks := make([]Pick, 0, len(projects))
	owner := map[string]revier.ProjectName{}
	for _, p := range projects {
		pick := Pick{Project: p}
		if p.Host != "" {
			pick.Skip = SkipRemote
			picks = append(picks, pick)
			continue
		}
		real, ok := resolve(p.Path)
		first, taken := owner[real]
		switch {
		case !ok:
			pick.Skip = SkipMissing
		case taken:
			pick.Skip, pick.SameAs = SkipDuplicate, first
		default:
			owner[real] = p.Name
			if keep != nil && !keep(p.Path) {
				pick.Skip = SkipFiltered
			}
		}
		picks = append(picks, pick)
	}
	return picks
}
