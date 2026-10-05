package events_test

import (
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hk9890/revier/internal/events"
	"github.com/hk9890/revier/pkg/revier"
)

// scratch points the process at an event file of its own, on a clock the test
// moves, and returns the state root.
func scratch(t *testing.T, at *time.Time) string {
	t.Helper()
	root := t.TempDir()
	events.Setup(root)
	events.SetNow(func() time.Time { return *at })
	t.Cleanup(func() {
		events.Setup("")
		events.SetNow(time.Now)
	})
	return root
}

func read(t *testing.T, root string, since time.Time) []revier.Event {
	t.Helper()
	got, err := events.Read(root, since)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestARecordedEventIsReadBackWithItsTime(t *testing.T) {
	at := time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)
	root := scratch(t, &at)

	events.Record(revier.Event{Kind: revier.EventGo, Project: "demo", Target: "editor", Launched: true})

	got := read(t, root, time.Time{})
	want := revier.Event{Time: at, Kind: revier.EventGo, Project: "demo", Target: "editor", Launched: true}
	if len(got) != 1 || got[0] != want {
		t.Errorf("events = %+v, want %+v", got, want)
	}
}

// The line is the contract `revier events` prints: these names, and no field
// an event does not carry.
func TestTheLineNamesItsFieldsAndOmitsTheEmptyOnes(t *testing.T) {
	at := time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)
	root := scratch(t, &at)

	events.Record(revier.Event{Kind: revier.EventAction, Project: "demo", Action: "sync"})

	line, err := os.ReadFile(events.Path(root))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"time":"2026-10-05T09:00:00Z","event":"action","project":"demo","action":"sync"}` + "\n"
	if string(line) != want {
		t.Errorf("line = %s, want %s", line, want)
	}
}

func TestReadLeavesOutWhatIsOlderThanSince(t *testing.T) {
	at := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	root := scratch(t, &at)
	events.Record(revier.Event{Kind: revier.EventGo, Project: "old"})
	at = at.AddDate(0, 0, 3)
	events.Record(revier.Event{Kind: revier.EventGo, Project: "new"})

	got := read(t, root, at.Add(-time.Hour))
	if len(got) != 1 || got[0].Project != "new" {
		t.Errorf("events = %+v, want new alone", got)
	}
}

func TestNoFileIsNoEvents(t *testing.T) {
	if got := read(t, t.TempDir(), time.Time{}); len(got) != 0 {
		t.Errorf("events = %+v, want none", got)
	}
}

// The file is appended to by every process for years: a line cut short by a
// full disk must not hide the events around it.
func TestALineThatIsNotAnEventIsSkipped(t *testing.T) {
	at := time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)
	root := scratch(t, &at)
	events.Record(revier.Event{Kind: revier.EventGo, Project: "a"})
	f, err := os.OpenFile(events.Path(root), os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("{\"time\":\"2026-10\n\n"); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	events.Record(revier.Event{Kind: revier.EventGo, Project: "b"})

	got := read(t, root, time.Time{})
	if len(got) != 2 || got[0].Project != "a" || got[1].Project != "b" {
		t.Errorf("events = %+v, want a and b", got)
	}
}

// A process that never called Setup records nothing, and fails nothing.
func TestWithoutSetupNothingIsRecorded(t *testing.T) {
	t.Chdir(t.TempDir())
	events.Setup("")

	events.Record(revier.Event{Kind: revier.EventGo, Project: "demo"})
	events.Sessions([]revier.ProjectView{agent("demo", "/src/demo", "abc", "")})

	if entries, err := os.ReadDir("."); err != nil || len(entries) != 0 {
		t.Errorf("wrote %v (%v), want nothing", entries, err)
	}
}

func TestConcurrentRecordsKeepEveryLineWhole(t *testing.T) {
	at := time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)
	root := scratch(t, &at)
	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() {
			events.Record(revier.Event{Kind: revier.EventGo, Project: "demo", Dir: strings.Repeat("x", 500)})
		})
	}
	wg.Wait()
	if got := read(t, root, time.Time{}); len(got) != 50 {
		t.Errorf("read %d events, want 50", len(got))
	}
}

func agent(project revier.ProjectName, path string, session revier.SessionID, dir string) revier.ProjectView {
	return revier.ProjectView{
		Project: revier.Project{Name: project, Path: path},
		Agents:  []revier.AgentView{{Panel: "1", State: revier.AgentState{Harness: "claude", Session: session, Dir: dir}}},
	}
}

// A survey runs every second: the conversation it sees each time is one event.
func TestASessionSeenEveryRefreshIsRecordedOnce(t *testing.T) {
	at := time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)
	first := at
	root := scratch(t, &at)
	views := []revier.ProjectView{agent("demo", "/src/demo", "abc", "")}

	events.Sessions(views)
	at = at.Add(time.Second)
	events.Sessions(views)

	got := read(t, root, time.Time{})
	want := revier.Event{Time: first, Kind: revier.EventAgentSession, Project: "demo", Agent: "claude", Session: "abc", Dir: "/src/demo"}
	if len(got) != 1 || got[0] != want {
		t.Errorf("events = %+v, want one %+v", got, want)
	}
}

// A conversation that lives for a week is in a read of the last day.
func TestASessionStillSeenOnALaterDayIsRecordedAgain(t *testing.T) {
	at := time.Date(2026, 10, 5, 23, 59, 0, 0, time.UTC)
	root := scratch(t, &at)
	views := []revier.ProjectView{agent("demo", "/src/demo", "abc", "")}

	events.Sessions(views)
	at = at.Add(2 * time.Minute)
	events.Sessions(views)

	if got := read(t, root, time.Time{}); len(got) != 2 {
		t.Errorf("events = %+v, want one for each day", got)
	}
}

func TestANewConversationInTheSamePanelIsItsOwnEvent(t *testing.T) {
	at := time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)
	root := scratch(t, &at)

	events.Sessions([]revier.ProjectView{agent("demo", "/src/demo", "abc", "")})
	events.Sessions([]revier.ProjectView{agent("demo", "/src/demo", "def", "")})

	got := read(t, root, time.Time{})
	if len(got) != 2 || got[0].Session != "abc" || got[1].Session != "def" {
		t.Errorf("events = %+v, want abc then def", got)
	}
}

func TestAnAgentThatNamesNoConversationIsNotRecorded(t *testing.T) {
	at := time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)
	root := scratch(t, &at)

	events.Sessions([]revier.ProjectView{agent("demo", "/src/demo", "", "")})

	if got := read(t, root, time.Time{}); len(got) != 0 {
		t.Errorf("events = %+v, want none", got)
	}
}

// An agent in a worktree is found by its own directory. A link's agent has no
// directory here, and the path of the link is one on its host.
func TestTheDirectoryIsTheAgentsOwnElseTheLocalProjects(t *testing.T) {
	at := time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)
	root := scratch(t, &at)
	far := agent("far", "~/dev/far", "ghi", "")
	far.Project.Remote = &revier.Link{Host: "buildbox", Project: "far"}

	events.Sessions([]revier.ProjectView{agent("demo", "/src/demo", "abc", "/src/demo/wt"), far})

	got := read(t, root, time.Time{})
	if len(got) != 2 || got[0].Dir != "/src/demo/wt" || got[1].Dir != "" {
		t.Errorf("events = %+v, want the worktree and no directory for the link", got)
	}
}
