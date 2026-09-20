package main

import (
	"log/slog"
	"os"
)

// leaveStartDir moves into the home directory, or into "/" when the home is
// unset or missing. The start directory is often a worktree, and the process
// outlives it: once it is removed, every process started from here inherits a
// directory that no longer exists, and git, claude and a probe script each
// refuse to run in one (decisions.md D92).
func leaveStartDir() {
	home, err := os.UserHomeDir()
	if err == nil {
		err = os.Chdir(home)
	}
	if err != nil {
		slog.Warn("start directory", "err", err)
		_ = os.Chdir("/")
	}
}

// leaveLostStartDir leaves a start directory that is already gone, and leaves
// a live one alone.
//
// The surface leaves unconditionally because it outlives the directory it was
// started in. A command does not live long enough for that, but it can be run
// in a directory that was removed under the shell, and then every probe and
// every git call it makes fails for a reason that has nothing to do with what
// was asked. A directory that is still there is the command's to keep: `new`,
// `each` and the project of the working directory are all read from it.
func leaveLostStartDir() {
	if _, err := os.Getwd(); err == nil {
		return
	}
	leaveStartDir()
}
