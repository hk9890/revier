// The recorded call, the process tree and the host the tests of this package
// share.
package kitty_test

import (
	"github.com/hk9890/revier/internal/adapter/kitty"
)

// call is one recorded kitten invocation.
type call struct {
	socket string
	args   []string
}

// parents is the process tree behind every recorded listing of these tests,
// child to parent. The pids are made up, so the host must not read them from
// this machine's /proc, where they belong to other processes.
var parents = map[int]int{
	4002: 4001, 4003: 4002,
	5002: 5001, 5003: 5002, 5021: 5020,
	6002: 6001, 6003: 6002, 6004: 6003,
	7002: 7001, 7003: 1,
}

// newHost is a Host that reads the process tree from parents.
func newHost() *kitty.Host {
	h := &kitty.Host{}
	h.SetParents(func(pid int) (int, bool) {
		ppid, ok := parents[pid]
		return ppid, ok
	})
	return h
}
