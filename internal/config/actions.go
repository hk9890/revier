package config

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
)

// ErrActionsChanged is an edit to an action that config.toml no longer holds
// as the caller read it: the file was changed by hand since.
var ErrActionsChanged = errors.New("the actions in config.toml changed since revier read them; restart revier")

// AddAction writes a new [[action]] table at the end of config.toml, which
// must still hold the actions was. A name or a key is checked against was, so
// an action added to the file by hand since could otherwise be added twice.
func AddAction(root string, was []Action, act Action) error {
	return editActions(root, func(lines []string, _ []table, have []Action) ([]string, []Action, error) {
		if !slices.EqualFunc(have, was, sameAction) {
			return nil, nil, ErrActionsChanged
		}
		out := strings.TrimRight(strings.Join(lines, "\n"), "\n")
		if out != "" {
			out += "\n\n"
		}
		values, err := actionValues(act)
		if err != nil {
			return nil, nil, err
		}
		out += "[[action]]\n"
		for _, v := range values {
			out += v.key + " = " + v.literal + "\n"
		}
		return strings.Split(out, "\n"), append(slices.Clone(have), act), nil
	})
}

// ReplaceAction writes act in place of the i-th action, which must still be
// was. Only a value that changed is written, where it stands, so the entry's
// comments, a value's own spelling and any key revier does not know stay as
// they were.
func ReplaceAction(root string, i int, was, act Action) error {
	return editActions(root, func(lines []string, _ []table, have []Action) ([]string, []Action, error) {
		if i >= len(have) || !sameAction(have[i], was) {
			return nil, nil, ErrActionsChanged
		}
		values, err := actionValues(act)
		if err != nil {
			return nil, nil, err
		}
		old, err := actionValues(was)
		if err != nil {
			return nil, nil, err
		}
		for j, v := range values {
			if v == old[j] {
				continue
			}
			// Each put can move the lines after it, so the entry is found
			// again for the next one.
			lines = putKey(lines, actionTables(lines)[i], v.key, v.literal)
		}
		want := slices.Clone(have)
		want[i] = act
		return lines, want, nil
	})
}

// RemoveAction deletes the i-th action, which must still be was. Its header
// and its values go; its comments stay, including one written after a value
// or inside an array over several lines. An entry left with nothing but blank
// lines goes whole.
func RemoveAction(root string, i int, was Action) error {
	return editActions(root, func(lines []string, entries []table, have []Action) ([]string, []Action, error) {
		if i >= len(have) || !sameAction(have[i], was) {
			return nil, nil, ErrActionsChanged
		}
		t := entries[i]
		var kept []string
		if h := headerLine.FindStringSubmatch(lines[t.header]); h[3] != "" {
			kept = append(kept, h[3])
		}
		for j := t.header + 1; j < t.end; j++ {
			at := keyLine.FindStringSubmatchIndex(lines[j])
			if at == nil {
				kept = append(kept, lines[j])
				continue
			}
			end, _ := valueEnd(lines, j, at[1])
			for k, from := j, at[1]; k <= end; k, from = k+1, 0 {
				if c := commentOf(lines[k], from); c != "" {
					kept = append(kept, c)
				}
			}
			j = end
		}
		if strings.TrimSpace(strings.Join(kept, "")) == "" {
			kept = nil
		}
		out := slices.Concat(lines[:t.header], kept, lines[t.end:])
		return out, slices.Delete(slices.Clone(have), i, i+1), nil
	})
}

// editActions runs one change to the [[action]] tables of config.toml and
// writes the result only if it parses as Load parses it and decodes to the
// actions the change meant. Actions written in another form, such as an
// inline array, are refused: the line editor would not find them.
func editActions(root string, change func(lines []string, entries []table, have []Action) ([]string, []Action, error)) error {
	path := File(root)
	old, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("%s: %w", path, err)
	}
	cfg, err := parse(old)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	lines := strings.Split(string(old), "\n")
	entries := actionTables(lines)
	if len(entries) != len(cfg.Actions) {
		return fmt.Errorf("%s: the actions are not written as [[action]] tables; change them by hand", path)
	}
	lines, want, err := change(lines, entries, cfg.Actions)
	if err != nil {
		return err
	}
	text := strings.Join(lines, "\n")
	got, err := parse([]byte(text))
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if !slices.EqualFunc(got.Actions, want, sameAction) {
		return fmt.Errorf("%s: the actions did not come out as written; change them by hand", path)
	}
	return replaceFile(path, []byte(text))
}

// actionTables is every [[action]] entry of a file, in file order, which is
// the order Load reads them in.
func actionTables(lines []string) []table {
	var out []table
	for _, t := range tables(lines) {
		if t.array && t.name == "action" {
			out = append(out, t)
		}
	}
	return out
}

// actionValue is one key of an [[action]] entry, as TOML writes its value.
type actionValue struct{ key, literal string }

// actionValues is every key revier writes for an action, in the order a new
// entry lists them.
func actionValues(act Action) ([]actionValue, error) {
	var out []actionValue
	for _, kv := range []struct {
		key   string
		value any
	}{{"key", act.Key}, {"name", act.Name}, {"run", act.Run}} {
		literal, err := literalOf(kv.value)
		if err != nil {
			return nil, fmt.Errorf("action.%s: %w", kv.key, err)
		}
		out = append(out, actionValue{kv.key, literal})
	}
	return out, nil
}

// commentOf is the comment on a line, read from the column its value text
// starts at: a # that no quote hides, to the end of the line.
func commentOf(line string, from int) string {
	var quote byte
	for j := from; j < len(line); j++ {
		c := line[j]
		switch {
		case quote != 0:
			if c == '\\' && quote == '"' {
				j++
			} else if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '#':
			return strings.TrimSpace(line[j:])
		}
	}
	return ""
}

func sameAction(a, b Action) bool {
	return a.Key == b.Key && a.Name == b.Name && slices.Equal(a.Run, b.Run)
}
