package claude

import "encoding/json"

// assistStatus is the line Claude Code draws under its prompt for as long as
// it is revier's assistant. It is the frame around the hand-over: the surface
// is off the screen, and nothing else there says whose session this is or
// how it ends. The keys it names are Claude Code's own.
const assistStatus = `printf '\033[7m %s \033[0m' 'revier assist  ·  /exit, or ctrl+c twice, returns to revier'`

// AssistArgv is the command line that starts Claude Code as revier's
// assistant: the brief appended to its own system prompt, the status line
// that says how to return, and each directory besides the one it starts in
// that it may read and write.
func AssistArgv(brief string, dirs ...string) []string {
	settings, err := json.Marshal(map[string]any{
		"statusLine": map[string]string{"type": "command", "command": assistStatus},
	})
	if err != nil {
		panic(err) // a map of strings always marshals
	}
	argv := []string{"claude", "--append-system-prompt", brief, "--settings", string(settings)}
	for _, dir := range dirs {
		argv = append(argv, "--add-dir", dir)
	}
	return argv
}
