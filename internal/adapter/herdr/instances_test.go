// What Instances costs and how it reads the server's answer, against a
// caller or a socket the test serves itself. No recorded output and no herdr,
// so the default layer runs it.
package herdr_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hk9890/revier/internal/adapter/herdr"
)

// One request, whatever the project count, while each pane's process carries
// its pane id.
func TestInstancesCostOneRequestWhateverTheProjectCount(t *testing.T) {
	const projects = 90
	const sock = "/run/herdr.sock"
	root := t.TempDir()
	var workspaces, panes []string
	for i := range projects {
		for p := 1; p <= 2; p++ {
			writeProc(t, root, 1000+2*i+p, 4242, fmt.Sprintf("HERDR_PANE_ID=w%d:p%d", i, p), "HERDR_SOCKET_PATH="+sock)
		}
		workspaces = append(workspaces, fmt.Sprintf(`{"workspace_id":"w%d","label":"project-%d"}`, i, i))
		panes = append(panes,
			fmt.Sprintf(`{"pane_id":"w%d:p1","workspace_id":"w%d","tab_id":"w%d:t1"}`, i, i, i),
			fmt.Sprintf(`{"pane_id":"w%d:p2","workspace_id":"w%d","tab_id":"w%d:t2"}`, i, i, i))
	}
	result := `{"type":"session_snapshot","snapshot":{"workspaces":[` + strings.Join(workspaces, ",") +
		`],"panes":[` + strings.Join(panes, ",") + `]}}`

	var calls []string
	h := &herdr.Host{Socket: sock}
	h.SetProcRoot(root)
	h.SetCaller(func(_ context.Context, method string, _ any) (int, []byte, error) {
		calls = append(calls, method)
		return 4242, []byte(result), nil
	})

	got, err := h.Instances(context.Background())
	if err != nil {
		t.Fatalf("Instances: %v", err)
	}
	if len(got) != projects {
		t.Fatalf("got %d instances, want %d", len(got), projects)
	}
	if ref := got[1].Ref; ref.ID != "4242/w1" || ref.Title != "project-1" {
		t.Errorf("ref = %+v, want id 4242/w1 titled project-1", ref)
	}
	if p := got[1].Panels; len(p) != 2 || p[0].ID != "w1:p1" || p[0].Tab != "w1:t1" || p[1].Tab != "w1:t2" {
		t.Errorf("panels = %+v, want w1:p1 in w1:t1 and w1:p2 in w1:t2", p)
	}
	if len(calls) != 1 {
		t.Errorf("herdr asked %d times, want 1: %q", len(calls), calls)
	}
}

// A pane moved to another workspace has a new id while its process carries
// the old one, so herdr is asked for that pane's process, and for no other.
// A pane herdr cannot answer for is listed with no process.
func TestAMovedPaneIsAskedForItsProcessAlone(t *testing.T) {
	const sock = "/run/herdr.sock"
	root := t.TempDir()
	writeProc(t, root, 1001, 4242, "HERDR_PANE_ID=w1:p1", "HERDR_SOCKET_PATH="+sock)
	writeProc(t, root, 1002, 4242, "HERDR_PANE_ID=w1:p2", "HERDR_SOCKET_PATH="+sock) // now w2:p2
	snapshot := `{"snapshot":{"workspaces":[{"workspace_id":"w1","label":"a"},{"workspace_id":"w2","label":"b"}],
		"panes":[{"pane_id":"w1:p1","workspace_id":"w1","tab_id":"w1:t1"},
			{"pane_id":"w2:p2","workspace_id":"w2","tab_id":"w2:t1"},
			{"pane_id":"w2:p3","workspace_id":"w2","tab_id":"w2:t1"}]}}`

	var asked []string
	h := &herdr.Host{Socket: sock}
	h.SetProcRoot(root)
	h.SetCaller(func(_ context.Context, method string, params any) (int, []byte, error) {
		if method == "session.snapshot" {
			return 4242, []byte(snapshot), nil
		}
		pane := params.(map[string]string)["pane_id"]
		asked = append(asked, method+" "+pane)
		if pane == "w2:p3" {
			return 0, nil, errors.New("herdr pane.process_info: not_found: pane not found")
		}
		return 4242, []byte(`{"process_info":{"pane_id":"w2:p2","shell_pid":1002}}`), nil
	})

	got, err := h.Instances(context.Background())
	if err != nil {
		t.Fatalf("Instances: %v", err)
	}
	if want := []string{"pane.process_info w2:p2", "pane.process_info w2:p3"}; !slices.Equal(asked, want) {
		t.Errorf("asked %q, want %q", asked, want)
	}
	if len(got) != 2 || len(got[1].Panels) != 2 {
		t.Errorf("instances = %+v, want w2 with both its panes listed", got)
	}
}

// The token revier leaves on a workspace is its title, so a rename in herdr
// does not lose it; a workspace without one, or with an empty one, is known
// by its label.
func TestTheNameTokenWinsOverTheLabel(t *testing.T) {
	result := `{"snapshot":{"focused_workspace_id":"w2","workspaces":[
		{"workspace_id":"w1","label":"renamed","tokens":{"revier_name":"home"}},
		{"workspace_id":"w2","label":"by hand"},
		{"workspace_id":"w3","label":"emptied","tokens":{"revier_name":""}}]}}`
	h := &herdr.Host{}
	h.SetProcRoot(t.TempDir())
	h.SetCaller(func(context.Context, string, any) (int, []byte, error) { return 7, []byte(result), nil })

	got, err := h.Instances(context.Background())
	if err != nil {
		t.Fatalf("Instances: %v", err)
	}
	if len(got) != 3 || got[0].Title != "home" || got[1].Title != "by hand" || got[2].Title != "emptied" {
		t.Errorf("titles = %+v, want home, by hand and emptied", got)
	}
	focused, err := h.Focused(context.Background())
	if err != nil {
		t.Fatalf("Focused: %v", err)
	}
	if focused.ID != "7/w2" || focused.Title != "by hand" {
		t.Errorf("Focused = %+v, want 7/w2 titled by hand", focused)
	}
}

// A socket nobody listens on is no server yet: an empty list and no focus,
// not an error.
func TestNoServerIsNothingListed(t *testing.T) {
	h := &herdr.Host{Socket: filepath.Join(t.TempDir(), "absent.sock")}
	got, err := h.Instances(context.Background())
	if err != nil || got != nil {
		t.Errorf("Instances = %v, %v; want nil, nil", got, err)
	}
	focused, err := h.Focused(context.Background())
	if err != nil || !focused.IsZero() {
		t.Errorf("Focused = %+v, %v; want nothing", focused, err)
	}
}

// The host speaks one line each way on the socket, and the server's pid is
// the peer's, which here is this test's own process.
func TestTheSocketCarriesOneRequestAndItsAnswer(t *testing.T) {
	sock := serve(t, func(req map[string]any) string {
		if req["method"] != "session.snapshot" {
			return fmt.Sprintf(`{"id":%q,"error":{"code":"invalid_request","message":"unknown method"}}`, req["id"])
		}
		return fmt.Sprintf(`{"id":%q,"result":{"type":"session_snapshot","snapshot":{"workspaces":[{"workspace_id":"w1","label":"demo"}]}}}`, req["id"])
	})
	h := &herdr.Host{Socket: sock}
	h.SetProcRoot(t.TempDir())
	got, err := h.Instances(context.Background())
	if err != nil {
		t.Fatalf("Instances: %v", err)
	}
	if want := fmt.Sprintf("%d/w1", os.Getpid()); len(got) != 1 || got[0].Ref.ID != want {
		t.Errorf("instances = %+v, want one with id %s", got, want)
	}
}

// A server that accepts and never answers holds the host no longer than the
// caller's context, also a context that is cancelled and has no deadline.
func TestACancelEndsARequestTheServerNeverAnswers(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "s.sock")
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	accepted := make(chan net.Conn, 1)
	go func() {
		if conn, err := l.Accept(); err == nil {
			accepted <- conn
		}
	}()
	t.Cleanup(func() {
		_ = l.Close()
		select {
		case conn := <-accepted:
			_ = conn.Close()
		default:
		}
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	time.AfterFunc(50*time.Millisecond, cancel)
	done := make(chan error, 1)
	go func() {
		_, err := (&herdr.Host{Socket: sock}).Instances(ctx)
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("err = %v, want one that says the caller cancelled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Instances still waits on a silent server after its context was cancelled")
	}
}

// An error herdr answers with names the method and herdr's code.
func TestAnErrorNamesTheMethodAndTheCode(t *testing.T) {
	sock := serve(t, func(req map[string]any) string {
		return fmt.Sprintf(`{"id":%q,"error":{"code":"internal","message":"boom"}}`, req["id"])
	})
	_, err := (&herdr.Host{Socket: sock}).Instances(context.Background())
	if err == nil || !strings.Contains(err.Error(), "session.snapshot") || !strings.Contains(err.Error(), "internal: boom") {
		t.Errorf("err = %v, want one naming session.snapshot and internal: boom", err)
	}
}
