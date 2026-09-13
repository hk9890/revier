package tui

import (
	"errors"
	"strings"
)

// An action's command is typed on one line and stored as an argv. The line is
// split the way a shell splits words, without anything else a shell does:
// whitespace separates words, quotes group them, and a backslash outside
// single quotes takes the next character as it is. A template, {{ .Path }},
// is one word however many spaces it holds, so it needs no quotes.

// splitCommand is a typed command line as the words of an argv.
func splitCommand(line string) ([]string, error) {
	var (
		words  []string
		word   strings.Builder
		inWord bool
		quote  byte
		depth  int // open {{ template braces
	)
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case quote == '\'':
			if c == '\'' {
				quote = 0
			} else {
				word.WriteByte(c)
			}
		case quote == '"':
			switch {
			case c == '"':
				quote = 0
			case c == '\\' && i+1 < len(line) && (line[i+1] == '"' || line[i+1] == '\\'):
				i++
				word.WriteByte(line[i])
			default:
				word.WriteByte(c)
			}
		case depth > 0:
			if strings.HasPrefix(line[i:], "}}") {
				depth--
				word.WriteString("}}")
				i++
			} else {
				word.WriteByte(c)
			}
		case c == ' ' || c == '\t':
			if inWord {
				words = append(words, word.String())
				word.Reset()
				inWord = false
			}
		case c == '\'' || c == '"':
			quote, inWord = c, true
		case c == '\\' && i+1 < len(line):
			i++
			word.WriteByte(line[i])
			inWord = true
		case strings.HasPrefix(line[i:], "{{"):
			depth++
			word.WriteString("{{")
			i++
			inWord = true
		default:
			word.WriteByte(c)
			inWord = true
		}
	}
	switch {
	case quote != 0:
		return nil, errors.New("the command has a quote that is not closed")
	case depth > 0:
		return nil, errors.New("the command has a {{ that is not closed")
	}
	if inWord {
		words = append(words, word.String())
	}
	return words, nil
}

// joinCommand is an argv as a line splitCommand reads back as the same argv.
// A word is quoted only when it would not come back as itself.
func joinCommand(argv []string) string {
	out := make([]string, len(argv))
	for i, arg := range argv {
		if words, err := splitCommand(arg); err == nil && len(words) == 1 && words[0] == arg {
			out[i] = arg
		} else if !strings.Contains(arg, "'") {
			out[i] = "'" + arg + "'"
		} else {
			out[i] = `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(arg) + `"`
		}
	}
	return strings.Join(out, " ")
}
