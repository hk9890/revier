// Package logging writes revier's operational log: one JSON line per record,
// one file per day under the state root, kept for Keep days. Every process
// appends to the same file - a keybinding's, the TUI, a restore - so each line
// carries the pid and the command that wrote it.
//
// Op, Poll and PollAt time an operation from its start, and every line they
// write carries duration_ms. Poll, PollAt and Repeat keep a poll that recurs
// from writing a line on every refresh.
//
// Read is the one reader of it: the WARN and ERROR lines the TUI's logs
// screen lists.
//
// It is the log/slog default logger and nothing else. A package that logs calls
// slog directly; a process that never called Setup writes nothing here.
// cmd/revier installs a discarding logger when Setup fails, so slog's own
// default never prints to stderr.
package logging

import (
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Keep is how many days of files stay on disk, today's included. The older
// ones are removed when a process creates the day's file.
const Keep = 14

// SlowPoll is how long a background poll - a survey the TUI refreshes on, the
// wait for a launched window - may take before it is worth a line. Below it a
// poll is the normal heartbeat, and a line a second would bury the operations.
const SlowPoll = 500 * time.Millisecond

// SlowRemotePoll is SlowPoll for a survey that waits for a linked host. Its
// time holds the ssh round trip and the host's own revier list, about 500 ms
// on a healthy host, so at SlowPoll it would write a line almost every minute
// and hide the survey that is slow.
const SlowRemotePoll = time.Second

const (
	prefix = "revier-"
	suffix = ".log"
	layout = "2006-01-02"
)

// Dir is where the files are.
func Dir(stateRoot string) string { return filepath.Join(stateRoot, "logs") }

// File is the log file of day under a state root.
func File(stateRoot string, day time.Time) string {
	return filepath.Join(Dir(stateRoot), fileName(day.Format(layout)))
}

func fileName(day string) string { return prefix + day + suffix }

// Setup makes the daily file the default slog logger for this process, writing
// records at level and above. A log that cannot be opened is not a reason to
// fail a keypress: the caller reports the error and carries on.
func Setup(stateRoot, command string, level slog.Level) error {
	w := &daily{dir: Dir(stateRoot), now: time.Now}
	created, err := w.open()
	if err != nil {
		return err
	}
	h := slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level})
	slog.SetDefault(slog.New(h).With("pid", os.Getpid(), "cmd", command))
	if created {
		w.prune()
	}
	return nil
}

// Op records one operation that started at start: at Info when it succeeded,
// at Error when it failed.
func Op(op string, start time.Time, err error, attrs ...any) {
	attrs = append(attrs, "duration_ms", time.Since(start).Milliseconds())
	if err != nil {
		slog.Error(op, append(attrs, "err", err)...)
		return
	}
	slog.Info(op, attrs...)
}

// SlowEvery is how often one recurring poll that stays slow is logged.
const SlowEvery = time.Minute

// Poll is Op for a poll that recurs, such as the TUI's survey every refresh.
// key names the poll, so one host's failure does not hide another's. A fast
// success writes nothing, a slow one writes at most once per SlowEvery, and a
// failure is Repeat's.
func Poll(key, op string, start time.Time, err error, attrs ...any) {
	recurring.poll(key, op, SlowPoll, time.Since(start), err, attrs)
}

// PollAt is Poll for a poll that is slow at slow, such as SlowRemotePoll for
// one that waits for a linked host.
func PollAt(key, op string, slow time.Duration, start time.Time, err error, attrs ...any) {
	recurring.poll(key, op, slow, time.Since(start), err, attrs)
}

// Repeat logs the failure of something that recurs once, at Warn, and again
// only with a message it has not logged since the last success; the first
// success after it is one Info line. A host down for an hour is two lines, not
// one per refresh, and one that fails three ways in turn is four.
//
// The Warn line is op with err. The Info line is op+": recovered" with was,
// the err of the run's last failure, last_failed, when that failure was, and
// failures, how many calls of the run failed.
func Repeat(key, op string, err error, attrs ...any) {
	recurring.repeat(key, op, err, attrs)
}

var recurring = &repeats{log: slog.Default, now: time.Now, failing: map[string]failure{}, slow: map[string]time.Time{}}

// repeats is what this process has seen and logged of each recurring key.
type repeats struct {
	mu      sync.Mutex
	log     func() *slog.Logger
	now     func() time.Time
	failing map[string]failure   // the failure a key is in, by key
	slow    map[string]time.Time // when a slow poll was last logged, by key
}

// failure is one key's run of errors since its last success.
type failure struct {
	last     string          // the error of the latest poll
	lastAt   time.Time       // when the latest poll failed
	failures int             // how many polls of the run failed
	logged   map[string]bool // every error the run has logged
}

func (r *repeats) poll(key, op string, slow, took time.Duration, err error, attrs []any) {
	attrs = append(attrs, "duration_ms", took.Milliseconds())
	r.repeat(key, op, err, attrs)
	if err != nil || took < slow {
		return
	}
	r.mu.Lock()
	now := r.now()
	due := now.Sub(r.slow[key]) >= SlowEvery
	if due {
		r.slow[key] = now
	}
	r.mu.Unlock()
	if due {
		r.log().Info(op, append(attrs, "slow", true)...)
	}
}

func (r *repeats) repeat(key, op string, err error, attrs []any) {
	r.mu.Lock()
	f, was := r.failing[key]
	fresh := false
	if err == nil {
		delete(r.failing, key)
	} else {
		if !was {
			f.logged = map[string]bool{}
		}
		f.last, f.lastAt = err.Error(), r.now()
		f.failures++
		fresh = !f.logged[f.last]
		f.logged[f.last] = true
		r.failing[key] = f
	}
	r.mu.Unlock()
	switch {
	case err == nil && was:
		// The run's first WARN says when it began; the last failure and the
		// count say whether it failed all along, which a run that went back
		// to an error it had logged, or a hidden popup that polled nothing,
		// leaves silent.
		r.log().Info(op+": recovered", append(attrs, "was", f.last, "last_failed", f.lastAt, "failures", f.failures)...)
	case fresh:
		r.log().Warn(op, append(attrs, "err", err)...)
	}
}

// daily is the file of the current day. A TUI left open past midnight moves to
// the next day's file at its first write after it.
type daily struct {
	mu  sync.Mutex
	dir string
	now func() time.Time
	day string
	f   *os.File

	pruning sync.WaitGroup
}

func (d *daily) Write(b []byte) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.now().Format(layout) != d.day {
		created, err := d.openLocked()
		if err != nil {
			return 0, err
		}
		if created {
			// Off this goroutine: the handler that called Write holds its
			// lock, and the prune reports a failure through it.
			d.pruning.Go(d.prune)
		}
	}
	return d.f.Write(b)
}

func (d *daily) open() (bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.openLocked()
}

// openLocked opens the day's file for append, and reports whether it created
// it. Each record is one write of one line, and O_APPEND keeps the lines of
// concurrent processes whole.
func (d *daily) openLocked() (bool, error) {
	day := d.now().Format(layout)
	if err := os.MkdirAll(d.dir, 0o755); err != nil {
		return false, err
	}
	path := filepath.Join(d.dir, fileName(day))
	_, statErr := os.Stat(path)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return false, err
	}
	if d.f != nil {
		_ = d.f.Close() // append-only; every write before it has landed
	}
	d.f, d.day = f, day
	return os.IsNotExist(statErr), nil
}

// prune removes old files once a day, when the day's file is created, so a
// keypress costs no directory listing. When it cannot list the log directory,
// or cannot remove a file older than Keep days, it writes "log prune" once at
// Warn, with dir and err. It stops at that failure, and the files it did not
// remove stay.
func (d *daily) prune() {
	if err := prune(d.dir, d.now()); err != nil {
		slog.Warn("log prune", "dir", d.dir, "err", err)
	}
}

// prune removes the day files older than Keep days. A file whose name is not a
// day file is not revier's and is left alone.
func prune(dir string, now time.Time) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	oldest := oldestKept(now)
	for _, e := range entries {
		name := e.Name()
		day, ok := fileDay(name, now.Location())
		if !ok || !day.Before(oldest) {
			continue
		}
		// Two processes can start the same new day and prune together.
		if err := os.Remove(filepath.Join(dir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}
