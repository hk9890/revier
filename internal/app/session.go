package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
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
// the save a plain restore opens (decisions.md D149) already holds the same,
// or nothing is open. saved is false when it wrote nothing.
//
// A caller that cancels saves nothing, and the cancel is the error: the
// agents read on a cancelled context lack their conversations, and that
// session could be the one a plain restore opens. A budget that
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
	logSaved(stored, path, gaps)
	return stored, path, nil
}

func logSaved(stored session.Session, path string, gaps core.SessionGaps) {
	slog.Info("session saved", "id", stored.ID, "name", stored.Name, "path", path, "by", string(stored.Reason),
		"projects", len(stored.Projects), "targets", stored.Targets(), "conversations", stored.Conversations(),
		"unnamed_agents", gaps.Unnamed, "agents_in_tab", gaps.InTab, "attached_not_recorded", gaps.Attached)
	for _, err := range gaps.Failed {
		slog.Warn("session save: probe could not be asked", "err", err)
	}
}

// SaveSeen keeps s as what the surface last saw (decisions.md D150).
func SaveSeen(stateRoot string, s session.Session) error {
	return session.SaveSeen(stateRoot, s)
}

// Unclean reports whether revier ended without a shutdown and lost
// something, and returns what a surface last saw when it did. It writes
// nothing, so a dry run asks it. r is a survey in which every host here
// listed. The end was clean when a full shutdown is on record (decisions.md
// D141), when no surface kept what it saw, when any session was saved after
// the surface last wrote it, since that save is newer word on what was open,
// or when every target it holds is open now (decisions.md D150).
func Unclean(c *core.Core, stateRoot string, r core.Report) (session.Session, bool, error) {
	if c.Ledger.State().ShutDown != nil {
		return session.Session{}, false, nil
	}
	seen, err := session.LoadSeen(stateRoot)
	if errors.Is(err, session.ErrNoSession) {
		return session.Session{}, false, nil
	}
	if err != nil {
		return session.Session{}, false, err
	}
	all, err := session.List(stateRoot)
	if err != nil {
		return session.Session{}, false, err
	}
	if slices.ContainsFunc(all, func(s session.Session) bool { return s.At.After(seen.At) }) {
		return session.Session{}, false, nil
	}
	return seen, c.Lost(seen, r), nil
}

// Recover is Unclean that acts: on an end that lost something it returns
// the session to offer, and what was last seen is taken, so it is offered
// once. When the save a plain restore opens holds the same, that save is
// offered and the snapshot dropped. Otherwise the snapshot becomes a saved
// session of the reason last-seen, of the moment it was written. Of two
// surfaces that start at once, one offers it and the other finds nothing.
func Recover(c *core.Core, stateRoot string, r core.Report) (session.Session, bool, error) {
	seen, unclean, err := Unclean(c, stateRoot, r)
	if err != nil || !unclean {
		return session.Session{}, false, err
	}
	var offer session.Session
	newest, err := session.Load(stateRoot, "")
	switch {
	case err == nil && newest.SameAs(seen):
		offer, err = newest, session.DropSeen(stateRoot)
	case err != nil && !errors.Is(err, session.ErrNoSession):
		return session.Session{}, false, err
	default:
		var path string
		if offer, path, err = session.PromoteSeen(stateRoot); err == nil {
			logSaved(offer, path, core.SessionGaps{})
		}
	}
	if errors.Is(err, session.ErrNoSession) {
		return session.Session{}, false, nil
	}
	if err != nil {
		return session.Session{}, false, err
	}
	var missing []string
	for _, step := range c.RestorePlan(seen, r) {
		if step.Action == core.RestoreLaunch {
			missing = append(missing, fmt.Sprintf("%s:%s", step.Project, step.Target))
		}
	}
	slog.Info("unclean end", "seen_at", seen.At, "missing", missing, "session", offer.ID)
	return offer, true, nil
}
