// Package logging writes revier's operational log: one JSON line per record,
// one file per day under the state root, kept for Keep days. Every process
// appends to the same file - a keybinding's, the TUI, a restore - so each line
// carries the pid and the command that wrote it.
//
// It is the log/slog default logger and nothing else. A package that logs calls
// slog directly; a process that never called Setup writes nothing here.
package logging

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Keep is how many days of files stay on disk, today's included.
const Keep = 14

// SlowPoll is how long a background poll - a survey the TUI refreshes on, the
// wait for a launched window - may take before it is worth a line. Below it a
// poll is the normal heartbeat, and a line a second would bury the operations.
const SlowPoll = 500 * time.Millisecond

const (
	prefix = "revier-"
	suffix = ".log"
	layout = "2006-01-02"
)

// Dir is where the files are.
func Dir(stateRoot string) string { return filepath.Join(stateRoot, "logs") }

// Setup makes the daily file the default slog logger for this process. A log
// that cannot be opened is not a reason to fail a keypress: the caller reports
// the error and carries on, and the default logger then discards.
func Setup(stateRoot, command string) error {
	w := &daily{dir: Dir(stateRoot), now: time.Now}
	created, err := w.open()
	if err != nil {
		slog.SetDefault(slog.New(slog.DiscardHandler))
		return err
	}
	h := slog.NewJSONHandler(w, &slog.HandlerOptions{Level: slog.LevelInfo})
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
		slog.Error(op, append(attrs, "err", err.Error())...)
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
	recurring.poll(key, op, time.Since(start), err, attrs)
}

// Repeat logs the failure of something that recurs once, at Warn, and again
// only when its message changes; the first success after it is one Info line.
// A host down for an hour is two lines, not one per refresh.
func Repeat(key, op string, err error, attrs ...any) {
	recurring.repeat(key, op, err, attrs)
}

var recurring = &repeats{now: time.Now, failing: map[string]string{}, slow: map[string]time.Time{}}

// repeats is what this process last logged about each recurring key.
type repeats struct {
	mu      sync.Mutex
	now     func() time.Time
	failing map[string]string    // the error last logged, by key
	slow    map[string]time.Time // when a slow poll was last logged, by key
}

func (r *repeats) poll(key, op string, took time.Duration, err error, attrs []any) {
	attrs = append(attrs, "duration_ms", took.Milliseconds())
	r.repeat(key, op, err, attrs)
	if err != nil || took < SlowPoll {
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
		slog.Info(op, append(attrs, "slow", true)...)
	}
}

func (r *repeats) repeat(key, op string, err error, attrs []any) {
	r.mu.Lock()
	last, was := r.failing[key]
	switch {
	case err == nil:
		delete(r.failing, key)
	case !was || last != err.Error():
		r.failing[key] = err.Error()
	}
	r.mu.Unlock()
	switch {
	case err == nil && was:
		slog.Info(op+": recovered", append(attrs, "was", last)...)
	case err != nil && (!was || last != err.Error()):
		slog.Warn(op, append(attrs, "err", err.Error())...)
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
	path := filepath.Join(d.dir, prefix+day+suffix)
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
// keypress costs no directory listing.
func (d *daily) prune() {
	if err := prune(d.dir, d.now()); err != nil {
		slog.Warn("log prune", "dir", d.dir, "err", err.Error())
	}
}

// prune removes the day files older than Keep days. A file whose name is not a
// day file is not revier's and is left alone.
func prune(dir string, now time.Time) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	y, m, dd := now.Date()
	oldest := time.Date(y, m, dd-(Keep-1), 0, 0, 0, 0, now.Location())
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, suffix) {
			continue
		}
		day, err := time.ParseInLocation(layout, strings.TrimSuffix(strings.TrimPrefix(name, prefix), suffix), now.Location())
		if err != nil || !day.Before(oldest) {
			continue
		}
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			return err
		}
	}
	return nil
}
