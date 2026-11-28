package tui

import (
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/plain"
	"github.com/hk9890/revier/internal/theme"
)

const tagGap = "  "

// tagsRow is a project's tags beside their label, by key. Closed, it is one
// line: the tags that fit, then how many more there are. Open, it is one tag
// to a line, a long one wrapped. It reports whether a click on it does
// anything: it opens a row that left a tag out or cut one, and closes an open
// one.
func tagsRow(th theme.Theme, tags []core.Tag, w int, open, hover bool) (string, bool) {
	style, meta := th.Path, th.Meta
	if hover {
		style, meta = th.OnHover(style), th.OnHover(meta)
	}
	head := th.Meta.Render(pad("Tags", detailLabelWidth))
	room := w - detailLabelWidth
	indent := "\n" + strings.Repeat(" ", detailLabelWidth)
	if open {
		var lines []string
		for _, t := range tags {
			for _, part := range wrap(tagText(t), room) {
				lines = append(lines, style.Render(part))
			}
		}
		return head + strings.Join(lines, indent) + "\n", true
	}
	shown, n, cut := closedTags(tags, room)
	value := style.Render(shown)
	if n < len(tags) {
		value += meta.Render(tagGap + more(len(tags)-n))
	}
	return head + value + "\n", n < len(tags) || cut
}

// closedTags is as many tags as fit in room, how many that is, and whether
// the one shown was cut. All of them when they fit; else the room a
// "+N more" after them needs is kept free. The first tag is shown cut rather
// than not at all, where room is left for any of it.
func closedTags(tags []core.Tag, room int) (string, int, bool) {
	texts := make([]string, len(tags))
	for i, t := range tags {
		texts[i] = tagText(t)
	}
	if all := strings.Join(texts, tagGap); lipgloss.Width(all) <= room {
		return all, len(tags), false
	}
	var b strings.Builder
	for i, text := range texts {
		next := text
		if i > 0 {
			next = tagGap + next
		}
		reserve := 0
		if left := len(tags) - i - 1; left > 0 {
			reserve = len(tagGap) + len(more(left))
		}
		if lipgloss.Width(b.String())+lipgloss.Width(next)+reserve <= room {
			b.WriteString(next)
			continue
		}
		if i > 0 {
			return b.String(), i, false
		}
		if room-reserve < 1 {
			return "", 0, false
		}
		return ellipsis(next, room-reserve), 1, true
	}
	return b.String(), len(tags), false
}

// tagText is a tag as the pane draws it: a value from a file is the user's
// text, and a control character in it would break the frame.
func tagText(t core.Tag) string { return plain.Line(t.String()) }

func more(n int) string { return "+" + strconv.Itoa(n) + " more" }
