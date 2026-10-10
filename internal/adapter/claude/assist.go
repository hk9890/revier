package claude

// AssistArgv is the command line that starts Claude Code as revier's
// assistant: the brief appended to its own system prompt, and each directory
// besides the one it starts in that it may read and write.
func AssistArgv(brief string, dirs ...string) []string {
	argv := []string{"claude", "--append-system-prompt", brief}
	for _, dir := range dirs {
		argv = append(argv, "--add-dir", dir)
	}
	return argv
}
