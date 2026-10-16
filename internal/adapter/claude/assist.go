package claude

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
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
	// Session is the conversation to resume, as a session restore names it.
	// It wins over Resume.
	Session string
	// Beside is an assistant that runs beside the surface, in a workspace of
	// its own, and not in the terminal the surface handed it. Its window and
	// its row say whose it is, so it gets no status line, and the user's own
	// stays.
	Beside bool
}

// Argv is the command line.
func (a Assist) Argv() []string {
	argv := []string{"claude", "--append-system-prompt", a.Brief}
	if !a.Beside {
		argv = append(argv, "--settings", a.handOverSettings())
	}
	switch {
	case a.Session != "":
		argv = append(argv, "--resume", a.Session)
	case a.Resume:
		argv = append(argv, "--continue")
	}
	for _, dir := range a.Dirs {
		argv = append(argv, "--add-dir", dir)
	}
	return argv
}

// handOverSettings is the status line that frames the hand-over: the surface
// is off the screen, and nothing else there says whose session this is or
// how it ends. The keys it names are Claude Code's own.
func (a Assist) handOverSettings() string {
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
	return string(settings)
}

// shellQuote is s as one word of a POSIX shell.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// HasConversation reports whether the conversation --continue in dir would
// open is one: the newest Claude Code filed for dir. A transcript with no
// turn of the user's is none: Claude Code files one for a session that was
// opened and left, and refuses to continue it, ending at once with status 1.
func HasConversation(dir string) bool {
	home := configDir()
	if home == "" {
		return false
	}
	// The directory is read and not matched as a pattern: a home directory
	// may hold a character a pattern gives a meaning to.
	filed := filepath.Join(home, "projects", projectDir(realDir(dir)))
	entries, _ := os.ReadDir(filed)
	var newest string
	var at time.Time
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		if info, err := e.Info(); err == nil && info.ModTime().After(at) {
			newest, at = e.Name(), info.ModTime()
		}
	}
	return newest != "" && hasUserTurn(filepath.Join(filed, newest))
}

// hasUserTurn reports whether a transcript holds a turn the user typed. A
// line of the user's kind that Claude Code marks meta - a caveat it adds,
// not the user's words - is none.
func hasUserTurn(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()
	lines := bufio.NewScanner(f)
	lines.Buffer(nil, 64<<20) // a turn that pasted a file is one long line
	for lines.Scan() {
		if !bytes.Contains(lines.Bytes(), []byte(`"type":"user"`)) {
			continue
		}
		var line struct {
			Type   string `json:"type"`
			IsMeta bool   `json:"isMeta"`
		}
		if json.Unmarshal(lines.Bytes(), &line) == nil && line.Type == "user" && !line.IsMeta {
			return true
		}
	}
	return false
}

// InUse reports whether a live Claude Code session works in dir. A second
// session that continued the newest conversation there would write the same
// transcript as the first. A listing that fails says none is in use: the
// cost is one shared conversation, and a broken listing is no reason to
// start no agent.
func InUse(ctx context.Context, dir string) bool {
	out, err := claudeAgents(ctx)
	if err != nil {
		return false
	}
	var listed []listedSession
	if err := json.Unmarshal(out, &listed); err != nil {
		return false
	}
	dir = realDir(dir)
	return slices.ContainsFunc(listed, func(s listedSession) bool {
		return s.PID != 0 && s.Cwd != "" && realDir(s.Cwd) == dir
	})
}

// realDir is dir as the kernel names it: Claude Code files and lists a
// session under that name, so a link on the way to dir is followed first.
func realDir(dir string) string {
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		return real
	}
	return dir
}
