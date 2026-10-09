//go:build integration

// Layer L3: what Instances costs, against a recorder in place of wctl that
// answers with a generated window list.
package gnome_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
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
	w := &wctl{reply: func(args string) ([]byte, error) {
		switch strings.Fields(args)[0] {
		case "list":
			return list, nil
		case "workspaces":
			return []byte(`[{"index": 0, "is_active": true}]`), nil
		}
		return nil, nil
	}}

	got, err := w.host().Instances(context.Background())
	if err != nil {
		t.Fatalf("Instances: %v", err)
	}
	if len(got) != projects {
		t.Fatalf("got %d instances, want %d", len(got), projects)
	}
	if len(w.calls) != 2 {
		t.Errorf("wctl invoked %d times, want 2: %q", len(w.calls), w.calls)
	}
}
