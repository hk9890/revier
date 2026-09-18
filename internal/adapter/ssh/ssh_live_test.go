//go:build live

// Layer L4: the command lines the adapter builds, run through a real POSIX
// shell on this machine, with no ssh and no host.
package ssh_test

import (
	"bytes"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/adapter/ssh"
)

// A login shell that prints from its profile, then runs the read line: the
// JSON alone reaches stdout, and the profile's greeting goes to stderr.
func TestAReadCommandKeepsTheProfileOutOfItsStdoutInAShell(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(home+"/.profile", []byte("echo welcome\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	c := exec.Command("sh", "-c", ssh.Quiet("printf '[]'"))
	c.Env = append(os.Environ(), "SHELL=sh", "HOME="+home)
	c.Stdout, c.Stderr = &out, &errb
	if err := c.Run(); err != nil {
		t.Fatal(err)
	}
	if out.String() != "[]" || !strings.Contains(errb.String(), "welcome") {
		t.Errorf("stdout %q, stderr %q; want the JSON alone on stdout and the profile on stderr", out.String(), errb.String())
	}
}

// What runs on the host runs in the login shell there, and reaches it
// through three shells with a name that needs quoting still one word, and
// the pid in the tag is the shell's own. Trailing arguments pass through.
func TestAPanelCommandTagsWhatItStartsWithItsOwnPid(t *testing.T) {
	argv := ssh.PanelCommand("buildbox", "it's far", "agent")
	argv[2] = strings.Replace(argv[2], "exec ssh -t", "printf '%s\\n'", 1)
	out, err := exec.Command(argv[0], append(argv[1:], "--resume", "abc-123")...).Output()
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	there := lines[len(lines)-1]
	// What sshd hands the user's shell: run it, with a login shell that only
	// prints its command line.
	line, err := exec.Command("sh", "-c", "SHELL=echo; "+strings.TrimPrefix(there, "exec ")).Output()
	if err != nil {
		t.Fatal(err)
	}
	host, _ := os.Hostname()
	want := regexp.MustCompile(`^-lc revier agent exec -p 'it'\\''s far' --tag ` + regexp.QuoteMeta(host) + `\.\d+ --resume abc-123\n$`)
	if !want.Match(line) {
		t.Errorf("on the host it runs %q, want %s", line, want)
	}
}
