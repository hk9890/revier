package kitty

import "testing"

// A panel's directory is its process's, as --cwd=current takes it: an agent
// under `kitten run-shell` that moved into a worktree reports the worktree,
// not the window's first process's directory. Without the process's own
// directory, the window's stands in.
func TestAPanelsDirectoryIsItsProcesss(t *testing.T) {
	noParents := func(int) (int, bool) { return 0, false }
	w := window{ID: 4, PID: 10, CWD: "/p", Foreground: []process{
		{PID: 10, Cmdline: []string{"kitten", "run-shell"}, CWD: "/p"},
		{PID: 11, Cmdline: []string{"claude"}, CWD: "/p/wt"},
	}}
	if got := panelOf(w, noParents); got.PID != 11 || got.Dir != "/p/wt" {
		t.Errorf("panel = pid %d in %q, want claude's 11 in /p/wt", got.PID, got.Dir)
	}
	w.Foreground[1].CWD = ""
	if got := panelOf(w, noParents).Dir; got != "/p" {
		t.Errorf("dir = %q, want the window's /p", got)
	}
}
