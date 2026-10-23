// What Open, Focus, Close and AttachCommand refuse before they act, against a
// caller in place of the socket. No herdr and no process, so the default
// layer runs it.
package herdr_test

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/adapter/herdr"
	"github.com/hk9890/revier/pkg/revier"
)

// recording is a host whose server has pid 9 and answers every request with
// an empty result, and the methods it was asked.
func recording() (*herdr.Host, *[]string) {
	var asked []string
	h := &herdr.Host{}
	h.SetCaller(func(_ context.Context, method string, _ any) (int, []byte, error) {
		asked = append(asked, method)
		return 9, []byte(`{}`), nil
	})
	return h, &asked
}

// herdr numbers workspaces per server, so a ref from an earlier server names
// an unrelated workspace now: neither a focus nor a close acts on it.
func TestFocusAndCloseRefuseAWorkspaceOfAnEarlierServer(t *testing.T) {
	stale := revier.TargetRef{Host: "herdr", ID: "4242/w1"}
	for name, act := range map[string]func(*herdr.Host) error{
		"Focus": func(h *herdr.Host) error { return h.Focus(context.Background(), stale) },
		"Close": func(h *herdr.Host) error { return h.Close(context.Background(), stale) },
	} {
		h, asked := recording()
		if err := act(h); err == nil || !strings.Contains(err.Error(), "4242/w1 is gone") {
			t.Errorf("%s = %v, want the workspace named as gone", name, err)
		}
		if !slices.Equal(*asked, []string{"ping"}) {
			t.Errorf("%s asked %q, want ping alone", name, *asked)
		}
	}
}

func TestFocusActsOnTheWorkspaceOfTheServerThatAnswers(t *testing.T) {
	h, asked := recording()
	if err := h.Focus(context.Background(), revier.TargetRef{Host: "herdr", ID: "9/w1"}); err != nil {
		t.Fatalf("Focus: %v", err)
	}
	if !slices.Equal(*asked, []string{"ping", "workspace.focus"}) {
		t.Errorf("asked %q, want ping then workspace.focus", *asked)
	}
}

func TestARefOfAnotherHostIsRefused(t *testing.T) {
	h, asked := recording()
	ref := revier.TargetRef{Host: "tmux", ID: "9/$1"}
	if err := h.Focus(context.Background(), ref); err == nil {
		t.Error("Focus took a tmux ref")
	}
	if err := h.Close(context.Background(), ref); err == nil {
		t.Error("Close took a tmux ref")
	}
	if _, err := h.AttachCommand(ref); err == nil {
		t.Error("AttachCommand took a tmux ref")
	}
	if len(*asked) != 0 {
		t.Errorf("asked %q, want nothing", *asked)
	}
}

// The terminal attaches to the server the host speaks to, and lands on the
// workspace that server shows.
func TestAttachCommandCarriesAPrivateSocket(t *testing.T) {
	ref := revier.TargetRef{Host: "herdr", ID: "9/w1"}
	got, err := (&herdr.Host{}).AttachCommand(ref)
	if err != nil || !slices.Equal(got, []string{"herdr"}) {
		t.Errorf("AttachCommand = %q, %v; want herdr", got, err)
	}
	got, err = (&herdr.Host{Socket: "/tmp/s.sock"}).AttachCommand(ref)
	if want := []string{"env", "HERDR_SOCKET_PATH=/tmp/s.sock", "herdr"}; err != nil || !slices.Equal(got, want) {
		t.Errorf("AttachCommand = %q, %v; want %q", got, err, want)
	}
}

// One layout holds at most 24 panes, so a larger one is refused before herdr
// is asked anything.
func TestOpenRefusesMorePanelsThanATabHolds(t *testing.T) {
	h, asked := recording()
	panels := make([]revier.PanelSpec, 25)
	_, err := h.Open(context.Background(), revier.Realization{Name: "big", Panels: panels})
	if err == nil || !strings.Contains(err.Error(), "at most 24") {
		t.Errorf("Open = %v, want a refusal naming 24", err)
	}
	if len(*asked) != 0 {
		t.Errorf("asked %q, want nothing", *asked)
	}
}
