package ssh_test

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/adapter/ssh"
)

// exitErr is what exec.CommandContext returns for a command that failed with
// a status: the same error, of the same type, that a failed ssh gives.
func exitErr(t *testing.T, code string) error {
	t.Helper()
	err := exec.Command("sh", "-c", "exit "+code).Run()
	if err == nil {
		t.Fatalf("sh -c 'exit %s' succeeded", code)
	}
	return err
}

// What ssh says is not what the reader needs to know. Each of its usual
// failures is one line naming the cause and what to do about it.
func TestClassifyNamesTheCauseAndTheRemedy(t *testing.T) {
	status255 := exitErr(t, "255")
	for _, tc := range []struct {
		name, stderr, want string
		err                error
	}{
		{"host key", "Host key verification failed.", "run `ssh router` once", status255},
		{"auth", "hans@desk: Permission denied (publickey,password).", "key auth", status255},
		{"dns", "ssh: Could not resolve hostname nope: Name or service not known", "does not resolve", status255},
		{"timeout", "ssh: connect to host x port 22: Connection timed out", "no answer on port 22", status255},
		{"timeout on macos", "ssh: connect to host x port 22: Operation timed out", "no answer on port 22", status255},
		{"refused", "ssh: connect to host x port 22: Connection refused", "nothing is listening", status255},
		{"git host", "Invalid command: revier list --json\n  You appear to be using ssh to clone a git:// URL.", "git host", exitErr(t, "1")},
		{"git host that says so", "You've successfully authenticated, but GitHub does not provide shell access.", "git host", exitErr(t, "1")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := ssh.Classify("router", []string{"revier", "list", "--json"}, tc.stderr, tc.err).Error()
			if !strings.HasPrefix(got, "router: ") || !strings.Contains(got, tc.want) {
				t.Errorf("classify = %q, want the host and %q", got, tc.want)
			}
			if strings.Contains(got, "revier list --json") && tc.name != "git host" {
				t.Errorf("classify = %q, want no command in a known failure", got)
			}
		})
	}
}

// A shell that cannot find revier says so in its own words, which differ by
// shell and by platform. The status it exits with does not.
func TestClassifySeesTheShellCannotFindRevier(t *testing.T) {
	for _, tc := range []struct {
		name, stderr string
		err          error
	}{
		{"by status", "", exitErr(t, "127")},
		{"zsh", "zsh:1: command not found: revier", exitErr(t, "127")},
		{"bash", "bash: line 1: revier: command not found", exitErr(t, "127")},
		{"powershell", "The term 'revier' is not recognized as a name of a cmdlet.", exitErr(t, "1")},
		{"dash", "sh: 1: revier: not found", exitErr(t, "127")},
		{"fish", "fish: Unknown command: revier", exitErr(t, "127")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := ssh.Classify("box", nil, tc.stderr, tc.err).Error()
			if !strings.Contains(got, "PATH ssh gives") {
				t.Errorf("classify = %q, want the PATH named", got)
			}
		})
	}
}

// A revier that ran and could not find a command of its own is not a revier
// missing from the PATH: its words are passed through.
func TestClassifyDoesNotReadAnotherMissingCommandAsRevier(t *testing.T) {
	for _, tc := range []struct {
		name, stderr string
		err          error
	}{
		{"exit 1", "probe: sh: mise: command not found", exitErr(t, "1")},
		{"exit 127", "sh: 1: mise: not found", exitErr(t, "127")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := ssh.Classify("box", []string{"revier", "list"}, tc.stderr, tc.err).Error()
			if strings.Contains(got, "PATH ssh gives") || !strings.Contains(got, "mise") {
				t.Errorf("classify = %q, want the remote's own words passed through", got)
			}
		})
	}
}

// A failure nothing here knows is passed through whole: the command that was
// run, and what it said, on one line.
func TestClassifyPassesAnUnknownFailureThrough(t *testing.T) {
	got := ssh.Classify("box", []string{"revier", "list"}, "config error: bad toml\nline 3", exitErr(t, "2")).Error()
	want := "box: revier list: config error: bad toml; line 3"
	if got != want {
		t.Errorf("classify = %q, want %q", got, want)
	}
}

// ssh's own failures are the ones it exits 255 with. A remote revier that
// says "permission denied" about a file of its own is not a refused key.
func TestClassifyDoesNotReadTheRemotesWordsAsSshsOwn(t *testing.T) {
	got := ssh.Classify("box", []string{"revier", "list"}, "open /etc/revier: permission denied", exitErr(t, "1")).Error()
	if strings.Contains(got, "key auth") {
		t.Errorf("classify = %q, want the remote's own words passed through", got)
	}
}
