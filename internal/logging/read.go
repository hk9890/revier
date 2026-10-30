package logging

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// Line is one WARN or ERROR record of the log, with the fields every reader
// of it wants by name. Raw is the whole line as written, for Fields.
type Line struct {
	Time    time.Time
	Level   string // "WARN" or "ERROR"
	Msg     string
	Pid     int
	Cmd     string
	Project string
	Err     string
	Raw     []byte
}

// Field is one key of a line and its value as written: a string as its text,
// anything else as its JSON.
type Field struct{ Key, Value string }

// Fields is every key of the line in the order it was written. The attrs of
// a line are open-ended, so they are read from the line rather than from a
// struct: a ref is an object, and stands as its JSON.
func (l Line) Fields() []Field {
	dec := json.NewDecoder(bytes.NewReader(l.Raw))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return nil
	}
	var out []Field
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return out
		}
		k, _ := t.(string)
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return out
		}
		out = append(out, Field{Key: k, Value: fieldValue(raw)})
	}
	return out
}

// fieldValue is a field's value as Fields gives it, and no value or null as
// "".
func fieldValue(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var b bytes.Buffer
	if json.Compact(&b, raw) != nil {
		return string(raw)
	}
	return b.String()
}

// Read is every WARN and ERROR line of the day files Keep keeps, as of now,
// newest first. skipped counts the lines that did not decode: a line another
// process is still appending is one. It logs nothing itself, since a line it
// wrote would land in the file it reads, once per read. No directory is no
// line and no error: nothing was ever logged.
func Read(stateRoot string, now time.Time) (lines []Line, skipped int, err error) {
	dir := Dir(stateRoot)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	oldest := oldestKept(now)
	type dayFile struct {
		day  time.Time
		name string
	}
	var files []dayFile
	for _, e := range entries {
		if day, ok := fileDay(e.Name(), now.Location()); ok && !day.Before(oldest) {
			files = append(files, dayFile{day, e.Name()})
		}
	}
	slices.SortFunc(files, func(a, b dayFile) int { return b.day.Compare(a.day) })
	for _, f := range files {
		day, bad, err := readDay(filepath.Join(dir, f.name))
		if err != nil {
			return lines, skipped, err
		}
		slices.Reverse(day)
		lines = append(lines, day...)
		skipped += bad
	}
	return lines, skipped, nil
}

// readDay is the WARN and ERROR lines of one file, in file order. A file
// pruned since the listing has none.
func readDay(path string) (lines []Line, skipped int, err error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	// No bufio.Scanner: a line with a tool's whole output in its err is
	// longer than its buffer.
	for raw := range bytes.Lines(data) {
		raw = bytes.TrimSpace(raw)
		if len(raw) == 0 {
			continue
		}
		var rec struct {
			Time  time.Time `json:"time"`
			Level string    `json:"level"`
			Msg   string    `json:"msg"`
			Pid   int       `json:"pid"`
			Cmd   string    `json:"cmd"`
			// Decoded as written: a line whose project or err a caller
			// gave as no string is still a line.
			Project json.RawMessage `json:"project"`
			Err     json.RawMessage `json:"err"`
		}
		if json.Unmarshal(raw, &rec) != nil {
			skipped++
			continue
		}
		if rec.Level != "WARN" && rec.Level != "ERROR" {
			continue
		}
		lines = append(lines, Line{
			Time: rec.Time, Level: rec.Level, Msg: rec.Msg, Pid: rec.Pid, Cmd: rec.Cmd,
			Project: fieldValue(rec.Project), Err: fieldValue(rec.Err), Raw: bytes.Clone(raw),
		})
	}
	return lines, skipped, nil
}

// oldestKept is the first day of the Keep days that end with now's.
func oldestKept(now time.Time) time.Time {
	y, m, d := now.Date()
	return time.Date(y, m, d-(Keep-1), 0, 0, 0, 0, now.Location())
}

// fileDay is the day a file of the log is for, in loc. A name that is not a
// day file's is not revier's.
func fileDay(name string, loc *time.Location) (time.Time, bool) {
	rest, ok := strings.CutPrefix(name, prefix)
	if !ok {
		return time.Time{}, false
	}
	if rest, ok = strings.CutSuffix(rest, suffix); !ok {
		return time.Time{}, false
	}
	day, err := time.ParseInLocation(layout, rest, loc)
	return day, err == nil
}
