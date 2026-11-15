package hc_test

import (
	"testing"

	"github.com/hk9890/revier/internal/adapter/hc"
	"github.com/hk9890/revier/pkg/revier"
)

// hc runs programs and draws nothing, so it is a runtime and no view: the
// wiring then finds no view of its own for it (decisions.md D176).
func TestHcIsARuntimeAndNoView(t *testing.T) {
	var rt revier.Runtime = &hc.Host{}
	if _, ok := rt.(revier.View); ok {
		t.Error("the hc runtime is a view")
	}
	if _, ok := rt.(revier.Attacher); !ok {
		t.Error("the hc runtime cannot attach a terminal")
	}
}
