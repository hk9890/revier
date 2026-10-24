package herdr_test

import (
	"context"
	"testing"

	"github.com/hk9890/revier/internal/adapter/herdr"
	"github.com/hk9890/revier/pkg/revier"
)

// A herdr pane carries HERDR_ENV and its server's socket. The attach is
// skipped only when that socket is the host's, so a pane of another server is
// still attached from, and so is a shell that only points HERDR_SOCKET_PATH at
// a server.
func TestEnclosesIsAPaneOfTheHostsServer(t *testing.T) {
	for _, tc := range []struct {
		name, hostSocket, envSocket, herdrEnv string
		want                                  bool
	}{
		{"a pane of the host's server", "/tmp/s.sock", "/tmp/s.sock", "1", true},
		{"a pane of the server the environment names", "", "/tmp/s.sock", "1", true},
		{"a pane of another server", "/tmp/s.sock", "/tmp/other.sock", "1", false},
		{"no herdr environment", "/tmp/s.sock", "/tmp/s.sock", "", false},
		{"outside herdr", "/tmp/s.sock", "", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("HERDR_SOCKET_PATH", tc.envSocket)
			t.Setenv("HERDR_ENV", tc.herdrEnv)
			var rt revier.Runtime = &herdr.Host{Socket: tc.hostSocket}
			enc, ok := rt.(revier.Encloser)
			if !ok {
				t.Fatal("herdr.Host does not implement revier.Encloser")
			}
			got, err := enc.Encloses(context.Background())
			if err != nil || got != tc.want {
				t.Errorf("Encloses = %v, %v; want %v", got, err, tc.want)
			}
		})
	}
}
