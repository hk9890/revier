package tui_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/internal/theme"
	"github.com/hk9890/revier/internal/tui"
	"github.com/hk9890/revier/pkg/revier"
)

// linkWorld is a surface with an ssh configuration naming buildbox and
// farbox, a scratch configuration root to write links into, and a fake
// buildbox that has the named projects.
func linkWorld(t *testing.T, projects []core.Project, onHost ...string) (tui.Model, *hosttest.FakeRemote, string) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("REVIER_CONFIG_HOME", root)
	sshConfig := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(sshConfig, []byte("Host buildbox\n  HostName 10.0.0.7\nHost farbox\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("REVIER_SSH_CONFIG", sshConfig)

	var views []revier.ProjectView
	for _, n := range onHost {
		views = append(views, revier.ProjectView{Project: revier.Project{Name: revier.ProjectName(n), Path: "/home/user/dev/" + n}, PathExists: true})
	}
	remote := hosttest.NewRemote("buildbox", views...)
	c := &core.Core{Runtime: hosttest.NewRuntime("rt"), NewRemote: func(string) revier.Remote { return remote }}
	return resize(refreshed(t, c, projects, stateWith(t, nil), nil), 80, 20), remote, root
}

// step presses a key and runs the command it returns, feeding the message
// back, the way the program does.
func step(m tui.Model, key string) tui.Model {
	m, cmd := press(m, key)
	if cmd != nil {
		if out := cmd(); out != nil {
			next, _ := m.Update(out)
			m = next.(tui.Model)
		}
	}
	return m
}

// alt+r lists the hosts the ssh configuration names; Enter on one asks it
// and shows its projects, with the link here that already points at each.
func TestAltRListsHostsAndEnterListsTheHostsProjects(t *testing.T) {
	m, remote, _ := linkWorld(t, remoteOnDisk(t, "alpha"), "alpha", "beta")

	m = step(m, "alt+r")
	if r := rows(m); len(r) < 2 || !strings.Contains(r[0], "buildbox") || !strings.Contains(r[1], "farbox") {
		t.Fatalf("rows = %q, want the two hosts", r)
	}
	if h := barLine(m); !strings.Contains(h, "Link") {
		t.Errorf("header = %q, want the dialog named", h)
	}

	m = step(m, "enter")
	if last := remote.Asked[len(remote.Asked)-1]; len(last) != 0 {
		t.Errorf("asked %v, want the dialog's ask to be for every project", remote.Asked)
	}
	// Two lines a project, as on the surface: the name, and the path with
	// the note beside it.
	r := rows(m)
	if len(r) < 4 || !strings.Contains(r[0], "alpha") || !strings.Contains(r[1], "linked as alpha") {
		t.Errorf("rows = %q, want alpha marked as linked", r)
	}
	if !strings.Contains(r[2], "beta") || strings.Contains(r[3], "linked") {
		t.Errorf("rows = %q, want beta unlinked", r)
	}
	if h := barLine(m); !strings.Contains(h, "buildbox") {
		t.Errorf("header = %q, want the host named", h)
	}
}

// Enter on an unlinked project asks for the link's name, with
// rs-<host>-<project> offered; Enter on that writes the link and returns to
// the list, with the new row selected and shown as name@host.
func TestEnterOnAHostsProjectWritesTheLinkUnderTheOfferedName(t *testing.T) {
	m, _, root := linkWorld(t, nil, "beta")

	m = step(m, "alt+r")
	m = step(m, "enter")
	m = step(m, "enter")
	if q := query(m); !strings.Contains(q, "rs-buildbox-beta") {
		t.Fatalf("field = %q, want the offered name", q)
	}
	m = step(m, "enter")

	body, err := os.ReadFile(config.ProjectFile(root, "rs-buildbox-beta"))
	if err != nil || !strings.Contains(string(body), `host = "buildbox"`) || !strings.Contains(string(body), `project = "beta"`) {
		t.Fatalf("file = %q, %v; want the link to beta written", body, err)
	}
	if row := selectedRow(t, m); !strings.Contains(row, "rs-buildbox-beta@buildbox") {
		t.Errorf("selected %q, want the new link", row)
	}
	if f := footer(m); strings.Contains(f, "already") {
		t.Errorf("footer = %q, want no refusal", f)
	}
}

// The row a link gets before the first survey answers counts no agent. The
// host lists every agent in its project, nothing here holds the link yet, and
// a row that counts one is a row drawn as closed with an agent Enter cannot
// reach (decisions.md D104).
func TestANewLinksRowCountsNoAgentUntilAPanelHereShowsOne(t *testing.T) {
	m, remote, _ := linkWorld(t, nil, "beta")
	remote.Views[0].Agents = []revier.AgentView{
		{Panel: "box.4242", State: revier.AgentState{Harness: "claude", Status: revier.StatusAttention}}}

	m = step(m, "alt+r")
	m = step(m, "enter")
	m = step(m, "enter")
	m = step(m, "enter")

	row := selectedRow(t, m)
	if !strings.Contains(row, "rs-buildbox-beta@buildbox") {
		t.Fatalf("selected %q, want the new link", row)
	}
	if strings.Contains(row, theme.Default().Glyphs.NeedsYou) {
		t.Errorf("row = %q, want no agent: no panel here shows the host's", row)
	}
}

// The first character typed replaces the offered name, and a name a project
// here already has is said as it is typed. Enter on it writes nothing, so no
// project here is overwritten.
func TestATakenLinkNameIsSaidWhileTypedAndNotWritten(t *testing.T) {
	m, _, root := linkWorld(t, remoteOnDisk(t, "alpha"), "beta")
	m = step(m, "alt+r")
	m = step(m, "enter")
	m = step(m, "enter")

	m, _ = press(m, "alph")
	if q := query(m); !strings.Contains(q, "alph") || strings.Contains(q, "rs-") {
		t.Fatalf("field = %q, want the offered name replaced", q)
	}
	if body := strings.Join(rows(m), "\n"); strings.Contains(body, "exists here") {
		t.Errorf("rows = %q, want a free name not warned about", rows(m))
	}
	m, _ = press(m, "a")
	if body := strings.Join(rows(m), "\n"); !strings.Contains(body, `"alpha" exists here`) {
		t.Errorf("rows = %q, want the taken name said", rows(m))
	}

	m = step(m, "enter")
	if h := barLine(m); !strings.Contains(h, "Name the link") {
		t.Errorf("header = %q, want the name step still up", h)
	}
	files, _ := filepath.Glob(filepath.Join(root, "projects", "*.toml"))
	if len(files) != 0 {
		t.Errorf("a refused name wrote %v", files)
	}

	m, _ = press(m, "esc")
	if f := footer(m); strings.Contains(f, "exists here") {
		t.Errorf("footer = %q, want the refusal left with the name step", f)
	}
}

// Esc on the name goes back to the host's projects, with the cursor on the
// project it left.
func TestEscOnTheLinkNameGoesBackToTheHostsProjects(t *testing.T) {
	m, _, _ := linkWorld(t, nil, "beta", "gamma")
	m = step(m, "alt+r")
	m = step(m, "enter")
	m, _ = press(m, "down")
	m = step(m, "enter")
	m, _ = press(m, "esc")
	if h := barLine(m); !strings.Contains(h, "buildbox") || strings.Contains(h, "Name the link") {
		t.Errorf("header = %q, want the host's projects", h)
	}
	if row := selectedRow(t, m); !strings.Contains(row, "gamma") {
		t.Errorf("selected %q, want gamma still under the cursor", row)
	}
}

// What is typed on a host's projects filters them, and Enter links the one
// the filter left under the cursor. Esc clears the query, with the cursor
// back where it was, before it goes back to the hosts (decisions.md D44).
func TestTypingFiltersTheHostsProjects(t *testing.T) {
	m, _, _ := linkWorld(t, nil, "alpha", "beta", "gamma")
	m = step(m, "alt+r")
	m = step(m, "enter")
	m, _ = press(m, "down")

	m = typeInto(m, "gam")
	if q := query(m); !strings.Contains(q, "gam") {
		t.Errorf("field = %q, want the query", q)
	}
	if r := ruleLine(m); !strings.Contains(r, "1/3 projects") {
		t.Errorf("rule = %q, want the filtered count", r)
	}
	if body := strings.Join(rows(m), "\n"); !strings.Contains(body, "gamma") || strings.Contains(body, "alpha") || strings.Contains(body, "beta") {
		t.Errorf("rows = %q, want gamma alone", rows(m))
	}

	m = step(m, "enter")
	if q := query(m); !strings.Contains(q, "rs-buildbox-gamma") {
		t.Fatalf("field = %q, want gamma offered", q)
	}
	m, _ = press(m, "esc")
	if r := ruleLine(m); !strings.Contains(r, "1/3 projects") {
		t.Errorf("rule = %q, want the query kept under the name step", r)
	}

	m, _ = press(m, "esc")
	if h := barLine(m); !strings.Contains(h, "buildbox") || strings.Contains(h, "Link") {
		t.Errorf("header = %q, want the host's projects still up", h)
	}
	if r := ruleLine(m); !strings.Contains(r, "3 projects") || strings.Contains(r, "/") {
		t.Errorf("rule = %q, want every project again", r)
	}
	if row := selectedRow(t, m); !strings.Contains(row, "beta") {
		t.Errorf("selected %q, want the cursor back on beta", row)
	}

	m, _ = press(m, "esc")
	if r := rows(m); len(r) < 1 || !strings.Contains(r[0], "buildbox") {
		t.Errorf("rows = %q, want the hosts again", r)
	}
}

// A link written from a query leaves no query behind: the next time the
// dialog opens, the first Esc on the hosts closes it.
func TestEscClosesTheHostsAfterALinkWrittenFromAQuery(t *testing.T) {
	m, _, _ := linkWorld(t, nil, "alpha", "beta")
	m = step(m, "alt+r")
	m = step(m, "enter")
	m = typeInto(m, "bet")
	m = step(m, "enter")
	m = step(m, "enter")

	m = step(m, "alt+r")
	if h := barLine(m); !strings.Contains(h, "Link") {
		t.Fatalf("header = %q, want the hosts up", h)
	}
	m, _ = press(m, "esc")
	if h := barLine(m); strings.Contains(h, "Link") {
		t.Errorf("header = %q, want the dialog closed by the first esc", h)
	}
}

// A query matching none of the host's projects says so.
func TestAQueryMatchingNoneOfTheHostsProjectsSaysSo(t *testing.T) {
	m, _, _ := linkWorld(t, nil, "alpha")
	m = step(m, "alt+r")
	m = step(m, "enter")
	m = typeInto(m, "zz")
	if body := strings.Join(rows(m), "\n"); !strings.Contains(body, `No project on buildbox matches "zz"`) {
		t.Errorf("rows = %q, want the empty match said", rows(m))
	}
}

// A project whose checkout is missing on the host shows the missing-folder
// glyph, and its row says nothing else about it: the table's right column is
// agent state only.
func TestAHostsMissingCheckoutShowsTheMissingFolder(t *testing.T) {
	m, remote, _ := linkWorld(t, nil)
	remote.Views = []revier.ProjectView{{Project: revier.Project{Name: "delta", Path: "/home/someone/dev/delta"}}}
	m = step(m, "alt+r")
	m = step(m, "enter")
	r := rows(m)
	if len(r) < 2 || !strings.Contains(r[0], theme.Default().Glyphs.NoFolder) || strings.Contains(r[1], "not on") {
		t.Errorf("rows = %q, want the missing-folder glyph and no note", r)
	}
}

// A path on the host is written against the host's home, as a path here is
// against this one's.
func TestAHostsPathIsWrittenAgainstItsHome(t *testing.T) {
	m, remote, _ := linkWorld(t, nil)
	remote.Views = []revier.ProjectView{{Project: revier.Project{Name: "delta", Path: "/home/someone/dev/delta"}, PathExists: true}}
	m = step(m, "alt+r")
	m = step(m, "enter")
	if r := rows(m); len(r) < 2 || !strings.Contains(r[1], "~/dev/delta") {
		t.Errorf("rows = %q, want the path under ~", r)
	}
}

// A project already linked is refused, not written twice.
func TestEnterOnALinkedProjectIsRefused(t *testing.T) {
	m, _, root := linkWorld(t, remoteOnDisk(t, "alpha"), "alpha")
	m = step(m, "alt+r")
	m = step(m, "enter")
	m = step(m, "enter")
	if f := footer(m); !strings.Contains(f, "already linked as alpha") {
		t.Errorf("footer = %q, want the existing link named", f)
	}
	if _, err := os.Stat(config.ProjectFile(root, "alpha")); err == nil {
		t.Error("a refused link wrote a file")
	}
}

// Esc walks back a level at a time: the host's projects, the hosts, the
// list.
func TestEscWalksBackThroughTheDialog(t *testing.T) {
	m, _, _ := linkWorld(t, nil, "beta")
	m = step(m, "alt+r")
	m = step(m, "enter")
	m, _ = press(m, "esc")
	if r := rows(m); len(r) < 1 || !strings.Contains(r[0], "buildbox") {
		t.Errorf("rows = %q, want the hosts again", r)
	}
	m, _ = press(m, "esc")
	if h := barLine(m); strings.Contains(h, "Link") {
		t.Errorf("header = %q, want the project list", h)
	}
}

// A host that does not answer is said in the footer, and the hosts stay
// on screen for another choice.
func TestAHostThatDoesNotAnswerIsSaidInTheFooter(t *testing.T) {
	m, remote, _ := linkWorld(t, nil)
	remote.Err = errors.New("buildbox: connection refused")
	m = step(m, "alt+r")
	m = step(m, "enter")
	if f := footer(m); !strings.Contains(f, "connection refused") {
		t.Errorf("footer = %q, want the failure", f)
	}
	if r := rows(m); len(r) < 1 || !strings.Contains(r[0], "buildbox") {
		t.Errorf("rows = %q, want the hosts still", r)
	}
}

// With no hosts configured there is nothing to choose from, and the footer
// says where a host would come from.
func TestAltRWithNoHostsSaysSo(t *testing.T) {
	m, _, _ := linkWorld(t, nil)
	t.Setenv("REVIER_SSH_CONFIG", filepath.Join(t.TempDir(), "none"))
	m = step(m, "alt+r")
	if f := footer(m); !strings.Contains(f, "no hosts") {
		t.Errorf("footer = %q, want no hosts said", f)
	}
}

// Esc while a host is being asked abandons the ask: the answer that lands
// afterwards must not pull the surface back into the dialog, nor leave the
// project list under an "asking" footer.
func TestEscDuringAnAskAbandonsIt(t *testing.T) {
	m, _, _ := linkWorld(t, nil, "beta")
	m = step(m, "alt+r")
	m, cmd := press(m, "enter")
	if cmd == nil {
		t.Fatal("Enter on a host started no ask")
	}
	m, _ = press(m, "esc")
	if f := footer(m); strings.Contains(f, "asking") {
		t.Errorf("footer = %q, want the project list's own legend", f)
	}
	next, _ := m.Update(cmd())
	m = next.(tui.Model)
	if h := barLine(m); strings.Contains(h, "buildbox") {
		t.Errorf("header = %q, want the project list, not the answered host", h)
	}
}

// A survey started before a link was written answers from the list it began
// with. The new row stays, and the cursor with it, rather than going off the
// screen until the next refresh.
func TestALinkSurvivesASurveyThatPredatesIt(t *testing.T) {
	m, _, _ := linkWorld(t, nil, "beta")
	stale := m.Survey()

	m = step(m, "alt+r")
	m = step(m, "enter")
	m = step(m, "enter")
	m = step(m, "enter")

	next, _ := m.Update(stale())
	m = next.(tui.Model)
	if row := selectedRow(t, m); !strings.Contains(row, "beta") {
		t.Errorf("selected %q, want the new link still on the cursor", row)
	}
}

// The dialog stands over the surface and owns the keys: a letter does not
// filter the list behind it, and Tab does not move a cursor that is not on
// the surface (decisions.md D45).
func TestTheDialogOwnsTheKeys(t *testing.T) {
	m, _, _ := linkWorld(t, remoteOnDisk(t, "alpha"), "alpha")
	m = step(m, "alt+r")

	m, _ = press(m, "a")
	m, _ = press(m, "tab")
	if r := rows(m); len(r) < 2 || !strings.Contains(r[0], "buildbox") || !strings.Contains(r[1], "farbox") {
		t.Errorf("rows = %q, want the hosts untouched", r)
	}
	if f := footer(m); !strings.Contains(f, "list its projects") {
		t.Errorf("footer = %q, want the dialog's own legend", f)
	}
}
