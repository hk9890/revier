package tui_test

import (
	"os"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/hosttest"
)

// The screen's last row adds a tag typed as key=value; the tag is written to
// the file and listed under Tags, by key, and the pane shows it.
func TestTheProjectScreenAddsATag(t *testing.T) {
	m, file, _ := projectSurface(t, demoProject, hosttest.NewRuntime("rt"))
	m, _ = press(m, "alt+e")
	if !strings.Contains(screen(m), "Tags") || !strings.Contains(screen(m), "add a tag") {
		t.Fatalf("the screen has no Tags section:\n%s", screen(m))
	}
	m = downs(m, 50) // the last row: add a tag
	m, _ = press(m, "enter")
	m = typeInto(m, "type=work")
	m, _ = press(m, "enter")

	if got := fileText(t, file); !strings.HasSuffix(got, "\n[tags]\ntype = \"work\"\n") {
		t.Errorf("file =\n%s\nwant a [tags] table with type", got)
	}
	if !strings.Contains(screen(m), "type") || !strings.Contains(screen(m), "work") {
		t.Errorf("the screen does not list the new tag:\n%s", screen(m))
	}
	m, _ = press(m, "esc")
	if !strings.Contains(screen(m), "Tags     type=work") {
		t.Errorf("the pane does not show the new tag:\n%s", screen(m))
	}
}

// Enter on a tag types its value in place; alt+d asks, and y
// removes it, and its table with the last tag.
func TestTheProjectScreenEditsAndDeletesATag(t *testing.T) {
	m, file, _ := projectSurface(t, demoProject+"\n[tags]\ntype = \"work\"\n", hosttest.NewRuntime("rt"))
	m, _ = press(m, "alt+e")
	m = downs(m, 50)
	m, _ = press(m, "up") // the tag type
	m, _ = press(m, "enter")
	m = typeInto(clearField(m), "private")
	m, _ = press(m, "enter")
	if got := fileText(t, file); !strings.Contains(got, "type = \"private\"") {
		t.Errorf("file =\n%s\nwant type = private", got)
	}

	m, _ = press(m, "alt+d")
	if !strings.Contains(screen(m), `delete tag "type"?`) {
		t.Fatalf("alt+d does not ask:\n%s", screen(m))
	}
	m, _ = press(m, "y")
	if got := fileText(t, file); strings.Contains(got, "[tags]") {
		t.Errorf("file =\n%s\nwant the table gone with its last tag", got)
	}
	if strings.Contains(screen(m), "private") {
		t.Errorf("the screen still lists the deleted tag:\n%s", screen(m))
	}
}

// Enter and Enter again on a tag writes nothing, and a value the input cannot
// hold is not typed over: either way the file keeps it.
func TestAnUnchangedTagEditWritesNothing(t *testing.T) {
	for value, refused := range map[string]bool{` draft `: false, `a\tb`: true} {
		body := demoProject + "\n[tags]\nnote = \"" + value + "\"\n"
		m, file, _ := projectSurface(t, body, hosttest.NewRuntime("rt"))
		m, _ = press(m, "alt+e")
		m = downs(m, 50)
		m, _ = press(m, "up") // the tag note
		m, _ = press(m, "enter")
		if got := strings.Contains(screen(m), "holds a tab or a newline"); got != refused {
			t.Errorf("note = %q: refused %v, want %v:\n%s", value, got, refused, screen(m))
		}
		_, _ = press(m, "enter")
		if got := fileText(t, file); got != body {
			t.Errorf("note = %q: an unchanged edit rewrote the file:\n%s", value, got)
		}
	}
}

// The row that adds a tag does not overwrite one the project has.
func TestAddingATagTheProjectHasIsRefused(t *testing.T) {
	body := demoProject + "\n[tags]\ntype = \"work\"\n"
	m, file, _ := projectSurface(t, body, hosttest.NewRuntime("rt"))
	m, _ = press(m, "alt+e")
	m = downs(m, 50)
	m, _ = press(m, "enter")
	m = typeInto(m, "type=private")
	m, _ = press(m, "enter")
	if got := fileText(t, file); got != body || !strings.Contains(screen(m), "already") {
		t.Errorf("file =\n%s\nscreen:\n%s", got, screen(m))
	}
}

// A long key is cut to the label column, so it does not run into its value.
func TestALongTagKeyStandsApartFromItsValue(t *testing.T) {
	m, _, _ := projectSurface(t, demoProject+"\n[tags]\ninfrastructure = \"work\"\n", hosttest.NewRuntime("rt"))
	m, _ = press(m, "alt+e")
	if !strings.Contains(screen(m), "infrastructu… work") {
		t.Errorf("the key is not cut before its value:\n%s", screen(m))
	}
}

// Another process can add a tag while the screen is up: the cursor follows
// the tag that was edited, by its key.
func TestTheCursorFollowsTheEditedTag(t *testing.T) {
	body := demoProject + "\n[tags]\ntype = \"work\"\n"
	m, file, _ := projectSurface(t, body, hosttest.NewRuntime("rt"))
	m, _ = press(m, "alt+e")
	m = downs(m, 50)
	m, _ = press(m, "up")
	if err := os.WriteFile(file, []byte(body+"aaa = \"1\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, _ = press(m, "enter")
	m = typeInto(clearField(m), "private")
	m, _ = press(m, "enter")
	m, _ = press(m, "alt+d")
	if !strings.Contains(screen(m), `delete tag "type"?`) {
		t.Errorf("the cursor left the edited tag:\n%s", screen(m))
	}
}

// A new tag that is not key=value is refused, and the file stays as it was.
func TestTheProjectScreenRefusesATagWithNoValue(t *testing.T) {
	m, file, _ := projectSurface(t, demoProject, hosttest.NewRuntime("rt"))
	m, _ = press(m, "alt+e")
	m = downs(m, 50)
	m, _ = press(m, "enter")
	m = typeInto(m, "work")
	m, _ = press(m, "enter")
	if got := fileText(t, file); got != demoProject {
		t.Errorf("a refused tag changed the file:\n%s", got)
	}
	if !strings.Contains(screen(m), "key=value") {
		t.Errorf("the screen does not say why:\n%s", screen(m))
	}
}
