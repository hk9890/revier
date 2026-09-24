package claude_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/hk9890/revier/internal/adapter/claude"
	"github.com/hk9890/revier/pkg/revier"
)

// Nothing to show is a normal outcome and says so: a pane whose process no
// session lists, a session with no transcript, and a transcript the agent has
// said nothing in are all revier.ErrNoDetail, so the core shows nothing and
// logs nothing. A read that broke is not one of them.
func TestDetailSaysThereIsNothingToShowRatherThanFailing(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(t *testing.T) (*claude.Probe, revier.Panel)
	}{
		{"the pid is not listed", func(t *testing.T) (*claude.Probe, revier.Panel) {
			p, root := transcripts(t)
			writeTranscript(t, root, "-demo", "s1", said("x"))
			return p, revier.Panel{PID: 202}
		}},
		{"a panel with no pid", func(t *testing.T) (*claude.Probe, revier.Panel) {
			p, _ := transcripts(t)
			return p, revier.Panel{}
		}},
		{"no transcript", func(t *testing.T) (*claude.Probe, revier.Panel) {
			p, _ := transcripts(t)
			return p, revier.Panel{PID: 101}
		}},
		{"nothing said", func(t *testing.T) (*claude.Probe, revier.Panel) {
			p, root := transcripts(t)
			writeTranscript(t, root, "-demo", "s1",
				`{"type":"user","message":{"role":"user","content":"hello?"}}`)
			return p, revier.Panel{PID: 101}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, panel := tc.setup(t)
			if _, err := p.Detail(context.Background(), panel); !errors.Is(err, revier.ErrNoDetail) {
				t.Errorf("Detail = %v, want revier.ErrNoDetail", err)
			}
		})
	}
}

// A session with no transcript under any project directory is looked for once
// in MaxAge, not on every refresh. The look reads every project directory -
// there can be hundreds - and the pane asks for a detail for every agent it
// shows, every refresh. A transcript that appears is found at the next look.
func TestDetailLooksForAMissingTranscriptOnceInMaxAge(t *testing.T) {
	p, root := transcripts(t)
	now := time.Now()
	p.SetNow(func() time.Time { return now })

	if _, err := p.Detail(context.Background(), revier.Panel{PID: 101}); !errors.Is(err, revier.ErrNoDetail) {
		t.Fatalf("Detail = %v with no transcript, want revier.ErrNoDetail", err)
	}
	writeTranscript(t, root, "-somewhere-else", "s1", said("here now"))
	if _, err := p.Detail(context.Background(), revier.Panel{PID: 101}); !errors.Is(err, revier.ErrNoDetail) {
		t.Errorf("Detail = %v within MaxAge, want the look not made again", err)
	}
	now = now.Add(claude.MaxAge)
	if d, err := p.Detail(context.Background(), revier.Panel{PID: 101}); err != nil || d.Message != "here now" {
		t.Errorf("Detail = %+v, %v after MaxAge; want the transcript found", d, err)
	}
}

// A transcript found under the directory named from the session's working
// directory is read at once, whatever an earlier look came to: that path is
// tried before the look, so the common case waits for nothing.
func TestDetailReadsATranscriptUnderTheWorkingDirectoryWithoutWaiting(t *testing.T) {
	root := t.TempDir()
	p, _ := transcriptsIn(t, root, "/home/user/dev/demo")
	now := time.Now()
	p.SetNow(func() time.Time { return now })

	if _, err := p.Detail(context.Background(), revier.Panel{PID: 101}); !errors.Is(err, revier.ErrNoDetail) {
		t.Fatalf("Detail = %v with no transcript, want revier.ErrNoDetail", err)
	}
	writeTranscript(t, root, "-home-user-dev-demo", "s1", said("just started"))
	if d, err := p.Detail(context.Background(), revier.Panel{PID: 101}); err != nil || d.Message != "just started" {
		t.Errorf("Detail = %+v, %v; want the transcript read at once", d, err)
	}
}

// What was read for a session is dropped once the listing no longer holds it.
// A read keeps the whole of an agent's last message, and the surface is one
// process that outlives every agent in it.
func TestDetailForgetsASessionTheListingNoLongerHolds(t *testing.T) {
	root := t.TempDir()
	p := &claude.Probe{SessionsDir: filepath.Join(root, "sessions")}
	live := `[{"pid": 101, "sessionId": "s1", "status": "idle"}]`
	p.SetAgents(func(context.Context) ([]byte, error) { return []byte(live), nil })
	now := time.Now()
	p.SetNow(func() time.Time { return now })
	writeTranscript(t, root, "-demo", "s1", said("the last word"))

	if d, err := p.Detail(context.Background(), revier.Panel{PID: 101}); err != nil || d.Message != "the last word" {
		t.Fatalf("Detail = %+v, %v; want the message read", d, err)
	}
	if reads, _ := p.Cached(); reads != 1 {
		t.Fatalf("the probe keeps %d reads, want the live session's", reads)
	}

	live = `[]`
	now = now.Add(claude.MaxAge)
	if _, err := p.Detail(context.Background(), revier.Panel{PID: 101}); !errors.Is(err, revier.ErrNoDetail) {
		t.Fatalf("Detail = %v for a session that ended, want revier.ErrNoDetail", err)
	}
	if reads, sweeps := p.Cached(); reads != 0 || sweeps != 0 {
		t.Errorf("the probe keeps %d reads and %d looks for a session that ended, want none", reads, sweeps)
	}
}
