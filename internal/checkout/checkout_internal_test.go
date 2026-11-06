package checkout

import "testing"

// git ends a failed clone with a hint about access rights, so the cause is
// the "fatal:" line after the line before it, or that line before alone when
// git's own names no cause.
func TestGitCauseNamesWhatFailed(t *testing.T) {
	for _, c := range []struct {
		name, stderr, want string
		fatal              bool
	}{
		{
			"ssh",
			"Cloning into '/p'...\nssh: Could not resolve hostname h: Name or service not known\r\nfatal: Could not read from remote repository.\n\nPlease make sure you have the correct access rights\nand the repository exists.\n",
			"ssh: Could not resolve hostname h: Name or service not known", true,
		},
		{
			"forge over ssh",
			"Cloning into '/p'...\nERROR: Repository not found.\nfatal: Could not read from remote repository.\n\nPlease make sure you have the correct access rights\nand the repository exists.\n",
			"ERROR: Repository not found.", true,
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

// The hint to correct git_url follows a cause that says the repository is
// missing or shut, and no other: a network that is down is not the URL's.
func TestUnreachableIsTheRepositoryNotTheNetwork(t *testing.T) {
	for cause, want := range map[string]bool{
		"ERROR: Repository not found.":                                        true,
		"fatal: repository '/absent.git' does not exist":                      true,
		"git@github.com: Permission denied (publickey).":                      true,
		"fatal: Authentication failed for 'https://h/o/r/'":                   true,
		"ssh: connect to host h port 22: Network is unreachable":              false,
		"fatal: unable to access 'https://h/o/r/': Could not resolve host: h": false,
	} {
		if got := unreachable(cause); got != want {
			t.Errorf("unreachable(%q) = %v, want %v", cause, got, want)
		}
	}
}
