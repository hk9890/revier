package core_test

import (
	"testing"

	"github.com/hk9890/revier/internal/core"
)

// A link's host is this machine by its own name or the HostName ssh connects
// to, compared by the first label, so dt and dt.local are one machine.
func TestThisMachineIsTheHostnameByItsFirstLabel(t *testing.T) {
	for _, tc := range []struct {
		names []string
		want  bool
	}{
		{[]string{"dt"}, true},
		{[]string{"DT.local"}, true},
		{[]string{"dt.example.com."}, true},
		{[]string{"alias", "dt.local"}, true},
		{[]string{"localhost"}, true},
		{[]string{"alias", "127.0.0.1"}, true},
		{[]string{"buildbox"}, false},
		{[]string{"dt2"}, false},
		{[]string{"buildbox", "10.0.0.7"}, false},
		{nil, false},
	} {
		if got := core.ThisMachine("dt", tc.names...); got != tc.want {
			t.Errorf("ThisMachine(dt, %q) = %v, want %v", tc.names, got, tc.want)
		}
	}
	if core.ThisMachine("", "") {
		t.Error("an unknown hostname matched an empty name")
	}
}
