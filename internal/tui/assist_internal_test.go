package tui

import (
	"errors"
	"testing"
)

// The footer says why `revier assist` failed: the line the command printed,
// which left the screen with the hand-over, and not a bare exit status.
func TestAssistErrIsTheReasonTheCommandGave(t *testing.T) {
	exit := errors.New("exit status 1")
	for _, tc := range []struct {
		name string
		err  error
		said string
		want string
	}{
		{name: "the command ended well", said: "revier: warning: no log\n"},
		{name: "the command said why", err: exit, said: "revier: warning: no log\nrevier: no project named \"gone\"\n", want: `no project named "gone"`},
		{name: "the command said nothing", err: exit, want: "revier assist: exit status 1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := assistErr(tc.err, tc.said)
			if tc.want == "" {
				if got != nil {
					t.Errorf("assistErr = %v, want none", got)
				}
				return
			}
			if got == nil || got.Error() != tc.want {
				t.Errorf("assistErr = %v, want %q", got, tc.want)
			}
		})
	}
}
