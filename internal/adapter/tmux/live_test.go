//go:build live

// Layer L4: a real tmux server, headless.
//
// Every test here runs against a private server on its own socket (`tmux -L`),
// started and killed by the test. Nothing attaches a client, nothing reaches a
// display, and the user's own tmux sessions are never visible to it. This is
// the layer that proves an adapter actually drives its tool, without which L2
// only proves the core is self-consistent.
package tmux_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hk9890/revier/internal/adapter/tmux"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/pkg/revier"
)

// server returns a Host on a socket private to this test, and kills the server
// when the test ends.
func server(t *testing.T) *tmux.Host {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed; L4 needs it")
	}
	socket := "revier-test-" + t.Name()
	h := &tmux.Host{Socket: socket, Session: "revier-test"}
	t.Cleanup(func() {
		_ = exec.Command("tmux", "-L", socket, "kill-server").Run()
	})
	return h
}

func ctx(t *testing.T) context.Context {
	c, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	return c
}

func TestProbeFindsTmux(t *testing.T) {
	if err := server(t).Probe(ctx(t)); err != nil {
		t.Fatalf("Probe: %v", err)
	}
}

// A server with nothing on it is an empty list, not an error: the survey has
// to render on a machine where nothing has been opened yet.
func TestInstancesOnAnEmptyServer(t *testing.T) {
	got, err := server(t).Instances(ctx(t))
	if err != nil {
		t.Fatalf("Instances: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d instances, want 0", len(got))
	}
}

// The invariant every host owes the core: what Open creates, Match finds.
func TestOpenThenMatchFindsIt(t *testing.T) {
	h, c := server(t), ctx(t)
	real := revier.Realization{
		Name:   "diff",
		Launch: []string{"sh", "-c", "sleep 30"},
		Match:  revier.Match{Title: "^diff$"},
	}

	ref, err := h.Open(c, real)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if ref.Host != "tmux" || ref.ID == "" {
		t.Fatalf("ref = %+v, want a tmux ref with an id", ref)
	}

	instances, err := h.Instances(c)
	if err != nil {
		t.Fatalf("Instances: %v", err)
	}
	m, err := real.Match.Compile()
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	for _, inst := range instances {
		if m.Matches(inst) {
			return
		}
	}
	t.Fatalf("no instance matched %q; got %+v", real.Match.Title, instances)
}

func TestFocusAndFocusedRoundTrip(t *testing.T) {
	h, c := server(t), ctx(t)
	first, err := h.Open(c, revier.Realization{
		Name: "home", Launch: []string{"sh", "-c", "sleep 30"},
		Match: revier.Match{Title: "^home$"},
	})
	if err != nil {
		t.Fatalf("Open home: %v", err)
	}
	second, err := h.Open(c, revier.Realization{
		Name: "diff", Launch: []string{"sh", "-c", "sleep 30"},
		Match: revier.Match{Title: "^diff$"},
	})
	if err != nil {
		t.Fatalf("Open diff: %v", err)
	}

	for _, want := range []revier.TargetRef{first, second, first} {
		if err := h.Focus(c, want); err != nil {
			t.Fatalf("Focus %s: %v", want.ID, err)
		}
		got, err := h.Focused(c)
		if err != nil {
			t.Fatalf("Focused: %v", err)
		}
		if got.ID != want.ID {
			t.Fatalf("Focused = %s, want %s", got.ID, want.ID)
		}
	}
}

// tmux writes every non-ASCII character as '_' when LANG and LC_* name no
// UTF-8 locale - cron, a container, ssh without locale forwarding - unless
// it is told its output may carry UTF-8. A project name and the glyph a
// Claude title leads with have to survive that.
func TestNonASCIISurvivesALocaleWithoutUTF8(t *testing.T) {
	for _, name := range []string{"LANG", "LC_ALL", "LC_CTYPE"} {
		t.Setenv(name, "C")
	}
	t.Setenv("TMUX", "") // restored afterwards; unset below, as outside tmux
	_ = os.Unsetenv("TMUX")
	h, c := server(t), ctx(t)
	real := revier.Realization{
		Name:   "session:münchen",
		Panels: []revier.PanelSpec{{Title: "⠧ Working", Command: []string{"sh", "-c", "sleep 30"}}},
		Match:  revier.Match{Title: "^session:münchen$"},
	}
	if _, err := h.Open(c, real); err != nil {
		t.Fatalf("Open: %v", err)
	}
	instances, err := h.Instances(c)
	if err != nil {
		t.Fatalf("Instances: %v", err)
	}
	if len(instances) != 1 || instances[0].Title != "session:münchen" {
		t.Fatalf("instances = %+v, want the window listed as session:münchen", instances)
	}
	if got := instances[0].Panels[0].Title; got != "⠧ Working" {
		t.Errorf("pane title = %q, want the spinner glyph kept", got)
	}
}

// A window linked into a second session - a session group, link-window - is
// listed by list-panes -a once per session. Its panes are still one set:
// counted twice, every agent in them would be two.
func TestALinkedWindowListsItsPanesOnce(t *testing.T) {
	h, c := server(t), ctx(t)
	if _, err := h.Open(c, revier.Realization{
		Name: "home", Launch: []string{"sh", "-c", "sleep 30"},
		Match: revier.Match{Title: "^home$"},
	}); err != nil {
		t.Fatalf("Open: %v", err)
	}
	socket := "revier-test-" + t.Name()
	if out, err := exec.Command("tmux", "-L", socket, "new-session", "-d", "-t", "revier-test", "-s", "second").CombinedOutput(); err != nil {
		t.Fatalf("new-session -t: %v\n%s", err, out)
	}
	instances, err := h.Instances(c)
	if err != nil {
		t.Fatalf("Instances: %v", err)
	}
	if len(instances) != 1 || len(instances[0].Panels) != 1 {
		t.Fatalf("instances = %+v, want one window with one pane", instances)
	}
}

func TestInstancesReportPanels(t *testing.T) {
	h, c := server(t), ctx(t)
	if _, err := h.Open(c, revier.Realization{
		Name: "home", Launch: []string{"sh", "-c", "sleep 30"},
		Match: revier.Match{Title: "^home$"},
	}); err != nil {
		t.Fatalf("Open: %v", err)
	}

	instances, err := h.Instances(c)
	if err != nil {
		t.Fatalf("Instances: %v", err)
	}
	if len(instances) != 1 {
		t.Fatalf("got %d instances, want 1", len(instances))
	}
	if len(instances[0].Panels) == 0 {
		t.Fatal("want at least one panel: a probe reads nothing else")
	}
	if instances[0].Panels[0].ID == "" {
		t.Error("panel has no id")
	}
}

// The whole product against a real substrate: run-or-raise opens the target the
// first time, raises it the second, and toggles home on the third.
func TestCoreRunOrRaiseAgainstRealTmux(t *testing.T) {
	h, c := server(t), ctx(t)
	cr := &core.Core{Runtime: h}
	p := revier.Project{
		Name: "revier",
		Targets: []revier.Target{
			{Name: "home", Home: true, Runtime: &revier.Realization{
				Name: "home", Launch: []string{"sh", "-c", "sleep 30"},
				Match: revier.Match{Title: "^home$"},
			}},
			{Name: "diff", Key: "ctrl-shift-d", Runtime: &revier.Realization{
				Name: "diff", Launch: []string{"sh", "-c", "sleep 30"},
				Match: revier.Match{Title: "^diff$"},
			}},
		},
	}

	prepared, err := core.PrepareProject(p)
	if err != nil {
		t.Fatalf("PrepareProject: %v", err)
	}
	homeRef, err := cr.Go(c, prepared, "home", nil)
	if err != nil {
		t.Fatalf("open home: %v", err)
	}
	diffRef, err := cr.Go(c, prepared, "diff", nil)
	if err != nil {
		t.Fatalf("open diff: %v", err)
	}
	if diffRef.Ref.ID == homeRef.Ref.ID {
		t.Fatal("diff and home must be different windows")
	}

	// Opening left diff focused, so the second press is the round trip home.
	back, err := cr.Go(c, prepared, "diff", nil)
	if err != nil {
		t.Fatalf("toggle back: %v", err)
	}
	if back.Ref.ID != homeRef.Ref.ID {
		t.Fatalf("toggle-back returned %s, want home %s", back.Ref.ID, homeRef.Ref.ID)
	}

	// Third press raises the existing window rather than opening a duplicate.
	again, err := cr.Go(c, prepared, "diff", nil)
	if err != nil {
		t.Fatalf("raise diff: %v", err)
	}
	if again.Ref.ID != diffRef.Ref.ID {
		t.Errorf("raise returned %s, want the existing %s", again.Ref.ID, diffRef.Ref.ID)
	}
	instances, err := h.Instances(c)
	if err != nil {
		t.Fatalf("Instances: %v", err)
	}
	if len(instances) != 2 {
		t.Fatalf("got %d windows, want 2: run-or-raise must not duplicate", len(instances))
	}
}

// tmux escapes non-printable bytes in format output on some versions and not
// others: 3.4 renders a raw \x1f as the literal text "\037" while 3.7 emits the
// byte. A control-character delimiter therefore parsed on the author's machine
// and failed in CI. The separator is printable now, and free text is the last
// field of its query so it may contain the separator itself.
func TestFreeTextContainingTheSeparatorSurvives(t *testing.T) {
	h, c := server(t), ctx(t)
	if _, err := h.Open(c, revier.Realization{
		Name: "a|b", Launch: []string{"sh", "-c", "sleep 30"},
		Match: revier.Match{Title: `^a\|b$`},
	}); err != nil {
		t.Fatalf("Open: %v", err)
	}

	instances, err := h.Instances(c)
	if err != nil {
		t.Fatalf("Instances: %v", err)
	}
	if len(instances) != 1 {
		t.Fatalf("got %d instances, want 1", len(instances))
	}
	if instances[0].Title != "a|b" {
		t.Errorf("window title = %q, want %q", instances[0].Title, "a|b")
	}

	// A pane title carrying the separator must arrive byte-exact: the Claude
	// probe reads it as the agent's activity line.
	title := "⠧ Working on a|b now"
	if _, err := h.Focused(c); err != nil {
		t.Fatalf("Focused: %v", err)
	}
	if err := exec.Command("tmux", "-L", h.Socket, "select-pane",
		"-t", instances[0].Panels[0].ID.String(), "-T", title).Run(); err != nil {
		t.Fatalf("set pane title: %v", err)
	}
	again, err := h.Instances(c)
	if err != nil {
		t.Fatalf("Instances: %v", err)
	}
	if got := again[0].Panels[0].Title; got != title {
		t.Errorf("pane title = %q, want %q", got, title)
	}
}

// A home target is its panels: each becomes a pane of the one window, in the
// project directory, with its title and its command, and the probe can tell
// the agent pane from the shell beside it.
func TestOpenBuildsThePanels(t *testing.T) {
	h, c := server(t), ctx(t)
	dir := t.TempDir()
	ref, err := h.Open(c, revier.Realization{
		Name: "session:demo", Dir: dir, Match: revier.Match{Title: "^session:demo$"},
		Panels: []revier.PanelSpec{
			{Kind: revier.PanelAgent, Title: "agent", Command: []string{"sh", "-c", "sleep 30"}},
			{Kind: revier.PanelShell, Title: "shell", Command: []string{"sh", "-c", "sleep 30"}},
		},
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	instances, err := h.Instances(c)
	if err != nil {
		t.Fatalf("Instances: %v", err)
	}
	if len(instances) != 1 || instances[0].Ref.ID != ref.ID {
		t.Fatalf("instances = %+v, want the one window opened", instances)
	}
	panels := instances[0].Panels
	if len(panels) != 2 {
		t.Fatalf("got %d panels, want 2: one pane per panel spec", len(panels))
	}
	if panels[0].Title != "agent" || panels[1].Title != "shell" {
		t.Errorf("titles = %q, %q", panels[0].Title, panels[1].Title)
	}
	out, err := exec.Command("tmux", "-L", h.Socket, "display-message", "-p", "-t", panels[1].ID.String(), "#{pane_current_path}").Output()
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(out)); got != dir {
		t.Errorf("pane cwd = %q, want the realization's dir %q", got, dir)
	}
}

// Text arrives in the pane as typed, whatever it holds: a leading dash that
// send-keys would read as a flag, a word that is a tmux key name, quotes, a
// backslash, the format separator. The "\r" sent after it is the Enter that
// submits it.
func TestSendTextTypesIntoThePane(t *testing.T) {
	h, c := server(t), ctx(t)
	out := filepath.Join(t.TempDir(), "typed")
	if _, err := h.Open(c, revier.Realization{
		Name: "agent", Match: revier.Match{Title: "^agent$"},
		Launch: []string{"sh", "-c", `IFS= read -r line; printf '%s' "$line" > "$0"; sleep 30`, out},
	}); err != nil {
		t.Fatalf("Open: %v", err)
	}
	instances, err := h.Instances(c)
	if err != nil || len(instances) != 1 {
		t.Fatalf("Instances = %+v, %v", instances, err)
	}
	inst := instances[0]

	var w revier.PanelWriter = h
	text := `-t Enter "it's" a\b|c`
	for _, s := range []string{text, "\r"} {
		if err := w.SendText(c, inst.Ref, inst.Panels[0].ID, s); err != nil {
			t.Fatalf("SendText %q: %v", s, err)
		}
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		got, err := os.ReadFile(out)
		if err == nil && len(got) > 0 {
			if string(got) != text {
				t.Fatalf("pane read %q, want %q", got, text)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the pane read nothing within 5s: the Enter did not submit the line")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// tmux expands -n, -c and -T as formats. A '#' in a project name, a pane
// title or a directory has to arrive as written: "C#" otherwise names the
// window "C", which the project's match never finds, and a directory with a
// '#' starts the pane in $HOME. tmux keeps a run of '#' before '[' as it is,
// so that case is here as well.
func TestHashSurvivesNameTitleAndDirectory(t *testing.T) {
	h, c := server(t), ctx(t)
	for i, name := range []string{"session:C#", "a#{b}", "x#[y]", "p##[q", "##", "e#"} {
		dir := filepath.Join(t.TempDir(), name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		ref, err := h.Open(c, revier.Realization{
			Name: name, Dir: dir,
			Panels: []revier.PanelSpec{{Title: name, Command: []string{"sh", "-c", "sleep 30"}}},
		})
		if err != nil {
			t.Fatalf("Open %q: %v", name, err)
		}
		instances, err := h.Instances(c)
		if err != nil {
			t.Fatalf("Instances: %v", err)
		}
		if len(instances) != i+1 {
			t.Fatalf("got %d instances, want %d", len(instances), i+1)
		}
		var inst revier.Instance
		for _, in := range instances {
			if in.Ref.ID == ref.ID {
				inst = in
			}
		}
		if inst.Title != name {
			t.Errorf("window name = %q, want %q", inst.Title, name)
		}
		if got := inst.Panels[0].Title; got != name {
			t.Errorf("pane title = %q, want %q", got, name)
		}
		out, err := exec.Command("tmux", "-L", h.Socket, "display-message", "-p", "-t", inst.Panels[0].ID.String(), "#{pane_current_path}").Output()
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.TrimSpace(string(out)); got != dir {
			t.Errorf("pane cwd = %q, want %q", got, dir)
		}
	}
}

// A one-element launch is an argv like any other: tmux would hand a single
// argument to the shell as a command line, where a space, a '&' or a ';' in
// the program's path breaks it.
func TestOneElementLaunchRunsWithoutAShell(t *testing.T) {
	h, c := server(t), ctx(t)
	dir := filepath.Join(t.TempDir(), "a b&c;d")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	ran := filepath.Join(dir, "ran")
	program := filepath.Join(dir, "run me")
	script := "#!/bin/sh\ntouch '" + ran + "'\nsleep 30\n"
	if err := os.WriteFile(program, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Open(c, revier.Realization{
		Name: "one", Launch: []string{program}, Match: revier.Match{Title: "^one$"},
	}); err != nil {
		t.Fatalf("Open: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(ran); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%q did not run within 5s", program)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// tmux numbers windows per server from @0, so a new server hands out the ids
// an old one used. A binding kept from before a restart must not raise the
// unrelated window that now holds its old number.
func TestABindingDoesNotOutliveItsServer(t *testing.T) {
	h, c := server(t), ctx(t)
	cr := &core.Core{Runtime: h}
	p, err := core.PrepareProject(revier.Project{
		Name: "revier",
		Targets: []revier.Target{
			{Name: "home", Home: true, Runtime: &revier.Realization{
				Name: "home", Launch: []string{"sh", "-c", "sleep 30"},
				Match: revier.Match{Title: "^home$"},
			}},
			{Name: "diff", Runtime: &revier.Realization{
				Name: "diff", Launch: []string{"sh", "-c", "sleep 30"},
				Match: revier.Match{Title: "^diff$"},
			}},
		},
	})
	if err != nil {
		t.Fatalf("PrepareProject: %v", err)
	}
	diff, err := cr.Go(c, p, "diff", nil)
	if err != nil {
		t.Fatalf("open diff: %v", err)
	}
	if err := exec.Command("tmux", "-L", h.Socket, "kill-server").Run(); err != nil {
		t.Fatalf("kill-server: %v", err)
	}
	if _, err := cr.Go(c, p, "home", nil); err != nil {
		t.Fatalf("open home on the new server: %v", err)
	}

	res, err := cr.Go(c, p, "diff", core.Bindings{"diff": diff.Ref})
	if err != nil {
		t.Fatalf("go diff: %v", err)
	}
	if !res.Launched {
		t.Fatalf("go diff raised %q: the binding from the old server landed on another window", res.Ref.Title)
	}
}
