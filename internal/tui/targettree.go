package tui

import (
	"slices"

	"github.com/hk9890/revier/pkg/revier"
)

// targetKind is what a row of the pane's Targets section is: a window of its
// own, an instance that holds tabs, or a tab inside one.
type targetKind int

const (
	kindWindow targetKind = iota
	kindContainer
	kindTab
)

// treeRows is the pane's targets in the order they nest: each target that is
// no tab, and under a container its tabs, first in the order it opens with
// them and then in the order of the survey. declared is the project's
// configuration, which says what is inside what; a target the survey reports
// and the configuration does not is a window.
func treeRows(views []revier.TargetView, declared []revier.Target) []targetRow {
	runtime := map[revier.TargetName]*revier.Realization{}
	for _, t := range declared {
		runtime[t.Name] = t.Runtime
	}
	present := map[revier.TargetName]bool{}
	for _, tv := range views {
		present[tv.Name] = true
	}
	inside := func(name revier.TargetName) revier.TargetName {
		if r := runtime[name]; r != nil && present[r.Inside] {
			return r.Inside
		}
		return ""
	}
	// A tab hangs only from a target drawn at the top. One its configuration
	// refused - inside itself, inside a tab, or in a cycle - is still
	// surveyed, and stays a row of its own so its reason can be read.
	container := func(name revier.TargetName) revier.TargetName {
		if home := inside(name); home != "" && inside(home) == "" {
			return home
		}
		return ""
	}
	tabs := map[revier.TargetName][]revier.TargetView{}
	for _, tv := range views {
		if home := container(tv.Name); home != "" {
			tabs[home] = append(tabs[home], tv)
		}
	}

	var rows []targetRow
	for _, tv := range views {
		if container(tv.Name) != "" {
			continue
		}
		held := tabs[tv.Name]
		if len(held) == 0 {
			rows = append(rows, targetRow{target: tv})
			continue
		}
		rows = append(rows, targetRow{target: tv, kind: kindContainer})
		var opensWith []revier.TargetName
		if r := runtime[tv.Name]; r != nil {
			opensWith = r.Tabs
		}
		rank := func(tab revier.TargetView) int {
			if i := slices.Index(opensWith, tab.Name); i >= 0 {
				return i
			}
			return len(opensWith)
		}
		slices.SortStableFunc(held, func(a, b revier.TargetView) int { return rank(a) - rank(b) })
		for i, tab := range held {
			rows = append(rows, targetRow{target: tab, kind: kindTab, last: i == len(held)-1})
		}
	}
	return rows
}
