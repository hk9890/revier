//go:build integration

// Layer L3: what Instances costs, against a stub swaymsg on the PATH that
// records every call and answers with a generated tree.
package sway_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/adapter/sway"
)

// One get_tree, whatever the project count.
func TestInstancesCostOneCallWhateverTheProjectCount(t *testing.T) {
	const projects = 90
	var windows []map[string]any
	for i := range projects {
		windows = append(windows, map[string]any{"id": i + 10, "type": "con", "name": "project", "pid": 1000 + i})
	}
	tree, err := json.Marshal(map[string]any{
		"id": 1, "type": "root",
		"nodes": []any{map[string]any{"id": 2, "type": "workspace", "nodes": windows}},
	})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "tree"), tree, 0o644); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(dir, "calls")
	script := "#!/bin/sh\necho \"$*\" >> " + log + "\ncat " + filepath.Join(dir, "tree") + "\n"
	if err := os.WriteFile(filepath.Join(dir, "swaymsg"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	got, err := (&sway.Host{}).Instances(context.Background())
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
	if n := strings.Count(string(calls), "\n"); n != 1 {
		t.Errorf("swaymsg invoked %d times, want 1:\n%s", n, calls)
	}
}
