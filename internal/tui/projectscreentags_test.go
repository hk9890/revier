package tui_test

import (
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
