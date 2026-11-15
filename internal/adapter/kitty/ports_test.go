package kitty_test

import (
	"testing"

	"github.com/hk9890/revier/internal/adapter/kitty"
	"github.com/hk9890/revier/pkg/revier"
)

// kitty runs its programs and draws them, so one host is the runtime and the
// view over the same kitty processes (decisions.md D176), and each optional
// capability is on the half the core asks it of.
func TestKittyIsTheRuntimeAndTheViewWithTheirCapabilities(t *testing.T) {
	var rt revier.Runtime = &kitty.Host{}
	for name, ok := range map[string]bool{
		"TabOpener":   is[revier.TabOpener](rt),
		"PanelWriter": is[revier.PanelWriter](rt),
		"PanelReader": is[revier.PanelReader](rt),
		"PanelCloser": is[revier.PanelCloser](rt),
		"Closer":      is[revier.Closer](rt),
	} {
		if !ok {
			t.Errorf("the kitty runtime is no %s", name)
		}
	}
	view, ok := rt.(revier.View)
	if !ok {
		t.Fatal("the kitty runtime is not its own view")
	}
	if !is[revier.PanelFocuser](view) || !is[revier.PanelFinder](view) {
		t.Error("the kitty view cannot focus a panel or map a key's panel id")
	}
	if !view.Capabilities().OSWindows {
		t.Error("the kitty view does not claim OS windows")
	}
}

func is[C any](v any) bool {
	_, ok := v.(C)
	return ok
}
