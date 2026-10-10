package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Assist is Claude Code started as revier's assistant.
type Assist struct {
	// Brief is appended to Claude Code's own system prompt.
	Brief string
	// Subject is what the user started the session from, for the status
	// line: a project's name, or nothing.
	Subject string
	// Dirs are the directories besides the one it starts in that it may read
	// and write.
	Dirs []string
	// Resume continues the newest conversation of the directory it starts
	// in. Claude Code refuses the flag where there is none: HasConversation
	// says whether there is.
	Resume bool
}

// Argv is the command line.
//
// The status line is the frame around the hand-over: the surface is off the
// screen, and nothing else there says whose session this is or how it ends.
// The keys it names are Claude Code's own.
func (a Assist) Argv() []string {
	status := "revier assist  ·  "
	if a.Subject != "" {
		status += a.Subject + "  ·  "
	}
	status += "/exit, or ctrl+c twice, returns to revier"
	settings, err := json.Marshal(map[string]any{
		"statusLine": map[string]string{
			"type":    "command",
			"command": `printf '\033[7m %s \033[0m' ` + shellQuote(status),
		},
	})
	if err != nil {
		panic(err) // a map of strings always marshals
	}
	argv := []string{"claude", "--append-system-prompt", a.Brief, "--settings", string(settings)}
	if a.Resume {
		argv = append(argv, "--continue")
	}
	for _, dir := range a.Dirs {
		argv = append(argv, "--add-dir", dir)
	}
	return argv
}

// shellQuote is s as one word of a POSIX shell.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// HasConversation reports whether Claude Code has filed a conversation that
// was held in dir, which is what --continue there continues.
func HasConversation(dir string) bool {
	home := configDir()
	if home == "" {
		return false
	}
	// Claude Code files a session under the directory as the kernel names
	// it, so a link on the way to dir is followed first.
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		dir = real
	}
	// The directory is read and not matched as a pattern: a home directory
	// may hold a character a pattern gives a meaning to.
	filed, _ := os.ReadDir(filepath.Join(home, "projects", projectDir(dir)))
	return slices.ContainsFunc(filed, func(f os.DirEntry) bool {
		return !f.IsDir() && strings.HasSuffix(f.Name(), ".jsonl")
	})
}
