package tui

import (
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/theme"
)

const tagGap = "  "

// tagsRow is a project's tags beside their label, by key. Closed, it is one
// line: the tags that fit, then how many more there are. Open, it is one tag
// to a line. It reports whether a click on it does anything: it opens a row
// that left a tag out, and closes an open one.
func tagsRow(th theme.Theme, tags []core.Tag, w int, open, hover bool) (string, bool) {
	style, meta := th.Path, th.Meta
	if hover {
		style, meta = th.OnHover(style), th.OnHover(meta)
	}
	head := th.Meta.Render(pad("Tags", detailLabelWidth))
	room := w - detailLabelWidth
	if open {
		lines := make([]string, len(tags))
		for i, t := range tags {
			lines[i] = style.Render(ellipsis(t.String(), room))
		}
		return head + strings.Join(lines, "\n"+strings.Repeat(" ", detailLabelWidth)) + "\n", true
	}
	shown, n := closedTags(tags, room)
	value := style.Render(shown)
	if n < len(tags) {
		value += meta.Render(tagGap + more(len(tags)-n))
	}
	return head + value + "\n", n < len(tags)
}

// closedTags is as many tags as fit in room, with the room a "+N more" after
// them needs kept free, and how many that is. The first tag is shown cut
// rather than not at all.
func closedTags(tags []core.Tag, room int) (string, int) {
	var b strings.Builder
	for i, t := range tags {
		next := t.String()
		if i > 0 {
			next = tagGap + next
		}
		reserve := 0
		if left := len(tags) - i - 1; left > 0 {
			reserve = len(tagGap) + len(more(left))
		}
		if lipgloss.Width(b.String())+lipgloss.Width(next)+reserve > room {
			if i == 0 {
				return ellipsis(next, room-reserve), 1
			}
			return b.String(), i
		}
		b.WriteString(next)
	}
	return b.String(), len(tags)
}

func more(n int) string { return "+" + strconv.Itoa(n) + " more" }
