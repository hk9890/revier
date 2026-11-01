// What Open, Focus, Close and AttachCommand refuse before they act, against a
// caller in place of the socket. No herdr and no process, so the default
// layer runs it.
package herdr_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/adapter/herdr"
	"github.com/hk9890/revier/pkg/revier"
)

// herdr numbers workspaces per server, so a ref from an earlier server names
// an unrelated workspace now: neither a focus nor a close acts on it.
func TestFocusAndCloseRefuseAWorkspaceOfAnEarlierServer(t *testing.T) {
	stale := revier.TargetRef{Host: "herdr", ID: "4242/w1"}
	for name, act := range map[string]func(*herdr.Host) error{
		"Focus": func(h *herdr.Host) error { return h.Focus(context.Background(), stale) },
		"Close": func(h *herdr.Host) error { return h.Close(context.Background(), stale) },
	} {
		h, asked := recording(t)
		if err := act(h); err == nil || !strings.Contains(err.Error(), "4242/w1 is gone") {
			t.Errorf("%s = %v, want the workspace named as gone", name, err)
		}
		if !slices.Equal(*asked, []string{"ping"}) {
			t.Errorf("%s asked %q, want ping alone", name, *asked)
		}
	}
}

func TestFocusActsOnTheWorkspaceOfTheServerThatAnswers(t *testing.T) {
	h, asked := recording(t)
	if err := h.Focus(context.Background(), revier.TargetRef{Host: "herdr", ID: "9/w1"}); err != nil {
		t.Fatalf("Focus: %v", err)
	}
	if !slices.Equal(*asked, []string{"ping", "workspace.focus"}) {
		t.Errorf("asked %q, want ping then workspace.focus", *asked)
	}
}

func TestARefOfAnotherHostIsRefused(t *testing.T) {
	h, asked := recording(t)
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
	h, asked := recording(t)
	panels := make([]revier.PanelSpec, 25)
	_, err := h.Open(context.Background(), revier.Realization{Name: "big", Panels: panels})
	if err == nil || !strings.Contains(err.Error(), "at most 24") {
		t.Errorf("Open = %v, want a refusal naming 24", err)
	}
	if len(*asked) != 0 {
		t.Errorf("asked %q, want nothing", *asked)
	}
}

// A server that fails the ping is named as failed. Its PATH is not guessed,
// so a program that may be on its PATH alone is not called missing.
func TestOpenNamesAServerThatFailsThePing(t *testing.T) {
	h := &herdr.Host{}
	h.SetCaller(func(context.Context, string, any) (int, []byte, error) {
		return 0, nil, context.DeadlineExceeded
	})
	panels := []revier.PanelSpec{{Kind: revier.PanelAgent, Command: []string{"revier-no-such-program"}}}
	_, err := h.Open(context.Background(), revier.Realization{Name: "x", Panels: panels})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Open = %v, want the failed ping", err)
	}
}

// herdr can make the workspace and its answer be lost, the end of the context
// while it is read: the workspace with the realization's label that was not
// there before is closed, since the next press would raise it with herdr's
// default shell alone in it.
func TestOpenClosesAWorkspaceWhoseAnswerWasLost(t *testing.T) {
	snapshots := []string{
		`{"snapshot":{"workspaces":[{"workspace_id":"w1","label":"home"}]}}`,
		`{"snapshot":{"workspaces":[{"workspace_id":"w1","label":"home"},
			{"workspace_id":"w2","label":"other"},{"workspace_id":"w3","label":"home"}]}}`,
	}
	var closed []string
	h := &herdr.Host{}
	h.SetProcRoot(t.TempDir())
	h.SetCaller(func(_ context.Context, method string, params any) (int, []byte, error) {
		switch method {
		case "ping":
			return 9, []byte(`{"version":"0.9.5"}`), nil
		case "session.snapshot":
			s := snapshots[0]
			snapshots = snapshots[1:]
			return 9, []byte(s), nil
		case "workspace.create":
			return 0, nil, context.DeadlineExceeded
		case "workspace.close":
			closed = append(closed, params.(map[string]string)["workspace_id"])
		}
		return 9, []byte(`{}`), nil
	})
	_, err := h.Open(context.Background(), revier.Realization{Name: "home", Launch: []string{"sh"}})
	if err == nil {
		t.Fatal("Open succeeded with no answer to its workspace.create")
	}
	if !slices.Equal(closed, []string{"w3"}) {
		t.Errorf("closed %q, want the new workspace labelled home alone", closed)
	}
}
