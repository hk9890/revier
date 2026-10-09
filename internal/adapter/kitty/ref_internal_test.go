// The instance id is the socket and the OS window id in one string. Raise,
// Close and the ownership check all read it back, so the two directions must
// agree, with no kitty and no recorded output.
package kitty

import (
	"slices"
	"testing"
)

// Discovery reads the kernel's socket table, which holds the sockets of every
// user. It takes the socket files of the user's own runtime directory, and the
// abstract names a kitty started before 0.14 listens on.
func TestDiscoveryFindsTheSocketFilesOfTheRuntimeDirectory(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "/run/user/1000")
	table := `Num       RefCount Protocol Flags    Type St Inode Path
0000000000000000: 00000002 00000000 00010000 0001 01 101 /run/user/1000/kitty-4002
0000000000000000: 00000002 00000000 00010000 0001 01 102 @kitty-4001
0000000000000000: 00000002 00000000 00010000 0001 01 103 /run/user/1000/kitty-4000
0000000000000000: 00000002 00000000 00010000 0001 01 104 /run/user/1001/kitty-4003
0000000000000000: 00000002 00000000 00010000 0001 01 105 /tmp/kitty-4004
0000000000000000: 00000002 00000000 00010000 0001 01 106 /run/user/1000/kitty-4005/sock
0000000000000000: 00000002 00000000 00010000 0001 01 107 /run/user/1000/bus
0000000000000000: 00000003 00000000 00000000 0001 03 108
`
	got := candidates("unix:/run/user/1000/kitty-4000", table)
	want := []string{"unix:/run/user/1000/kitty-4000", "unix:/run/user/1000/kitty-4002", "unix:@kitty-4001"}
	if !slices.Equal(got, want) {
		t.Errorf("candidates = %q, want %q: the socket this process was started in, then the others", got, want)
	}
	for socket, pid := range map[string]int{
		"unix:/run/user/1000/kitty-4000": 4000,
		"unix:@kitty-4001":               4001,
		"unix:/run/user/1001/kitty-4003": 0,
		"unix:/tmp/mykitty":              0,
	} {
		if got := pidOf(socket); got != pid {
			t.Errorf("pidOf(%q) = %d, want %d", socket, got, pid)
		}
	}
}

// Without a runtime directory no path is the user's own, so no socket file is
// taken.
func TestDiscoveryTakesNoSocketFileWithoutARuntimeDirectory(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "")
	table := "0000000000000000: 00000002 00000000 00010000 0001 01 101 /kitty-4000\n" +
		"0000000000000000: 00000002 00000000 00010000 0001 01 102 kitty-4001\n"
	if got := candidates("", table); len(got) != 0 {
		t.Errorf("candidates = %q, want none", got)
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
