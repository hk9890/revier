// Package procgroup runs a host command in a process group of its own, and
// ends every such group still running when revier is made to end.
//
// A group of its own keeps the ctrl+c a terminal sends to revier's group from
// ending the command of a shutdown's or a restore's step: the first ctrl+c
// cancels the walk after that step. The same group would outlive revier when
// the second ctrl+c ends revier on a step that hangs, so the groups that run
// are kept here, and at that second ctrl+c KillAll ends them before revier
// exits.
//
// A command made by CommandContext ends with its whole group when its context
// ends: a child it started would otherwise hold its output open past it.
//
// The process Run starts also ends when revier is killed, which KillAll
// cannot see: else a caller that kills a slow `revier list` at its deadline
// leaves `claude agents --json` running, and its next poll starts one more.
// The kernel ends that process alone, not a child it started.
package procgroup

import (
	"context"
	"errors"
	"os/exec"
	"runtime"
	"sync"
	"syscall"
	"time"
)

// ErrEnded is a command refused because KillAll has run: revier is ending,
// and a command started now would outlive it.
var ErrEnded = errors.New("revier is ending: no command starts")

var groups = struct {
	sync.Mutex
	running map[int]bool
	ended   bool
}{running: map[int]bool{}}

// CommandContext is exec.CommandContext for Run: when ctx ends, it kills the
// whole process group, not only the command, and stops waiting for the output
// soon after, which a child that left the group can hold open.
func CommandContext(ctx context.Context, name string, arg ...string) *exec.Cmd {
	c := exec.CommandContext(ctx, name, arg...)
	c.Cancel = func() error { return syscall.Kill(-c.Process.Pid, syscall.SIGKILL) }
	c.WaitDelay = 50 * time.Millisecond
	return c
}

// Run runs c as Cmd.Run does, in a process group of its own, and keeps the
// group known until c has ended. The kernel kills c, not a child of c, when
// revier dies.
func Run(c *exec.Cmd) error {
	if c.SysProcAttr == nil {
		c.SysProcAttr = &syscall.SysProcAttr{}
	}
	c.SysProcAttr.Setpgid = true
	c.SysProcAttr.Pdeathsig = syscall.SIGKILL
	// The kernel sends Pdeathsig when the thread that started c ends, not
	// only the process, and Go ends a thread when a goroutine exits locked
	// to it. Holding the thread until c ends keeps every other goroutine
	// off it, so the signal comes only at revier's end.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	// The start is made under the lock, so KillAll never runs between a
	// start and the group being known.
	groups.Lock()
	if groups.ended {
		groups.Unlock()
		return ErrEnded
	}
	if err := c.Start(); err != nil {
		groups.Unlock()
		return err
	}
	pgid := c.Process.Pid
	groups.running[pgid] = true
	groups.Unlock()

	err := c.Wait()
	groups.Lock()
	delete(groups.running, pgid)
	groups.Unlock()
	return err
}

// KillAll ends every group Run started that still runs, and refuses every
// later Run. It is for the end of the process alone.
func KillAll() {
	groups.Lock()
	defer groups.Unlock()
	groups.ended = true
	for pgid := range groups.running {
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
	}
}
