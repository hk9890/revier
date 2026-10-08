package tui

import (
	"cmp"
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	"github.com/sahilm/fuzzy"
)

// ranked is the filter under every list that takes a query. The fuzzy scorer
// says which rows match and is the last word on their order, not the first:
// a query is a view over the list (decisions.md D113), so a row stays in the
// group the list sorted it into, and within a group a row that holds the
// query whole stands above one that holds its letters apart. The scorer alone
// does neither: it gives a letter after a separator four times what it gives
// a letter beside the last, so "cap" ranked claude-code-podman above
// cap-data-intelligence, stopped above open.
//
// group is the place the list's order gives row i, lowest first.
func ranked(group func(i int) int) list.FilterFunc {
	return func(term string, targets []string) []list.Rank {
		type match struct {
			rank  list.Rank
			group int
			apart int
			score int
		}
		var matches []match
		for _, f := range fuzzy.FindNoSort(term, targets) {
			m := match{
				rank:  list.Rank{Index: f.Index, MatchedIndexes: f.MatchedIndexes},
				group: group(f.Index),
				apart: 1,
				score: f.Score,
			}
			if run := wholeMatch(term, targets[f.Index]); run != nil {
				m.rank.MatchedIndexes, m.apart = run, 0
			}
			matches = append(matches, m)
		}
		// Rows the three measures cannot tell apart keep the list's own
		// order.
		slices.SortFunc(matches, func(a, b match) int {
			return cmp.Or(
				cmp.Compare(a.group, b.group),
				cmp.Compare(a.apart, b.apart),
				cmp.Compare(b.score, a.score),
				cmp.Compare(a.rank.Index, b.rank.Index),
			)
		})
		out := make([]list.Rank, len(matches))
		for i, m := range matches {
			out[i] = m.rank
		}
		return out
	}
}

// ungrouped is the order of a list that sorts its rows into no groups.
func ungrouped(int) int { return 0 }

// wholeMatch is the byte positions of the first place the target holds the
// query whole, whatever the case of either, or nil. These are the letters to
// light: the scorer's own alignment of a whole match can be a scattered one.
// A target whose lower case is another length has no positions to give.
func wholeMatch(term, target string) []int {
	lower := strings.ToLower(target)
	if len(lower) != len(target) {
		return nil
	}
	term = strings.ToLower(term)
	at := strings.Index(lower, term)
	if at < 0 {
		return nil
	}
	run := make([]int, len(term))
	for i := range run {
		run[i] = at + i
	}
	return run
}
