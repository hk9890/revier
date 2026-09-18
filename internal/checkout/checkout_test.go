package checkout_test

import (
	"io"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/checkout"
	"github.com/hk9890/revier/pkg/revier"
)

// A link records the host's path and repository for its targets to render, so
// both name a directory on another machine and neither is cloned here
// (decisions.md D83). The refusal names the host that holds the checkout.
func TestEnsureRefusesToCloneALink(t *testing.T) {
	p := revier.Project{
		Name:   "far",
		Path:   "/srv/far",
		GitURL: "git@github.com:example/far.git",
		Remote: &revier.Link{Host: "buildbox", Project: "far"},
	}
	cloned, err := checkout.Ensure(p, io.Discard)
	if cloned {
		t.Error("cloned a link")
	}
	if err == nil || !strings.Contains(err.Error(), "buildbox") {
		t.Errorf("err = %v, want the clone refused, naming the host that holds the checkout", err)
	}
}
