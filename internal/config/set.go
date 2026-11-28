package config

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/hk9890/revier/internal/fsutil"
)

// Set writes one value into config.toml and leaves every other line of the
// file as it was: comments, order and spacing. A TOML encoder cannot do that -
// it writes what it decoded, and a comment is not decoded - so the value is
// put in place of the old one on its own line, under its table, and the table
// is added at the end when the file has none.
//
// The result is parsed and validated as Load does before anything is written,
// and refused for a problem it adds that the file did not have. It is also
// read back: a key the line editor does not find where it looks, such as one
// written as a dotted key or an inline table, would otherwise land twice or
// not at all. That is refused, and the file stays as it was.
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
	// A file that does not decode has no problems to keep, and a write that
	// mends the line it breaks on is how the config screen repairs it.
	was, err := parse(old)
	if err != nil {
		was = &Config{}
	}
	text := setKey(string(old), table, key, literal)
	next, err := parse([]byte(text))
	if err != nil {
		return fmt.Errorf("%s: %s.%s: %w", path, table, key, err)
	}
	if err := problemsAdded(was, next); err != nil {
		return fmt.Errorf("%s: %w", path, err)
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

// keyPart is one part of a TOML key: bare, or quoted either way.
const keyPart = `(?:[A-Za-z0-9_-]+|"(?:[^"\\]|\\.)*"|'[^']*')`

var (
	// headerLine is a table header, [name] or [[name]], with an optional
	// comment after it.
	headerLine = regexp.MustCompile(`^\s*(\[\[?)\s*([A-Za-z0-9_.-]+)\s*\]\]?\s*(#.*)?$`)
	// keyLine is the start of a key's line, up to where its value begins. It
	// takes a quoted and a dotted key too, so the editor reads past their
	// values; the key it captures is as written, so only a bare one is ever
	// the key an edit names.
	keyLine = regexp.MustCompile(`^\s*(` + keyPart + `(?:\s*\.\s*` + keyPart + `)*)\s*=\s*`)
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
		end, _, comments := valueScan(lines, j, at[1])
		for _, c := range comments {
			kept = append(kept, leading(lines[j])+c)
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
// ends on, and the column after its last character.
func valueEnd(lines []string, i, col int) (int, int) {
	end, endCol, _ := valueScan(lines, i, col)
	return end, endCol
}

// valueScan is valueEnd, with each comment on the value's lines. A bracket
// opens a value that runs until it is closed, over as many lines as that
// takes, and so does a multi-line string, basic or literal, which can hold a
// line that reads as a header or a key; a quote hides brackets and comment marks inside it.
func valueScan(lines []string, i, col int) (int, int, []string) {
	depth := 0
	var quote byte
	multi := false // quote opened a """ or ''' string
	end, endCol := i, col
	var comments []string
	for ; i < len(lines); i, col = i+1, 0 {
		line := lines[i]
	scan:
		for j := col; j < len(line); j++ {
			c := line[j]
			switch {
			case quote != 0 && c == '\\' && quote == '"':
				j++
			case quote != 0 && c == quote && !multi:
				quote, end, endCol = 0, i, j+1
			case quote != 0 && c == quote && strings.HasPrefix(line[j:], strings.Repeat(string(c), 3)):
				// A closing run of up to five quotes: the two before the
				// last three are the string's own.
				run := len(line[j:]) - len(strings.TrimLeft(line[j:], string(c)))
				quote, multi, end, endCol = 0, false, i, j+min(run, 5)
				j += min(run, 5) - 1
			case quote != 0:
			case (c == '"' || c == '\'') && strings.HasPrefix(line[j:], strings.Repeat(string(c), 3)):
				quote, multi = c, true
				j += 2
			case c == '"' || c == '\'':
				quote = c
			case c == '#':
				comments = append(comments, strings.TrimSpace(line[j:]))
				break scan
			case c == '[' || c == '{':
				depth++
			case c == ']' || c == '}':
				depth--
				end, endCol = i, j+1
			case c == ' ' || c == '\t' || c == '\r':
			default:
				end, endCol = i, j+1
			}
		}
		if depth <= 0 && !multi {
			return end, endCol, comments
		}
	}
	return end, endCol, comments
}

// replaceFile writes a file whole or not at all: a crash halfway through
// leaves the old file, not half of the new one. A symlink is written through,
// so a config.toml linked in from a dotfiles repository stays linked, and the
// file keeps its permissions. A write that succeeds logs "config written" at
// INFO with path, the file written through any link: config.toml or a
// project's file.
//
// A file whose every line ends in CRLF keeps that: the line editor splits at
// "\n", so a line it adds ends in "\n" alone, and the file would come out
// mixed. A file that is mixed already is written as the editor left it.
func replaceFile(path string, data []byte) error {
	if old, err := os.ReadFile(path); err == nil {
		data = withLineEnds(old, data)
	}
	real, err := fsutil.WriteThrough(path, data)
	if err != nil {
		return err
	}
	slog.Info("config written", "path", real)
	return nil
}

// withLineEnds is data with CRLF line ends when every line of old has them.
func withLineEnds(old, data []byte) []byte {
	if !allCRLF(old) {
		return data
	}
	return bytes.ReplaceAll(bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n")), []byte("\n"), []byte("\r\n"))
}

// allCRLF reports whether a file has line ends and each one is CRLF.
func allCRLF(data []byte) bool {
	lf := bytes.Count(data, []byte("\n"))
	return lf > 0 && bytes.Count(data, []byte("\r\n")) == lf
}
