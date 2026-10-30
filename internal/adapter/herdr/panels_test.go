// What the panel operations refuse before they act, and how they read
// herdr's answers, against a caller in place of the socket. No herdr and no
// process, so the default layer runs it.
package herdr_test

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/adapter/herdr"
	"github.com/hk9890/revier/pkg/revier"
)

// A tab's vars are its identity, so one herdr would change refuses the tab
// before herdr is asked anything.
func TestOpenTabRefusesAVarHerdrWouldChange(t *testing.T) {
	ref := revier.TargetRef{Host: "herdr", ID: "9/w1"}
	many := map[string]string{}
	for i := range 17 {
		many[fmt.Sprintf("v%d", i)] = "x"
	}
	for name, c := range map[string]struct {
		vars map[string]string
		want string
	}{
		"newline":   {map[string]string{"revier_target": "a\nb"}, "not kept as written"},
		"too long":  {map[string]string{"revier_target": strings.Repeat("x", 81)}, "not kept as written"},
		"bad name":  {map[string]string{"revier.target": "a"}, "not a token name"},
		"too many":  {many, "at most 16 vars"},
		"leading ␠": {map[string]string{"revier_target": " a"}, "not kept as written"},
	} {
		h, asked := recording(t)
		_, err := h.OpenTab(context.Background(), ref, revier.Realization{Launch: []string{"sh"}}, c.vars)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: OpenTab = %v, want %q", name, err, c.want)
		}
		if len(*asked) != 0 {
			t.Errorf("%s: asked %q, want nothing", name, *asked)
		}
	}
}

// herdr can make the tab and its answer be lost, the end of the context while
// it is read: the tab of the workspace that was not there before is closed,
// since nothing would ever find it unmarked.
func TestOpenTabClosesATabWhoseAnswerWasLost(t *testing.T) {
	snapshots := []string{
		`{"snapshot":{"layouts":[{"workspace_id":"w1","tab_id":"w1:t1"},{"workspace_id":"w2","tab_id":"w2:t1"}]}}`,
		`{"snapshot":{"layouts":[{"workspace_id":"w1","tab_id":"w1:t1"},{"workspace_id":"w1","tab_id":"w1:t3"},
			{"workspace_id":"w2","tab_id":"w2:t1"},{"workspace_id":"w2","tab_id":"w2:t2"}]}}`,
	}
	var closed []string
	h := &herdr.Host{}
	h.SetProcRoot(t.TempDir())
	h.SetCaller(func(_ context.Context, method string, params any) (int, []byte, error) {
		switch method {
		case "session.snapshot":
			s := snapshots[0]
			snapshots = snapshots[1:]
			return 9, []byte(s), nil
		case "layout.apply":
			return 0, nil, context.DeadlineExceeded
		case "tab.close":
			closed = append(closed, params.(map[string]string)["tab_id"])
		}
		return 9, []byte(`{}`), nil
	})
	_, err := h.OpenTab(context.Background(), revier.TargetRef{Host: "herdr", ID: "9/w1"},
		revier.Realization{Launch: []string{"sh"}}, map[string]string{"revier_target": "agent"})
	if err == nil {
		t.Fatal("OpenTab succeeded with no answer to its layout")
	}
	if !slices.Equal(closed, []string{"w1:t3"}) {
		t.Errorf("closed %q, want the new tab of w1 alone", closed)
	}
}

// Every panel operation that acts on a pane first checks the server is the
// one that assigned the ref: herdr numbers panes per server too. OpenTab pings
// twice: first to read the server's PATH.
func TestPanelOperationsRefuseAWorkspaceOfAnEarlierServer(t *testing.T) {
	stale := revier.TargetRef{Host: "herdr", ID: "4242/w1"}
	c := context.Background()
	for name, act := range map[string]func(*herdr.Host) error{
		"OpenTab": func(h *herdr.Host) error {
			_, err := h.OpenTab(c, stale, revier.Realization{Launch: []string{"sh"}}, nil)
			return err
		},
		"FocusPanel": func(h *herdr.Host) error { return h.FocusPanel(c, stale, "w1:p1") },
		"SendText":   func(h *herdr.Host) error { return h.SendText(c, stale, "w1:p1", "x") },
		"ReadPanel": func(h *herdr.Host) error {
			_, err := h.ReadPanel(c, stale, "w1:p1", false)
			return err
		},
		"ClosePanel": func(h *herdr.Host) error { return h.ClosePanel(c, stale, "w1:p1") },
	} {
		h, asked := recording(t)
		if err := act(h); err == nil || !strings.Contains(err.Error(), "4242/w1 is gone") {
			t.Errorf("%s = %v, want the workspace named as gone", name, err)
		}
		if !slices.Equal(slices.Compact(*asked), []string{"ping"}) {
			t.Errorf("%s asked %q, want ping alone", name, *asked)
		}
	}
}

// The focused pane is the one of the workspace's active tab, read from the
// listing, whether or not the workspace is the one shown.
func TestFocusedPanelIsTheActiveTabsFocusedPane(t *testing.T) {
	h := &herdr.Host{}
	h.SetCaller(func(context.Context, string, any) (int, []byte, error) {
		return 9, []byte(`{"snapshot":{"focused_workspace_id":"w2",
			"workspaces":[{"workspace_id":"w1","active_tab_id":"w1:t2"},{"workspace_id":"w2","active_tab_id":"w2:t1"}],
			"layouts":[{"tab_id":"w1:t1","focused_pane_id":"w1:p1"},{"tab_id":"w1:t2","focused_pane_id":"w1:p4"},
				{"tab_id":"w2:t1","focused_pane_id":"w2:p1"}]}}`), nil
	})
	got, err := h.FocusedPanel(context.Background(), revier.TargetRef{Host: "herdr", ID: "9/w1"})
	if err != nil || got != "w1:p4" {
		t.Errorf("FocusedPanel = %q, %v; want w1:p4", got, err)
	}
	if _, err := h.FocusedPanel(context.Background(), revier.TargetRef{Host: "herdr", ID: "4242/w1"}); err == nil {
		t.Error("FocusedPanel answered for a workspace of an earlier server")
	}
}

// A read asks for the screen alone, or for the last 1000 lines with
// scrollback, and its lines end as tmux ends them.
func TestReadPanelAsksForTheScreenOrTheScrollback(t *testing.T) {
	var reads []map[string]any
	h := &herdr.Host{}
	h.SetCaller(func(_ context.Context, method string, params any) (int, []byte, error) {
		if method == "pane.read" {
			reads = append(reads, params.(map[string]any))
		}
		return 9, []byte(`{"read":{"text":"\u001b[1mred\u001b[0m\r\nplain\r\n"}}`), nil
	})
	ref := revier.TargetRef{Host: "herdr", ID: "9/w1"}
	got, err := h.ReadPanel(context.Background(), ref, "w1:p2", false)
	if err != nil || got != "\x1b[1mred\x1b[0m\nplain\n" {
		t.Errorf("ReadPanel = %q, %v; want the text with \\n line ends", got, err)
	}
	if _, err := h.ReadPanel(context.Background(), ref, "w1:p2", true); err != nil {
		t.Fatalf("ReadPanel with scrollback: %v", err)
	}
	if len(reads) != 2 || reads[0]["source"] != "visible" || reads[1]["source"] != "recent" || reads[1]["lines"] != 1000 {
		t.Errorf("reads = %v, want the visible screen, then the last 1000 recent lines", reads)
	}
}
