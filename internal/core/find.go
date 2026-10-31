package core

import (
	"cmp"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/hk9890/revier/pkg/revier"
)

// Matches reports whether word finds the project v shows: a part of its name,
// of its checkout's directory name, of its repository's name or, for a link,
// of its name on the host; a part of an agent's activity, or the whole of the
// name an agent's harness gives it (decisions.md D161). Case does not count,
// and an empty word finds nothing. It is how a word a user gives, such as a
// tool's name, finds the project it lives in.
func Matches(v revier.ProjectView, word string) bool {
	if word == "" {
		return false
	}
	w := strings.ToLower(word)
	has := func(s string) bool { return strings.Contains(strings.ToLower(s), w) }
	p := v.Project
	if has(string(p.Name)) || has(dirName(p.Path)) || has(RepoName(p.GitURL)) {
		return true
	}
	if p.Remote != nil && has(string(p.Remote.Project)) {
		return true
	}
	for _, a := range v.Agents {
		if has(a.State.Activity) || strings.EqualFold(a.State.Name, word) {
			return true
		}
	}
	return false
}

func dirName(path string) string {
	if path == "" {
		return ""
	}
	return filepath.Base(path)
}

// RepoName is the repository's name in a git URL, without ".git", which is
// the directory git clone makes of it: "taskmgr" in
// git@github.com:o/taskmgr.git and in https://github.com/o/taskmgr.
func RepoName(url string) string {
	url = strings.TrimSuffix(strings.TrimRight(url, "/"), ".git")
	return url[strings.LastIndexAny(url, "/:")+1:]
}

// Closest is the names of the projects a name that is no project's probably
// meant, the nearest first and at most three: the projects Matches finds for
// the name, and those whose name is at most half the name's length of edits
// from it.
func Closest(projects []Project, name string) []revier.ProjectName {
	type near struct {
		name revier.ProjectName
		dist int
	}
	var found []near
	for _, p := range projects {
		d := edits(strings.ToLower(name), strings.ToLower(string(p.Name)))
		if d <= utf8.RuneCountInString(name)/2 || Matches(revier.ProjectView{Project: p.Project}, name) {
			found = append(found, near{p.Name, d})
		}
	}
	slices.SortStableFunc(found, func(a, b near) int { return cmp.Compare(a.dist, b.dist) })
	var names []revier.ProjectName
	for _, n := range found[:min(len(found), 3)] {
		names = append(names, n.name)
	}
	return names
}

// edits is the Levenshtein distance between a and b: the fewest inserted,
// deleted or replaced characters that turn one into the other.
func edits(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev, cur := make([]int, len(rb)+1), make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := range ra {
		cur[0] = i + 1
		for j := range rb {
			replace := prev[j]
			if ra[i] != rb[j] {
				replace++
			}
			cur[j+1] = min(prev[j+1]+1, cur[j]+1, replace)
		}
		prev, cur = cur, prev
	}
	return prev[len(rb)]
}
