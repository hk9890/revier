package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/session"
)

// ErrNothingOpen is a save with no project open, which is a normal outcome
// and not a failure. An empty session is refused because it would become
// the newest, the one a plain restore opens: the save made before a reboot
// would lose to one made after it.
var ErrNothingOpen = errors.New("nothing is open; no session saved")

// SaveSession records what r found open under stateRoot, named name and
// stamped at, and returns the session as stored, the file it wrote, and what
// the save could not record. It refuses with ErrNothingOpen when no project
// is open. The command and the surface both save through here, so the
// refusal and what is logged are decided once.
func SaveSession(ctx context.Context, c *core.Core, stateRoot string, r core.Report, name string, at time.Time) (session.Session, string, core.SessionGaps, error) {
	s, gaps := c.Session(ctx, r)
	if len(s.Projects) == 0 {
		return session.Session{}, "", gaps, ErrNothingOpen
	}
	s.At, s.Name, s.Reason = at, name, session.ReasonSaved
	stored, path, err := store(stateRoot, s, gaps)
	return stored, path, gaps, err
}

// SaveChanged saves the session a close ends, for the given reason, unless
// the newest saved session already holds the same, or nothing is open. saved
// is false when it wrote nothing.
//
// A caller that cancels saves nothing, and the cancel is the error: the
// agents read on a cancelled context lack their conversations, and that
// session would be the newest, the one a plain restore opens. A budget that
// runs out is no such decision, and the save names what it could not ask.
func SaveChanged(ctx context.Context, c *core.Core, stateRoot string, r core.Report, reason session.Reason, at time.Time) (stored session.Session, saved bool, gaps core.SessionGaps, err error) {
	s, gaps := c.Session(ctx, r)
	if errors.Is(ctx.Err(), context.Canceled) {
		return session.Session{}, false, gaps, ctx.Err()
	}
	if len(s.Projects) == 0 {
		return session.Session{}, false, gaps, nil
	}
	newest, err := session.Load(stateRoot, "")
	switch {
	case err == nil && newest.SameAs(s):
		return newest, false, gaps, nil
	case err != nil && !errors.Is(err, session.ErrNoSession):
		return session.Session{}, false, gaps, err
	}
	s.At, s.Reason = at, reason
	stored, _, err = store(stateRoot, s, gaps)
	if err != nil {
		return session.Session{}, false, gaps, err
	}
	return stored, true, gaps, nil
}

// store writes s and logs what it holds and what it lacks, and by its reason
// which save wrote the file.
func store(stateRoot string, s session.Session, gaps core.SessionGaps) (session.Session, string, error) {
	stored, path, err := session.Save(stateRoot, s)
	if err != nil {
		return session.Session{}, "", fmt.Errorf("save the session: %w", err)
	}
	slog.Info("session saved", "id", stored.ID, "name", stored.Name, "path", path, "by", string(stored.Reason),
		"projects", len(stored.Projects), "targets", stored.Targets(), "conversations", stored.Conversations(),
		"unnamed_agents", gaps.Unnamed, "agents_in_tab", gaps.InTab, "attached_not_recorded", gaps.Attached)
	for _, err := range gaps.Failed {
		slog.Warn("session save: probe could not be asked", "err", err)
	}
	return stored, path, nil
}
