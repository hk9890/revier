package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/state"
)

// status names the view beside the runtime, and "-" where nothing shows the
// runtime.
func TestStatusNamesTheView(t *testing.T) {
	a, _ := demoDesktop(t, &state.State{})
	var out bytes.Buffer
	a.out = &out
	if err := cmdStatus(t.Context(), a, []string{"-p", "demo"}); err != nil {
		t.Fatal(err)
	}
	if want := "runtime   hc\nview      hc\nwindow    -\n"; !strings.Contains(out.String(), want) {
		t.Errorf("status:\n%s\nwant it to hold:\n%s", out.String(), want)
	}

	out.Reset()
	a.core.View = nil
	if err := cmdStatus(t.Context(), a, []string{"-p", "demo"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "view      -\n") {
		t.Errorf("status:\n%s\nwant no view named", out.String())
	}
}
