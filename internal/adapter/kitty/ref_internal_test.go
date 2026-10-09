// The instance id is the socket and the OS window id in one string. Raise,
// Close and the ownership check all read it back, so the two directions must
// agree, with no kitty and no recorded output.
package kitty

import "testing"

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
