package tui_test

import (
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/hosttest"
)

const manyTagsProject = demoProject + `
[tags]
alpha = "first-value-of-many"
bravo = "second-value-of-many"
charlie = "third-value-of-many"
delta = "fourth-value-of-many"
echo = "fifth-value-of-many"
foxtrot = "sixth-value-of-many"
`

// The tags row is one line, by key, that ends in how many more there are;
// a click opens it to one tag a line, and a second click closes it.
func TestAClickOpensAndClosesTheTagsRow(t *testing.T) {
	m, _, _ := projectSurface(t, manyTagsProject, hosttest.NewRuntime("rt"))
	if !strings.Contains(screen(m), "Tags     alpha=first-value-of-many") || !strings.Contains(screen(m), " more") {
		t.Fatalf("the closed row does not start with the first key and end in more:\n%s", screen(m))
	}
	if strings.Contains(screen(m), "foxtrot=") {
		t.Fatalf("the closed row shows every tag; the test wants a narrower pane:\n%s", screen(m))
	}

	x, y := paneCell(t, m, "Tags")
	m = clickAt(m, x, y)
	for _, want := range []string{"alpha=first-value-of-many", "foxtrot=sixth-value-of-many"} {
		if !strings.Contains(screen(m), want) {
			t.Errorf("the open row does not show %q:\n%s", want, screen(m))
		}
	}
	if strings.Contains(screen(m), " more") {
		t.Errorf("the open row still says there are more:\n%s", screen(m))
	}

	x, y = paneCell(t, m, "Tags")
	m = clickAt(m, x, y)
	if strings.Contains(screen(m), "foxtrot=") {
		t.Errorf("the second click did not close the row:\n%s", screen(m))
	}
}

// A row that shows every tag has nothing to open: a click on it changes
// nothing.
func TestTagsThatFitAreOneLineAndAClickLeavesThem(t *testing.T) {
	m, _, _ := projectSurface(t, demoProject+"\n[tags]\ntype = \"work\"\nlanguage = \"go\"\n", hosttest.NewRuntime("rt"))
	if !strings.Contains(screen(m), "Tags     language=go  type=work") || strings.Contains(screen(m), " more") {
		t.Fatalf("the row is not the two tags by key:\n%s", screen(m))
	}
	before := screen(m)
	x, y := paneCell(t, m, "Tags")
	if after := screen(clickAt(m, x, y)); after != before {
		t.Errorf("a click on a row with nothing left out changed the pane:\n%s", after)
	}
}

func TestAProjectWithNoTagsHasNoTagsRow(t *testing.T) {
	m, _, _ := projectSurface(t, demoProject, hosttest.NewRuntime("rt"))
	if strings.Contains(screen(m), "Tags ") {
		t.Errorf("a project with no tags shows a tags row:\n%s", screen(m))
	}
}
