package record_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/hk9890/revier/internal/record"
	"github.com/hk9890/revier/pkg/revier"
)

// cest is the zone of the recorded fixtures, so a test reads the same day
// files on a machine in any zone.
var cest = time.FixedZone("CEST", 2*60*60)

func at(day, clock string) time.Time {
	t, err := time.ParseInLocation(time.DateTime, day+" "+clock, cest)
	if err != nil {
		panic(err)
	}
	return t
}

// scratch points the process at a record of its own, on a clock the test
// moves, and returns the state root.
func scratch(t *testing.T, clock *time.Time) string {
	t.Helper()
	root := t.TempDir()
	record.Setup(root, "tui")
	record.SetNow(func() time.Time { return *clock })
	t.Cleanup(func() {
		record.Setup("", "")
		record.SetNow(time.Now)
	})
	return root
}

func read(t *testing.T, root string, when time.Time) []record.Line {
	t.Helper()
	lines, err := record.Read(root, when)
	if err != nil {
		t.Fatal(err)
	}
	return lines
}

// fixture is a recorded state root under testdata.
func fixture(name string) string { return filepath.Join("testdata", name) }

// desktop is a survey of this machine: a terminal with the revier
// workspace and its agent, and a browser window of taskmgr on gnome.
func desktop() record.Seen {
	return record.Seen{
		Current:  "revier",
		Projects: []revier.ProjectName{"revier", "taskmgr"},
		Hosts:    []string{"gnome", "kitty"},
		Instances: []record.Instance{
			{Host: "kitty", Ref: "3", Title: "revier"},
			{Host: "gnome", Ref: "0x1a", Title: "taskmgr - Firefox"},
		},
		Targets: []record.Target{
			{Project: "revier", Target: "workspace", Host: "kitty", Ref: "3"},
			{Project: "taskmgr", Target: "browser", Host: "gnome", Ref: "0x1a"},
		},
		Agents: []record.Agent{
			{Project: "revier", Host: "kitty", Ref: "3", Panel: "7", Harness: "claude", Session: "s1", Dir: "/home/hans/dev/github/revier"},
		},
	}
}

// kinds is the kind of each line, in order.
func kinds(lines []record.Line) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = l.Kind
	}
	return out
}

func machineOf(t *testing.T, st record.State, name string) record.Machine {
	t.Helper()
	for _, m := range st.Machines {
		if m.Machine == name {
			return m
		}
	}
	t.Fatalf("machines = %+v, want one named %q", st.Machines, name)
	return record.Machine{}
}
