// Layer L1: the session store is files and names, with no host and no
// substrate behind it.
package session_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/hk9890/revier/internal/session"
)

func at(day int) time.Time { return time.Date(2026, 9, day, 14, 33, 5, 0, time.UTC) }

func sample(day int, name string) session.Session {
	return session.Session{
		Name: name, At: at(day), Current: "revier",
		Projects: []session.Project{{
			Name: "revier",
			Targets: []session.Target{
				{Name: "home", Agents: []session.Agent{
					{Harness: "claude", Session: "abc-123", Dir: "/home/hans/dev/github/revier/.claude/worktrees/tui"},
					{Harness: "claude"},
				}},
				{Name: "editor"},
			},
		}},
	}
}

// A project renamed on the surface is still the one every saved session
// opens again: the sessions are rewritten in place, under their own ids, and
// one that never named the project is left as it was.
func TestRenameMovesAProjectInEverySession(t *testing.T) {
	root := t.TempDir()
	other := sample(11, "other")
	other.Current, other.Projects[0].Name = "setup", "setup"
	for _, s := range []session.Session{sample(12, "before-reboot"), other} {
		if _, _, err := session.Save(root, s); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}
	if err := session.Rename(root, "revier", "rv"); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	renamed, err := session.Load(root, "before-reboot")
	if err != nil {
		t.Fatal(err)
	}
	if renamed.ID != "2026-09-12T14-33-05" || renamed.Current != "rv" || renamed.Projects[0].Name != "rv" {
		t.Errorf("session = %+v, want rv under the same id", renamed)
	}
	if want := sample(12, "").Projects[0].Targets; !slices.EqualFunc(renamed.Projects[0].Targets, want, func(a, b session.Target) bool {
		return a.Name == b.Name && slices.Equal(a.Agents, b.Agents)
	}) {
		t.Errorf("targets = %+v, want them kept", renamed.Projects[0].Targets)
	}
	if kept, _ := session.Load(root, "other"); kept.Current != "setup" || kept.Projects[0].Name != "setup" {
		t.Errorf("other session = %+v, want it left alone", kept)
	}
}

func TestSaveThenLoadRoundTrip(t *testing.T) {
	root := t.TempDir()
	if _, _, err := session.Save(root, sample(12, "before-reboot")); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := session.Load(root, "")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.ID != "2026-09-12T14-33-05" {
		t.Errorf("ID = %q, want the timestamp", got.ID)
	}
	if got.Name != "before-reboot" || got.Current != "revier" {
		t.Errorf("label and current did not survive: %+v", got)
	}
	if len(got.Projects) != 1 || len(got.Projects[0].Targets) != 2 {
		t.Fatalf("projects did not survive: %+v", got.Projects)
	}
	// The agent with nothing to say survives too: it holds its place in the
	// order a restore lays the agents out by.
	want := sample(12, "").Projects[0].Targets[0].Agents
	if agents := got.Projects[0].Targets[0].Agents; !slices.Equal(agents, want) {
		t.Errorf("agents = %+v, want %+v", agents, want)
	}
	if got.Conversations() != 1 {
		t.Errorf("Conversations = %d, want the one with an id", got.Conversations())
	}
	if got.Targets() != 2 {
		t.Errorf("Targets = %d, want 2", got.Targets())
	}
}

// The file is meant to be opened and edited - "restore all of that except
// those three" - so a target is a table a key can be added to, not a bare
// string in an array.
func TestSavedFileIsReadableTOML(t *testing.T) {
	root := t.TempDir()
	stored, path, err := session.Save(root, sample(12, ""))
	if err != nil {
		t.Fatal(err)
	}
	// The id comes back on the session, not only in the file: the caller has
	// to print it and cannot predict a collision suffix.
	if stored.ID != "2026-09-12T14-33-05" {
		t.Errorf("Save returned ID %q, want the id it wrote", stored.ID)
	}
	if filepath.Dir(path) != session.Dir(root) {
		t.Errorf("wrote to %s, want a file under %s", path, session.Dir(root))
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"[[project]]", "[[project.target]]", "[[project.target.agent]]", `dir = "/home/hans/dev/github/revier/.claude/worktrees/tui"`, `name = "home"`} {
		if !contains(string(b), want) {
			t.Errorf("saved file has no %s:\n%s", want, b)
		}
	}
}

// An empty ref means the newest, which is what a restore after a reboot is.
func TestLoadWithoutRefTakesTheNewest(t *testing.T) {
	root := t.TempDir()
	mustSave(t, root, sample(11, "older"))
	mustSave(t, root, sample(13, "newer"))
	mustSave(t, root, sample(12, "middle"))

	got, err := session.Load(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "newer" {
		t.Errorf("Load(\"\") = %q, want the newest", got.Name)
	}
}

func TestLoadByIDAndByName(t *testing.T) {
	root := t.TempDir()
	mustSave(t, root, sample(11, "nightly"))
	mustSave(t, root, sample(13, "nightly"))

	byID, err := session.Load(root, "2026-09-11T14-33-05")
	if err != nil {
		t.Fatal(err)
	}
	if !byID.At.Equal(at(11)) {
		t.Errorf("by id took %v, want the 11th", byID.At)
	}
	// A name several sessions carry resolves to its newest holder.
	byName, err := session.Load(root, "nightly")
	if err != nil {
		t.Fatal(err)
	}
	if !byName.At.Equal(at(13)) {
		t.Errorf("by name took %v, want the newest holder", byName.At)
	}
}

// Two saves in one second are two sessions. Letting the second overwrite the
// first would lose a desktop silently.
func TestSaveInTheSameSecondKeepsBoth(t *testing.T) {
	root := t.TempDir()
	first := mustSave(t, root, sample(12, "one"))
	second := mustSave(t, root, sample(12, "two"))
	// The second id is the first with -2: a user types it to restore, so the
	// scheme is a contract.
	base := session.NewID(at(12))
	if filepath.Base(first) != base+".toml" || filepath.Base(second) != base+"-2.toml" {
		t.Fatalf("saves wrote %s and %s, want %s.toml and %s-2.toml", first, second, base, base)
	}
	all, err := session.List(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Errorf("List = %d sessions, want 2", len(all))
	}
}

// A store that was never written to is a normal outcome, not a failure: the
// first restore on a machine that never saved.
func TestLoadFromEmptyStore(t *testing.T) {
	if _, err := session.Load(t.TempDir(), ""); !errors.Is(err, session.ErrNoSession) {
		t.Errorf("err = %v, want ErrNoSession", err)
	}
	root := t.TempDir()
	mustSave(t, root, sample(12, ""))
	if _, err := session.Load(root, "nothing-by-that-name"); !errors.Is(err, session.ErrNoSession) {
		t.Errorf("err = %v, want ErrNoSession", err)
	}
}

// One hand-edited file must not hide every other session.
func TestListSkipsAFileThatDoesNotParse(t *testing.T) {
	root := t.TempDir()
	mustSave(t, root, sample(12, "good"))
	if err := os.WriteFile(filepath.Join(session.Dir(root), "broken.toml"), []byte("this is not ] toml"), 0o644); err != nil {
		t.Fatal(err)
	}
	all, err := session.List(root)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 1 || all[0].Name != "good" {
		t.Errorf("List = %+v, want the one good session", all)
	}
}

func TestListOfMissingStore(t *testing.T) {
	all, err := session.List(filepath.Join(t.TempDir(), "never-written"))
	if err != nil {
		t.Errorf("List of a missing store: %v", err)
	}
	if all != nil {
		t.Errorf("List = %+v, want none", all)
	}
}

func mustSave(t *testing.T, root string, s session.Session) string {
	t.Helper()
	_, path, err := session.Save(root, s)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	return path
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// A session saved by v0.3.0 recorded its conversations as panels, by position.
// Read after the upgrade, it restores those conversations in panel order
// rather than losing them without a word.
func TestASessionSavedByV030KeepsItsConversations(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(session.Dir(root), 0o755); err != nil {
		t.Fatal(err)
	}
	v030 := `id = "2026-09-01T10-00-00"
at = 2026-09-01T10:00:00Z

[[project]]
name = "revier"

[[project.target]]
name = "home"

[[project.target.panel]]
index = 3
harness = "opencode"
session = "later"

[[project.target.panel]]
index = 1
harness = "claude"
session = "first"
`
	if err := os.WriteFile(filepath.Join(session.Dir(root), "2026-09-01T10-00-00.toml"), []byte(v030), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := session.Load(root, "")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := []session.Agent{{Harness: "claude", Session: "first"}, {Harness: "opencode", Session: "later"}}
	if agents := got.Projects[0].Targets[0].Agents; !slices.Equal(agents, want) {
		t.Errorf("agents = %+v, want %+v", agents, want)
	}
	if got.Conversations() != 2 {
		t.Errorf("Conversations = %d, want both counted", got.Conversations())
	}
}

// Two saves of one desktop are the same session whenever they were made and
// whatever they were called; the survey's project order does not count, and
// the agents' order does, because a restore lays them out by it.
func TestSameAsComparesWhatIsOpen(t *testing.T) {
	a := sample(1, "before")
	b := sample(2, "")
	b.Current = ""
	if !a.SameAs(b) {
		t.Error("two saves of one desktop differ")
	}

	other := session.Project{Name: "other", Targets: []session.Target{{Name: "home"}}}
	a.Projects = append(a.Projects, other)
	b.Projects = append([]session.Project{other}, b.Projects...)
	if !a.SameAs(b) {
		t.Error("the project order made two sessions differ")
	}

	swapped := sample(1, "")
	agents := swapped.Projects[0].Targets[0].Agents
	agents[0], agents[1] = agents[1], agents[0]
	if sample(1, "").SameAs(swapped) {
		t.Error("the agents in another order are the same session")
	}
	moved := sample(1, "")
	moved.Projects[0].Targets[0].Agents[0].Session = "other-id"
	if sample(1, "").SameAs(moved) {
		t.Error("another conversation is the same session")
	}
}
