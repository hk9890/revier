package config

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
)

// Set writes one value into config.toml and leaves every other line of the
// file as it was: comments, order and spacing. A TOML encoder cannot do that -
// it writes what it decoded, and a comment is not decoded - so the value is
// put in place of the old one on its own line, under its table, and the table
// is added at the end when the file has none.
//
// The result is parsed and validated as Load does before anything is written,
// and read back: a key the line editor does not find where it looks, such as
// one written as a dotted key or an inline table, would otherwise land twice
// or not at all. That is refused, and the file stays as it was.
func Set(root, table, key string, value any) error {
	path := File(root)
	old, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("%s: %w", path, err)
	}
	literal, err := literalOf(value)
	if err != nil {
		return fmt.Errorf("%s.%s: %w", table, key, err)
	}
	text := setKey(string(old), table, key, literal)
	if _, err := parse([]byte(text)); err != nil {
		return fmt.Errorf("%s: %s.%s: %w", path, table, key, err)
	}
	if got, err := valueAt(text, table, key); err != nil || got != literal {
		return fmt.Errorf("%s: %s.%s is not written as a plain key under [%s]; change it by hand", path, table, key, table)
	}
	return replaceFile(path, []byte(text))
}

// literalOf is a value as TOML writes it: a quoted string, an array.
func literalOf(value any) (string, error) {
	var b strings.Builder
	if err := toml.NewEncoder(&b).Encode(map[string]any{"v": value}); err != nil {
		return "", err
	}
	return strings.TrimPrefix(strings.TrimSpace(b.String()), "v = "), nil
}

// valueAt is what a decoder reads at table.key, written back as a literal.
func valueAt(text, table, key string) (string, error) {
	var doc map[string]any
	if _, err := toml.Decode(text, &doc); err != nil {
		return "", err
	}
	t, _ := doc[table].(map[string]any)
	v, ok := t[key]
	if !ok {
		return "", fmt.Errorf("%s.%s is not set", table, key)
	}
	return literalOf(v)
}

var (
	// headerLine is a table header, [name] or [[name]], with an optional
	// comment after it.
	headerLine = regexp.MustCompile(`^\s*(\[\[?)\s*([A-Za-z0-9_.-]+)\s*\]\]?\s*(#.*)?$`)
	// keyLine is the start of a bare key's line, up to where its value begins.
	keyLine = regexp.MustCompile(`^\s*([A-Za-z0-9_-]+)\s*=\s*`)
)

// setKey puts key = literal under [table] in text. A missing table is added
// at the end of the file.
func setKey(text, table, key, literal string) string {
	lines := strings.Split(text, "\n")
	for _, t := range tables(lines) {
		if !t.array && t.name == table {
			return strings.Join(putKey(lines, t, key, literal), "\n")
		}
	}
	out := strings.TrimRight(text, "\n")
	if out != "" {
		out += "\n\n"
	}
	return out + "[" + table + "]\n" + key + " = " + literal + "\n"
}

// table is the lines of one table in a file: its header, and the line after
// its last. The lines before the first header are a table with no header, at
// -1.
type table struct {
	array  bool // [[name]], one entry of an array of tables
	name   string
	header int
	end    int
}

// tables splits a file into its tables. A value over several lines is skipped
// whole, so a line inside an array is never read as a header.
func tables(lines []string) []table {
	var out []table
	cur := table{header: -1}
	for i := 0; i < len(lines); i++ {
		if h := headerLine.FindStringSubmatch(lines[i]); h != nil {
			cur.end = i
			out = append(out, cur)
			cur = table{array: h[1] == "[[", name: h[2], header: i}
			continue
		}
		if at := keyLine.FindStringSubmatchIndex(lines[i]); at != nil {
			i, _ = valueEnd(lines, i, at[1])
		}
	}
	cur.end = len(lines)
	return append(out, cur)
}

// putKey puts key = literal in table t. An existing value is replaced where
// it stands, and whatever followed it on its last line - a comment - is kept.
// A missing key goes after the table's last value, indented as the table's
// header is.
func putKey(lines []string, t table, key, literal string) []string {
	last := t.header
	for i := t.header + 1; i < t.end; i++ {
		at := keyLine.FindStringSubmatchIndex(lines[i])
		if at == nil {
			continue
		}
		end, col := valueEnd(lines, i, at[1])
		if lines[i][at[2]:at[3]] == key {
			lines[i] = lines[i][:at[1]] + literal + lines[end][col:]
			return slices.Delete(lines, i+1, end+1)
		}
		last, i = end, end
	}
	indent := ""
	if t.header >= 0 {
		indent = leading(lines[t.header])
	}
	return slices.Insert(lines, last+1, indent+key+" = "+literal)
}

// deleteKey removes key from table t. The comments on its lines stay, each on
// a line of its own.
func deleteKey(lines []string, t table, key string) []string {
	for i := t.header + 1; i < t.end; i++ {
		at := keyLine.FindStringSubmatchIndex(lines[i])
		if at == nil {
			continue
		}
		end, _ := valueEnd(lines, i, at[1])
		if lines[i][at[2]:at[3]] == key {
			return dropLines(lines, i, end+1)
		}
		i = end
	}
	return lines
}

// dropLines removes lines[from:to] - headers, keys and their values - and
// keeps every comment in them: a comment line as it is, and a comment after a
// header or inside a value on a line of its own. A stretch left with nothing
// but blank lines goes whole.
func dropLines(lines []string, from, to int) []string {
	var kept []string
	for j := from; j < to; j++ {
		if h := headerLine.FindStringSubmatch(lines[j]); h != nil {
			if h[3] != "" {
				kept = append(kept, leading(lines[j])+h[3])
			}
			continue
		}
		at := keyLine.FindStringSubmatchIndex(lines[j])
		if at == nil {
			kept = append(kept, lines[j])
			continue
		}
		end, _ := valueEnd(lines, j, at[1])
		for k, col := j, at[1]; k <= end; k, col = k+1, 0 {
			if c := commentOf(lines[k], col); c != "" {
				kept = append(kept, leading(lines[j])+c)
			}
		}
		j = end
	}
	if strings.TrimSpace(strings.Join(kept, "")) == "" {
		kept = nil
	}
	return slices.Concat(lines[:from], kept, lines[to:])
}

// leading is the whitespace a line starts with.
func leading(line string) string {
	return line[:len(line)-len(strings.TrimLeft(line, " \t"))]
}

// valueEnd finds where a value that starts at lines[i][col] ends: the line it
// ends on, and the column after its last character. A bracket opens a value
// that runs until it is closed, over as many lines as that takes; a quote
// hides brackets and comment marks inside it.
func valueEnd(lines []string, i, col int) (int, int) {
	depth := 0
	var quote byte
	end, endCol := i, col
	for ; i < len(lines); i, col = i+1, 0 {
		line := lines[i]
	scan:
		for j := col; j < len(line); j++ {
			c := line[j]
			switch {
			case quote != 0:
				if c == '\\' && quote == '"' {
					j++
				} else if c == quote {
					quote, end, endCol = 0, i, j+1
				}
			case c == '"' || c == '\'':
				quote = c
			case c == '#':
				break scan
			case c == '[' || c == '{':
				depth++
			case c == ']' || c == '}':
				depth--
				end, endCol = i, j+1
			case c == ' ' || c == '\t':
			default:
				end, endCol = i, j+1
			}
		}
		if depth <= 0 {
			return end, endCol
		}
	}
	return end, endCol
}

// replaceFile writes a file whole or not at all: a crash halfway through
// leaves the old file, not half of the new one. A symlink is written through,
// so a config.toml linked in from a dotfiles repository stays linked, and the
// file keeps its permissions.
func replaceFile(path string, data []byte) error {
	if target, err := filepath.EvalSymlinks(path); err == nil {
		path = target
	}
	mode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".config.toml.*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), mode); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return err
	}
	slog.Info("config written", "path", path)
	return nil
}
