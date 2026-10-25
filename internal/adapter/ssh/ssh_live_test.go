//go:build live

// Layer L4: the command lines the adapter builds, run through a real POSIX
// shell on this machine, with no ssh and no host.
package ssh_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
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
	line := onTheHost(t, ssh.PanelCommand("buildbox", "it's far", "agent", ""))
	host, _ := os.Hostname()
	want := regexp.MustCompile(`^-lc revier agent exec -p 'it'\\''s far' --tag ` + regexp.QuoteMeta(host) + `\.\d+ --resume abc-123\n$`)
	if !want.Match(line) {
		t.Errorf("on the host it runs %q, want %s", line, want)
	}
}

// A named agent reaches the host as one word, also a name that needs quoting.
func TestAPanelCommandCarriesTheNamedAgentAsOneWord(t *testing.T) {
	line := onTheHost(t, ssh.PanelCommand("buildbox", "far", "agent", "it's dtop"))
	want := regexp.MustCompile(`^-lc revier agent exec -p far --agent 'it'\\''s dtop' --tag \S+ --resume abc-123\n$`)
	if !want.Match(line) {
		t.Errorf("on the host it runs %q, want %s", line, want)
	}
}

// onTheHost is the command line the user's login shell on the host is handed
// by the panel's argv, with --resume abc-123 appended to that argv.
func onTheHost(t *testing.T, argv []string) []byte {
	t.Helper()
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
	return line
}

// ssh runs in a process group of its own, so the ctrl+c a terminal sends to
// revier's group, which cancels a shutdown or a restore after its step, does
// not end the call that step makes. The ssh is a script first on PATH.
func TestSSHRunsInAProcessGroupOfItsOwn(t *testing.T) {
	bin := t.TempDir()
	file := filepath.Join(bin, "pgrp")
	script := "#!/bin/sh\ncut -d' ' -f5 /proc/$$/stat > '" + file + "'\necho '[]'\n"
	if err := os.WriteFile(filepath.Join(bin, "ssh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_RUNTIME_DIR", "")

	_, _ = ssh.New("box").Survey(context.Background(), nil)
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("ssh never ran: %v", err)
	}
	if pgrp, _ := strconv.Atoi(strings.TrimSpace(string(b))); pgrp == 0 || pgrp == syscall.Getpgrp() {
		t.Errorf("ssh's process group = %d, want one other than revier's (%d)", pgrp, syscall.Getpgrp())
	}
}
