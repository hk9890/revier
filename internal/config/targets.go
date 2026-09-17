package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/pkg/revier"
)

// The shared targets of config.toml, written from the config screen. A
// target is edited line by line, as an action is: only a value that changed
// is written, in place of the old one, so the comments in an entry and the
// values the screen does not show stay as they were. A realization is a
// sub-table of its entry, [target.runtime] or [target.window], and each panel
// an entry of [[target.runtime.panels]] under it.
//
// A write is refused unless every project still loads with the result:
// a shared target is part of every project, and a change that breaks one
// would make the next start refuse.

// ErrTargetsChanged is an edit to shared targets that config.toml no longer
// holds as the caller read them: the file was changed by hand since.
var ErrTargetsChanged = errors.New("the shared targets in config.toml changed since revier read them; restart revier")

// TargetEdit is a shared target as the config screen changed it.
type TargetEdit struct {
	Target revier.Target
	// PanelFrom is, for each panel of Target.Runtime, the index of the
	// panel it was in the file, or -1 for a new one. A panel of the file no
	// index names was deleted. Kept panels stay in their order, and new ones
	// come after them.
	PanelFrom []int
}

// TargetsWritten is what a change to the shared targets left: the shared
// targets as config.toml now holds them, and every project loaded with them.
type TargetsWritten struct {
	Shared   []map[string]any
	Projects []core.Project
}

// DecodeTargets is shared targets as typed targets.
func DecodeTargets(shared []map[string]any) ([]revier.Target, error) {
	out := make([]revier.Target, len(shared))
	for i, t := range shared {
		if err := recode(t, &out[i]); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// AddTarget writes a new shared target at the end of config.toml, which must
// still hold the shared targets was.
func AddTarget(root string, was []map[string]any, t revier.Target) (TargetsWritten, error) {
	return editTargets(root, was, func(lines []string, have []revier.Target, raw []map[string]any) ([]string, []revier.Target, error) {
		lines = appendTarget(lines, t.Name)
		old := revier.Target{Name: t.Name}
		lines, err := applyTarget(lines, len(have), old, map[string]any{}, TargetEdit{Target: t, PanelFrom: newPanels(panelCount(t))})
		return lines, append(slices.Clone(have), t), err
	})
}

// ReplaceTarget writes the i-th shared target as e says.
func ReplaceTarget(root string, i int, was []map[string]any, e TargetEdit) (TargetsWritten, error) {
	return editTargets(root, was, func(lines []string, have []revier.Target, raw []map[string]any) ([]string, []revier.Target, error) {
		if i >= len(have) {
			return nil, nil, ErrTargetsChanged
		}
		lines, err := applyTarget(lines, i, have[i], raw[i], e)
		want := slices.Clone(have)
		want[i] = e.Target
		return lines, want, err
	})
}

// RemoveTarget deletes the i-th shared target. Its tables and values go; its
// comments stay.
func RemoveTarget(root string, i int, was []map[string]any) (TargetsWritten, error) {
	return editTargets(root, was, func(lines []string, have []revier.Target, _ []map[string]any) ([]string, []revier.Target, error) {
		if i >= len(have) {
			return nil, nil, ErrTargetsChanged
		}
		e := targetEntries(lines)[i]
		return dropLines(lines, e.start, e.end), slices.Delete(slices.Clone(have), i, i+1), nil
	})
}

// editTargets runs one change to the [[target]] entries of config.toml, and
// writes it only if it parses, decodes to the targets the change meant, and
// leaves every project loading.
func editTargets(root string, was []map[string]any, change func(lines []string, have []revier.Target, raw []map[string]any) ([]string, []revier.Target, error)) (TargetsWritten, error) {
	path := File(root)
	old, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return TargetsWritten{}, fmt.Errorf("%s: %w", path, err)
	}
	cfg, err := parse(old)
	if err != nil {
		return TargetsWritten{}, fmt.Errorf("%s: %w", path, err)
	}
	if (len(cfg.Targets) > 0 || len(was) > 0) && !reflect.DeepEqual(cfg.Targets, was) {
		return TargetsWritten{}, ErrTargetsChanged
	}
	lines := strings.Split(string(old), "\n")
	if len(targetEntries(lines)) != len(cfg.Targets) {
		return TargetsWritten{}, fmt.Errorf("%s: the shared targets are not written as [[target]] tables; change them by hand", path)
	}
	have, err := DecodeTargets(cfg.Targets)
	if err != nil {
		return TargetsWritten{}, fmt.Errorf("%s: %w", path, err)
	}
	lines, want, err := change(lines, have, cfg.Targets)
	if err != nil {
		return TargetsWritten{}, fmt.Errorf("%s: %w", path, err)
	}
	text := strings.Join(lines, "\n")
	next, err := parse([]byte(text))
	if err != nil {
		return TargetsWritten{}, fmt.Errorf("%s: %w", path, err)
	}
	got, err := DecodeTargets(next.Targets)
	if err != nil || !sameTargets(got, want) {
		return TargetsWritten{}, fmt.Errorf("%s: the shared targets did not come out as written; change them by hand", path)
	}
	projects, err := LoadProjects(filepath.Join(root, "projects"), next.Targets)
	if err != nil {
		return TargetsWritten{}, fmt.Errorf("not written, a project would not load with it: %w", err)
	}
	if err := replaceFile(path, []byte(text)); err != nil {
		return TargetsWritten{}, err
	}
	return TargetsWritten{Shared: next.Targets, Projects: projects}, nil
}

// sameTargets compares targets as they serialize, so an empty list and a
// missing one are the same, as they are in TOML.
func sameTargets(a, b []revier.Target) bool {
	ja, errA := json.Marshal(a)
	jb, errB := json.Marshal(b)
	return errA == nil && errB == nil && string(ja) == string(jb)
}

// applyTarget writes into entry i every value of e.Target that differs from
// old. raw is the entry as TOML decoded it, for the keys of a match the
// screen does not show.
func applyTarget(lines []string, i int, old revier.Target, raw map[string]any, e TargetEdit) ([]string, error) {
	t := e.Target
	var err error
	for _, kv := range []struct {
		key      string
		old, new any
	}{
		{"name", string(old.Name), string(t.Name)},
		{"key", old.Key, t.Key},
		{"home", old.Home, t.Home},
		{"prefer", string(old.Prefer), string(t.Prefer)},
	} {
		if lines, err = putValue(lines, i, "target", kv.key, kv.old, kv.new); err != nil {
			return nil, err
		}
	}
	for _, r := range []struct {
		kind     string
		old, new *revier.Realization
	}{
		{"runtime", old.Runtime, t.Runtime},
		{"window", old.Window, t.Window},
	} {
		path := "target." + r.kind
		if r.new == nil {
			if r.old != nil {
				lines = dropTables(lines, i, path)
			}
			continue
		}
		o := r.old
		if o == nil {
			o = &revier.Realization{}
		}
		for _, kv := range []struct {
			key      string
			old, new any
		}{
			{"name", o.Name, r.new.Name},
			{"launch", o.Launch, r.new.Launch},
			{"dir", o.Dir, r.new.Dir},
			{"place", o.Place, r.new.Place},
		} {
			if lines, err = putValue(lines, i, path, kv.key, kv.old, kv.new); err != nil {
				return nil, err
			}
		}
		if o.Match != r.new.Match {
			rawRealization, _ := raw[r.kind].(map[string]any)
			rawMatch, _ := rawRealization["match"].(map[string]any)
			if lines, err = putMatch(lines, i, path, rawMatch, r.new.Match); err != nil {
				return nil, err
			}
		}
		if r.kind == "runtime" {
			if lines, err = putPanels(lines, i, o.Panels, r.new.Panels, e.PanelFrom); err != nil {
				return nil, err
			}
		}
	}
	return lines, nil
}

// putValue writes key under the table path of entry i when the value
// changed: set, or removed when the new value is empty. A missing table is
// added at the end of the entry.
func putValue(lines []string, i int, path, key string, old, value any) ([]string, error) {
	if reflect.DeepEqual(old, value) || isEmpty(old) && isEmpty(value) {
		return lines, nil
	}
	if isEmpty(value) {
		if t, ok := tableIn(lines, i, path); ok {
			lines = deleteKey(lines, t, key)
		}
		return lines, nil
	}
	literal, err := literalOf(value)
	if err != nil {
		return nil, fmt.Errorf("%s.%s: %w", path, key, err)
	}
	return putLiteral(lines, i, path, key, literal), nil
}

func putLiteral(lines []string, i int, path, key, literal string) []string {
	t, ok := tableIn(lines, i, path)
	if !ok {
		lines = addTable(lines, i, path, false)
		t, _ = tableIn(lines, i, path)
	}
	return putKey(lines, t, key, literal)
}

// putMatch writes a realization's match. A match written as a table of its
// own is changed key by key; otherwise it is an inline table, written whole
// with the keys the screen does not show kept.
func putMatch(lines []string, i int, path string, raw map[string]any, m revier.Match) ([]string, error) {
	values := map[string]any{"class": m.Class, "title": m.Title, "pid": int64(m.PID)}
	if t, ok := tableIn(lines, i, path+".match"); ok {
		for _, key := range []string{"class", "title", "pid"} {
			if isEmpty(values[key]) || values[key] == int64(0) {
				lines = deleteKey(lines, t, key)
				t, _ = tableIn(lines, i, path+".match")
				continue
			}
			literal, err := literalOf(values[key])
			if err != nil {
				return nil, err
			}
			lines = putKey(lines, t, key, literal)
			t, _ = tableIn(lines, i, path+".match")
		}
		return lines, nil
	}
	merged := map[string]any{}
	for k, v := range raw {
		merged[k] = v
	}
	for k, v := range values {
		if isEmpty(v) || v == int64(0) {
			delete(merged, k)
		} else {
			merged[k] = v
		}
	}
	if len(merged) == 0 {
		if t, ok := tableIn(lines, i, path); ok {
			lines = deleteKey(lines, t, "match")
		}
		return lines, nil
	}
	literal, err := inlineTable(merged)
	if err != nil {
		return nil, err
	}
	return putLiteral(lines, i, path, "match", literal), nil
}

// inlineTable writes a table on one line, class and title first.
func inlineTable(t map[string]any) (string, error) {
	keys := make([]string, 0, len(t))
	for k := range t {
		keys = append(keys, k)
	}
	rank := map[string]int{"class": 0, "title": 1, "pid": 2}
	sort.Slice(keys, func(a, b int) bool {
		ra, oka := rank[keys[a]]
		rb, okb := rank[keys[b]]
		switch {
		case oka && okb:
			return ra < rb
		case oka != okb:
			return oka
		}
		return keys[a] < keys[b]
	})
	parts := make([]string, len(keys))
	for j, k := range keys {
		literal, err := literalOf(t[k])
		if err != nil {
			return "", err
		}
		parts[j] = k + " = " + literal
	}
	return "{ " + strings.Join(parts, ", ") + " }", nil
}

// putPanels writes a runtime realization's panels. A panel deleted on the
// screen loses its entry, a kept one is changed value by value, and a new one
// is added after the last.
func putPanels(lines []string, i int, old, panels []revier.PanelSpec, from []int) ([]string, error) {
	const path = "target.runtime.panels"
	if len(arrayTables(lines, i, path)) != len(old) {
		return nil, errors.New("the panels are not written as [[target.runtime.panels]] tables; change them by hand")
	}
	kept := map[int]bool{}
	for _, f := range from {
		if f >= 0 {
			kept[f] = true
		}
	}
	for j := len(old) - 1; j >= 0; j-- {
		if !kept[j] {
			t := arrayTables(lines, i, path)[j]
			lines = dropLines(lines, t.header, t.end)
		}
	}
	// A kept panel's entry is found by its rank among the kept ones.
	rank := 0
	for k, p := range panels {
		f := -1
		if k < len(from) {
			f = from[k]
		}
		if f < 0 {
			continue
		}
		was := old[f]
		for _, kv := range []struct {
			key      string
			old, new any
		}{
			{"kind", string(was.Kind), string(p.Kind)},
			{"title", was.Title, p.Title},
			{"command", was.Command, p.Command},
		} {
			if reflect.DeepEqual(kv.old, kv.new) || isEmpty(kv.old) && isEmpty(kv.new) {
				continue
			}
			t := arrayTables(lines, i, path)[rank]
			if isEmpty(kv.new) {
				lines = deleteKey(lines, t, kv.key)
				continue
			}
			literal, err := literalOf(kv.new)
			if err != nil {
				return nil, err
			}
			lines = putKey(lines, t, kv.key, literal)
		}
		rank++
	}
	for k, p := range panels {
		if k < len(from) && from[k] >= 0 {
			continue
		}
		lines = addTable(lines, i, path, true)
		all := arrayTables(lines, i, path)
		t := all[len(all)-1]
		for _, kv := range []struct {
			key   string
			value any
		}{{"kind", string(p.Kind)}, {"title", p.Title}, {"command", p.Command}} {
			if isEmpty(kv.value) {
				continue
			}
			literal, err := literalOf(kv.value)
			if err != nil {
				return nil, err
			}
			lines = putKey(lines, t, kv.key, literal)
			t = arrayTables(lines, i, path)[len(all)-1]
		}
	}
	return lines, nil
}

// entry is the lines of one [[target]]: its header, and the line after the
// last line of its last sub-table.
type entry struct{ start, end int }

// targetEntries is every [[target]] of a file, in file order.
func targetEntries(lines []string) []entry {
	var out []entry
	for _, t := range tables(lines) {
		switch {
		case t.array && t.name == "target":
			out = append(out, entry{t.header, t.end})
		case len(out) > 0 && strings.HasPrefix(t.name, "target.") && out[len(out)-1].end == t.header:
			out[len(out)-1].end = t.end
		}
	}
	return out
}

// tableIn is the table called path in entry i: the entry's own header for
// "target", a sub-table otherwise.
func tableIn(lines []string, i int, path string) (table, bool) {
	e := targetEntries(lines)[i]
	for _, t := range tables(lines) {
		if t.header < e.start || t.header >= e.end {
			continue
		}
		if path == "target" && t.header == e.start || !t.array && t.name == path {
			return t, true
		}
	}
	return table{}, false
}

// arrayTables is every array entry called path in entry i, in order.
func arrayTables(lines []string, i int, path string) []table {
	e := targetEntries(lines)[i]
	var out []table
	for _, t := range tables(lines) {
		if t.header >= e.start && t.header < e.end && t.array && t.name == path {
			out = append(out, t)
		}
	}
	return out
}

// addTable adds the header of table path to entry i: after the tables it
// belongs under, or at the end of the entry. It is indented two spaces a
// level, as the project files are.
func addTable(lines []string, i int, path string, array bool) []string {
	e := targetEntries(lines)[i]
	parent := path[:strings.LastIndex(path, ".")]
	at := e.end
	for _, t := range tables(lines) {
		if t.header > e.start && t.header < e.end && (t.name == parent || strings.HasPrefix(t.name, parent+".")) {
			at = t.end
		}
	}
	// Before the blank lines that end the stretch, so the entry stays
	// separated from what follows it.
	for at > e.start+1 && strings.TrimSpace(lines[at-1]) == "" {
		at--
	}
	header := "[" + path + "]"
	if array {
		header = "[" + header + "]"
	}
	return slices.Insert(lines, at, strings.Repeat("  ", strings.Count(path, "."))+header)
}

// dropTables removes table path of entry i and every table under it.
func dropTables(lines []string, i int, path string) []string {
	for {
		e := targetEntries(lines)[i]
		var last *table
		for _, t := range tables(lines) {
			if t.header > e.start && t.header < e.end && (t.name == path || strings.HasPrefix(t.name, path+".")) {
				last = &t
			}
		}
		if last == nil {
			return lines
		}
		lines = dropLines(lines, last.header, last.end)
	}
}

func isEmpty(v any) bool {
	if v == nil {
		return true
	}
	r := reflect.ValueOf(v)
	switch r.Kind() {
	case reflect.String, reflect.Slice, reflect.Map:
		return r.Len() == 0
	case reflect.Bool:
		return !r.Bool()
	}
	return false
}

func panelCount(t revier.Target) int {
	if t.Runtime == nil {
		return 0
	}
	return len(t.Runtime.Panels)
}
