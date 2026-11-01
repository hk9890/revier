package ssh_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/adapter/ssh"
)

// fishWords splits a line into words as fish reads them, by the rules of
// fish's documentation, section Quotes, for what the adapter writes: inside
// single quotes only \' and \\ are escapes; inside double quotes only \",
// \$, \\ and a backslash before a newline are, and $NAME expands; outside
// quotes a backslash makes the character after it literal. $SHELL expands to
// shell; any other expansion is an error, as is a quote left open.
// ssh_live_test.go runs the lines through every POSIX shell installed; this
// stands in for fish where none is.
func fishWords(line, shell string) ([]string, error) {
	var words []string
	var word strings.Builder
	inWord := false
	expand := func(rest string) (int, error) {
		if !strings.HasPrefix(rest, "$SHELL") {
			return 0, fmt.Errorf("expansion at %q", rest)
		}
		word.WriteString(shell)
		return len("$SHELL"), nil
	}
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch c {
		case ' ', '\t', '\n':
			if inWord {
				words = append(words, word.String())
				word.Reset()
				inWord = false
			}
			continue
		case '\\':
			if i+1 == len(line) {
				return nil, fmt.Errorf("line ends in a backslash")
			}
			i++
			if line[i] != '\n' {
				word.WriteByte(line[i])
			}
		case '\'':
			for i++; ; i++ {
				if i == len(line) {
					return nil, fmt.Errorf("single quote left open")
				}
				if line[i] == '\'' {
					break
				}
				if line[i] == '\\' && i+1 < len(line) && (line[i+1] == '\'' || line[i+1] == '\\') {
					i++
				}
				word.WriteByte(line[i])
			}
		case '"':
			for i++; ; i++ {
				if i == len(line) {
					return nil, fmt.Errorf("double quote left open")
				}
				if line[i] == '"' {
					break
				}
				if line[i] == '$' {
					n, err := expand(line[i:])
					if err != nil {
						return nil, err
					}
					i += n - 1
					continue
				}
				if line[i] == '\\' && i+1 < len(line) && strings.IndexByte("\"$\\\n", line[i+1]) >= 0 {
					i++
					if line[i] == '\n' {
						continue
					}
				}
				word.WriteByte(line[i])
			}
		case '$':
			n, err := expand(line[i:])
			if err != nil {
				return nil, err
			}
			i += n - 1
		default:
			word.WriteByte(c)
		}
		inWord = true
	}
	if inWord {
		words = append(words, word.String())
	}
	return words, nil
}

// Under a fish login shell on the host, the line passes through fish twice:
// once as sshd hands it to the login shell, once as $SHELL -lc. Each name
// still reaches revier there as the one word it was.
func TestAnAwkwardNameReachesTheHostAsOneWordUnderFish(t *testing.T) {
	names := []string{"it's far", "back`tick", `back\slash`, `trail\`, `say "hi"`, "$HOME", "a  b", `'\''`}
	args := append([]string{"revier", "list", "--json"}, names...)
	words := make([]string, len(args))
	for i, a := range args {
		words[i] = ssh.Quote(a)
	}
	first, err := fishWords(ssh.Login(strings.Join(words, " ")), "fish")
	if err != nil {
		t.Fatalf("the login shell: %v", err)
	}
	if len(first) != 4 || first[0] != "exec" || first[1] != "fish" || first[2] != "-lc" {
		t.Fatalf("the login shell runs %q, want exec fish -lc and one word", first)
	}
	got, err := fishWords(first[3], "fish")
	if err != nil {
		t.Fatalf("$SHELL -lc: %v", err)
	}
	if !slices.Equal(got, args) {
		t.Errorf("revier on the host got %q, want %q", got, args)
	}
}
