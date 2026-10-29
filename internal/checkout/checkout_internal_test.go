package checkout

import "testing"

// git ends a failed clone with a hint about access rights, so the cause is
// the "fatal:" line and, for ssh, the line before it.
func TestGitCauseNamesWhatFailed(t *testing.T) {
	for _, c := range []struct{ name, stderr, want string }{
		{
			"ssh",
			"Cloning into '/p'...\nssh: Could not resolve hostname h: Name or service not known\r\nfatal: Could not read from remote repository.\n\nPlease make sure you have the correct access rights\nand the repository exists.\n",
			"ssh: Could not resolve hostname h: Name or service not known; fatal: Could not read from remote repository.",
		},
		{
			"git alone",
			"Cloning into '/p'...\nfatal: repository '/absent.git' does not exist\n",
			"fatal: repository '/absent.git' does not exist",
		},
		{"no fatal line", "Cloning into '/p'...\nerror: something\n", "error: something"},
		{"no output", "", ""},
	} {
		if got := gitCause(c.stderr); got != c.want {
			t.Errorf("%s: gitCause = %q, want %q", c.name, got, c.want)
		}
	}
}
