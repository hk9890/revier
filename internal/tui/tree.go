package tui

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// The bounds on a listing. Two levels and twenty lines is what identifies a
// checkout - a Go tree, a docs tree, an empty directory - and the per
// directory cap keeps a directory of ten thousand files from being read to
// the end for a pane that shows twenty lines of it.
const (
	treeMaxLines   = 20
	treeMaxEntries = 200
	treeTTL        = 30 * time.Second
)

// treeEntry is one cached listing. The cursor moves through ninety projects
// on held-down arrow keys, and the pane is rebuilt after every message, so
// the walk has to be remembered rather than repeated.
type treeEntry struct {
	lines []string
	at    time.Time
}

// treeFor is the cached listing of a project directory. It is deliberately
// not live: a listing that lags by half a minute still says which checkout
// this is, which is the only thing it is for.
func (m *Model) treeFor(path string) []string {
	if e, ok := m.trees[path]; ok && time.Since(e.at) < treeTTL {
		return e.lines
	}
	lines := walkTree(path)
	if m.trees == nil {
		m.trees = map[string]treeEntry{}
	}
	m.trees[path] = treeEntry{lines: lines, at: time.Now()}
	return lines
}

// walkTree lists two levels of dir, in the shape the shell picker's preview
// draws (os-fzf.sh:156). Dot entries are skipped: a project's identity is in
// what it holds, not in its tooling. An unreadable directory lists nothing
// rather than reporting an error, because the pane is informational.
func walkTree(dir string) []string {
	roots, truncated := readSome(dir)
	var out []string
	for i, e := range roots {
		last := i == len(roots)-1
		connector, indent := "├── ", "│   "
		if last {
			connector, indent = "└── ", "    "
		}
		if len(out) >= treeMaxLines {
			return append(out, "...")
		}
		out = append(out, connector+e.Name())
		if !e.IsDir() {
			continue
		}
		children, more := readSome(filepath.Join(dir, e.Name()))
		for j, c := range children {
			if len(out) >= treeMaxLines {
				return append(out, "...")
			}
			childConnector := "├── "
			if j == len(children)-1 && !more {
				childConnector = "└── "
			}
			out = append(out, indent+childConnector+c.Name())
		}
	}
	if truncated && len(out) > 0 {
		out = append(out, "...")
	}
	return out
}

// readSome reads at most treeMaxEntries names, sorted, skipping dot entries.
// The bool says whether the directory held more.
func readSome(dir string) ([]os.DirEntry, bool) {
	f, err := os.Open(dir)
	if err != nil {
		return nil, false
	}
	defer func() { _ = f.Close() }() // read-only; a close error says nothing useful

	entries, err := f.ReadDir(treeMaxEntries)
	if err != nil && len(entries) == 0 {
		return nil, false
	}
	// One more than the cap tells us whether anything was left behind.
	rest, _ := f.ReadDir(1)

	out := make([]os.DirEntry, 0, len(entries))
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out, len(rest) > 0
}
