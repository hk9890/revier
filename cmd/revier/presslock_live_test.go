//go:build live

// Layer L4: concurrent presses from real processes on a private hc core. The
// view is none, so no process reaches a window.
package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"

	"github.com/hk9890/revier/internal/adapter/hc"
)

// Three processes open one closed project at once, and one instance runs.
func TestConcurrentOpensOfAClosedProjectLaunchOneInstance(t *testing.T) {
	scratch(t)
	noView(t)
	bin := filepath.Join(t.TempDir(), "revier")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}

	var wg sync.WaitGroup
	outs, errs := make([][]byte, 3), make([]error, 3)
	for i := range 3 {
		wg.Go(func() {
			cmd := exec.Command(bin, "open", "demo")
			cmd.Env = os.Environ()
			outs[i], errs[i] = cmd.CombinedOutput()
		})
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("process %d: %v\n%s", i, err, outs[i])
		}
	}

	instances, err := (&hc.Host{Socket: liveSocket}).Instances(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	homes := 0
	for _, inst := range instances {
		if inst.Title == "home" {
			homes++
		}
	}
	if homes != 1 {
		t.Errorf("%d home instances of %d listed, want one: %+v", homes, len(instances), instances)
	}
}
