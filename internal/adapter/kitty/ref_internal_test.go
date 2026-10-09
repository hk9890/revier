// The instance id is the socket and the OS window id in one string. Raise,
// Close and the ownership check all read it back, so the two directions must
// agree, with no kitty and no recorded output.
package kitty

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/hk9890/revier/pkg/revier"
)

// Discovery reads the kernel's socket table, which holds the sockets of every
// user. It takes the socket files of the user's own runtime directory, and the
// abstract names a kitty started before 0.14 listens on. A path another user
// bound is taken as written: one that only cleans to the runtime directory is
// a file wherever its symbolic links lead.
func TestDiscoveryFindsTheSocketFilesOfTheRuntimeDirectory(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "/run/user/1000/")
	table := `Num       RefCount Protocol Flags    Type St Inode Path
0000000000000000: 00000002 00000000 00010000 0001 01 101 /run/user/1000/kitty-4002
0000000000000000: 00000002 00000000 00010000 0001 01 102 @kitty-4001
0000000000000000: 00000002 00000000 00010000 0001 01 103 /run/user/1000/kitty-4000
0000000000000000: 00000002 00000000 00010000 0001 01 104 /run/user/1001/kitty-4003
0000000000000000: 00000002 00000000 00010000 0001 01 105 /tmp/kitty-4004
0000000000000000: 00000002 00000000 00010000 0001 01 106 /run/user/1000/kitty-4005/sock
0000000000000000: 00000002 00000000 00010000 0001 01 107 /run/user/1000/bus
0000000000000000: 00000002 00000000 00010000 0001 01 108 /tmp/link/../../run/user/1000/kitty-4006
0000000000000000: 00000002 00000000 00010000 0001 01 109 /run/user/1001/../1000/kitty-4007
0000000000000000: 00000002 00000000 00010000 0001 01 110 /run/user/1000x/kitty-4008
0000000000000000: 00000003 00000000 00000000 0001 03 111
`
	got := candidates("unix:/run/user/1000/kitty-4000", table)
	want := []string{"unix:/run/user/1000/kitty-4000", "unix:/run/user/1000/kitty-4002", "unix:@kitty-4001"}
	if !slices.Equal(got, want) {
		t.Errorf("candidates = %q, want %q: the socket this process was started in, then the others", got, want)
	}
	for socket, pid := range map[string]int{
		"unix:/run/user/1000/kitty-4000":  4000,
		"unix:/run/user/1000//kitty-4009": 4009,
		"unix:@kitty-4001":                4001,
		"unix:/run/user/1001/kitty-4003":  0,
		"unix:/tmp/mykitty":               0,
	} {
		if got := pidOf(socket); got != pid {
			t.Errorf("pidOf(%q) = %d, want %d", socket, got, pid)
		}
	}
}

// Without a runtime directory no path is the user's own, so no socket file is
// taken. A relative one is no directory either.
func TestDiscoveryTakesNoSocketFileWithoutARuntimeDirectory(t *testing.T) {
	table := "0000000000000000: 00000002 00000000 00010000 0001 01 101 /kitty-4000\n" +
		"0000000000000000: 00000002 00000000 00010000 0001 01 102 kitty-4001\n" +
		"0000000000000000: 00000002 00000000 00010000 0001 01 103 run/kitty-4002\n"
	for _, dir := range []string{"", ".", "run"} {
		t.Setenv("XDG_RUNTIME_DIR", dir)
		if got := candidates("", table); len(got) != 0 {
			t.Errorf("XDG_RUNTIME_DIR=%q: candidates = %q, want none", dir, got)
		}
	}
}

// The socket Open asks a new kitty for must be one discovery takes, or the
// kitty is never found and every press starts another.
func TestDiscoveryTakesTheSocketANewKittyIsStartedOn(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "/run/user/1000/")
	var listenOn string
	h := &Host{
		sockets: func() []string { return nil },
		start: func(_ context.Context, args ...string) error {
			listenOn = args[slices.Index(args, "--listen-on")+1]
			return errors.New("no kitty in this test")
		},
	}
	if _, _, err := h.startKitty(context.Background(), revier.Realization{Name: "session:demo"}, revier.PanelSpec{}); err == nil {
		t.Fatal("startKitty reported a kitty that the starter never started")
	}
	socket := strings.ReplaceAll(listenOn, "{kitty_pid}", "4000")
	if pid := pidOf(socket); pid != 4000 {
		t.Errorf("pidOf(%q) = %d, want 4000: discovery does not take the socket Open asks for", socket, pid)
	}
}

func TestARefRoundTripsThroughItsSocketAndWindowID(t *testing.T) {
	id := refID("unix:@kitty-4000", 7)
	if id != "@kitty-4000/7" {
		t.Fatalf("refID = %q, want @kitty-4000/7", id)
	}
	socket, window, err := parseRef(id)
	if err != nil || socket != "unix:@kitty-4000" || window != 7 {
		t.Errorf("parseRef(%q) = %q, %d, %v; want the socket and window back", id, socket, window, err)
	}
	// A socket path holds slashes of its own: the window id is after the last.
	socket, window, err = parseRef("/tmp/kitty/sock/12")
	if err != nil || socket != "unix:/tmp/kitty/sock" || window != 12 {
		t.Errorf("parseRef of a path socket = %q, %d, %v", socket, window, err)
	}
	for _, bad := range []string{"", "@kitty-4000", "@kitty-4000/", "@kitty-4000/seven"} {
		if _, _, err := parseRef(bad); err == nil {
			t.Errorf("parseRef(%q) accepted a ref that names no window", bad)
		}
	}
}
