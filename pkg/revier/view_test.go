package revier_test

import (
	"testing"

	"github.com/hk9890/revier/pkg/revier"
)

// A project is held while anything on this machine holds it. The home target
// is one way; a target that is not the home is another, and so is a terminal
// attached by hand, which the survey adds as a target of its own. Every agent
// a surface shows sits in a panel of one of these, so a project that reads as
// closed shows no agents (decisions.md D104).
func TestHeldIsAnyTargetOrAttachmentHere(t *testing.T) {
	ref := revier.TargetRef{Host: "kitty", ID: "@1"}
	for name, v := range map[string]revier.ProjectView{
		"the home target": {Running: true, Home: ref, Targets: []revier.TargetView{{Name: "home", Ref: ref}}},
		"another target":  {Targets: []revier.TargetView{{Name: "home"}, {Name: "logs", Ref: ref}}},
		"an attachment":   {Targets: []revier.TargetView{{Name: "home"}, {Ref: ref, Attached: true}}},
	} {
		if !v.Held() {
			t.Errorf("%s: Held = false, want the project open", name)
		}
	}
	closed := revier.ProjectView{Targets: []revier.TargetView{{Name: "home", Available: true}, {Name: "logs"}}}
	if closed.Held() {
		t.Errorf("Held = true, want closed: no instance here is its")
	}
}
