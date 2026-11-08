package kitty

import "testing"

// A panel's directory is its process's, as --cwd=current takes it: an agent
// under `kitten run-shell` that moved into a worktree reports the worktree,
// not the window's first process's directory. Without the process's own
// directory, the window's stands in. This is the window up to kitty 0.49,
// whose own process is the wrapper.
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

// From kitty 0.50 the window's own process, cmdline and cwd are the program
// the wrapper runs, while the group still lists the wrapper, whose parent is
// kitty. The panel is the same as up to 0.49: the agent, in its worktree, and
// not the wrapper or the tool the agent runs.
func TestAPanelUnderKitty050IsTheProgramTheWrapperRuns(t *testing.T) {
	tree := map[int]int{10: 5, 5: 1, 11: 10, 12: 11}
	parents := func(pid int) (int, bool) {
		ppid, ok := tree[pid]
		return ppid, ok
	}
	w := window{ID: 4, PID: 11, CWD: "/p/wt", Foreground: []process{
		{PID: 10, Cmdline: []string{"kitten", "run-shell"}, CWD: "/p"},
		{PID: 11, Cmdline: []string{"claude"}, CWD: "/p/wt"},
		{PID: 12, Cmdline: []string{"sleep", "1800"}, CWD: "/p/wt"},
	}}
	got := panelOf(w, parents)
	if got.PID != 11 || len(got.Command) == 0 || got.Command[0] != "claude" || got.Dir != "/p/wt" {
		t.Errorf("panel = %v pid %d in %q, want claude's 11 in /p/wt", got.Command, got.PID, got.Dir)
	}
}
