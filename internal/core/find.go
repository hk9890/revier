package core

import (
	"path/filepath"
	"strings"

	"github.com/hk9890/revier/pkg/revier"
)

// Matches reports whether word finds the project v shows: a part of its name,
// of its checkout's directory name, of its repository's name or, for a link,
// of its name on the host; a part of an agent's activity, or the whole of the
// name an agent's harness gives it (decisions.md D161). Case does not count.
// It is how a word a user gives, such as a tool's name, finds the project it
// lives in.
func Matches(v revier.ProjectView, word string) bool {
	w := strings.ToLower(word)
	has := func(s string) bool { return s != "" && strings.Contains(strings.ToLower(s), w) }
	p := v.Project
	if has(string(p.Name)) || has(dirName(p.Path)) || has(repoName(p.GitURL)) {
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

// repoName is the repository's name in a git URL, without ".git":
// "taskmgr" in git@github.com:o/taskmgr.git and in https://github.com/o/taskmgr.
func repoName(url string) string {
	url = strings.TrimSuffix(strings.TrimRight(url, "/"), ".git")
	return url[strings.LastIndexAny(url, "/:")+1:]
}
