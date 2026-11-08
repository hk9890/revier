// Package shell quotes a word so that a POSIX shell and fish read it back as
// that one word.
package shell

import "strings"

// Quote wraps s for a POSIX shell and for fish alike: single quotes, with
// each single quote and each backslash in s put outside them, escaped. Inside
// single quotes a POSIX shell reads a backslash as itself and fish reads `\'`
// and `\\` as escapes; outside them, both read `\'` and `\\` as the quote and
// the backslash.
func Quote(s string) string {
	// A leading = is zsh's EQUALS expansion: =echo becomes echo's path.
	if s != "" && !strings.HasPrefix(s, "=") && !strings.ContainsAny(s, " \t\n'\"\\$`!#&|;<>(){}[]*?~^%") {
		return s
	}
	return "'" + strings.NewReplacer(`'`, `'\''`, `\`, `'\\'`).Replace(s) + "'"
}
