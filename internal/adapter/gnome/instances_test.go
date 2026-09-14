//go:build integration

// Layer L3: what Instances costs, against a stub wctl that records every call
// and answers with a generated window list.
package gnome_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/adapter/gnome"
)

// At most two calls, whatever the project count: the window list, and the
// active workspace when no shown window tells it. Every window here is hidden
// on another workspace, so the second call is made and every window is kept.
func TestInstancesCostTwoCallsWhateverTheProjectCount(t *testing.T) {
	const projects = 90
	var windows []map[string]any
	for i := range projects {
		windows = append(windows, map[string]any{"id": i + 1, "title": "project", "is_hidden": true, "workspace_index": 1})
	}
	list, err := json.Marshal(windows)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for name, body := range map[string][]byte{"list": list, "workspaces": []byte(`[{"index": 0, "is_active": true}]`)} {
		if err := os.WriteFile(filepath.Join(dir, name), body, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	log := filepath.Join(dir, "calls")
	stub := filepath.Join(dir, "wctl")
	script := "#!/bin/sh\n" +
		"echo \"$*\" >> " + log + "\n" +
		"case \"$1\" in\n" +
		"list) cat " + filepath.Join(dir, "list") + " ;;\n" +
		"workspaces) cat " + filepath.Join(dir, "workspaces") + " ;;\n" +
		"esac\n"
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := (&gnome.Host{Bin: stub}).Instances(context.Background())
	if err != nil {
		t.Fatalf("Instances: %v", err)
	}
	if len(got) != projects {
		t.Fatalf("got %d instances, want %d", len(got), projects)
	}
	calls, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(calls), "\n"); n != 2 {
		t.Errorf("wctl invoked %d times, want 2:\n%s", n, calls)
	}
}
