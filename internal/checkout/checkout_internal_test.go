package checkout

import "testing"

// git ends a failed clone with a hint about access rights, so the cause is
// the "fatal:" line, after the line before it, where ssh, the forge or curl
// says what went wrong.
func TestGitCauseNamesWhatFailed(t *testing.T) {
	for _, c := range []struct {
		name, stderr, want string
		fatal              bool
	}{
		{
			"ssh",
			"Cloning into '/p'...\nssh: Could not resolve hostname h: Name or service not known\r\nfatal: Could not read from remote repository.\n\nPlease make sure you have the correct access rights\nand the repository exists.\n",
			"ssh: Could not resolve hostname h: Name or service not known; fatal: Could not read from remote repository.", true,
		},
		{
			"https",
			"Cloning into '/p'...\nerror: RPC failed; HTTP 500 curl 22 The requested URL returned error: 500\nfatal: the remote end hung up unexpectedly\n",
			"error: RPC failed; HTTP 500 curl 22 The requested URL returned error: 500; fatal: the remote end hung up unexpectedly", true,
		},
		{
			"git alone",
			"Cloning into '/p'...\nfatal: repository '/absent.git' does not exist\n",
			"fatal: repository '/absent.git' does not exist", true,
		},
		{"no fatal line", "Cloning into '/p'...\nerror: something\n", "error: something", false},
		{"interrupted", "Cloning into '/p'...\nReceiving objects:  37% (370/1000)\r", "Receiving objects:  37% (370/1000)", false},
		{"no output", "", "", false},
	} {
		if got, fatal := gitCause(c.stderr); got != c.want || fatal != c.fatal {
			t.Errorf("%s: gitCause = %q, %v; want %q, %v", c.name, got, fatal, c.want, c.fatal)
		}
	}
}
