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
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/hk9890/revier/internal/adapter/ssh"
	"github.com/hk9890/revier/pkg/revier"
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

// awkward are names a user may give a project or an agent that each shell
// on the way reads in its own way: a backtick, a backslash, a trailing
// backslash, quotes, a dollar and spaces.
var awkward = []string{"it's far", "back`tick", `back\slash`, `trail\`, `say "hi"`, "$HOME", "a  b", `'\''`}

// loginShells are the shells installed here that a user's login shell on a
// host may be, by the path sshd would set SHELL to. fish is one when this
// machine has it.
func loginShells(t *testing.T) []string {
	t.Helper()
	var found []string
	for _, name := range []string{"sh", "bash", "dash", "zsh", "fish"} {
		if path, err := exec.LookPath(name); err == nil {
			found = append(found, path)
		}
	}
	if len(found) == 0 {
		t.Skip("no shell installed")
	}
	return found
}

// asTheHost puts a stand-in ssh first on PATH that hands its last word to
// login, as sshd hands the command line to the user's login shell with
// SHELL set to it, and a stand-in revier that writes its arguments to the
// file it returns and answers with an empty list. HOME and ZDOTDIR are
// scratch, so no profile of the user's runs.
func asTheHost(t *testing.T, login string) string {
	t.Helper()
	bin, home := t.TempDir(), t.TempDir()
	argv := filepath.Join(home, "argv")
	for name, script := range map[string]string{
		"ssh":    "#!/bin/sh\nfor a; do last=$a; done\nexec \"$SHELL\" -c \"$last\"\n",
		"revier": "#!/bin/sh\nprintf '%s\\0' \"$@\" > '" + argv + "'\necho '[]'\n",
	} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("SHELL", login)
	t.Setenv("HOME", home)
	t.Setenv("ZDOTDIR", home)
	t.Setenv("XDG_RUNTIME_DIR", "")
	return argv
}

// argsOf reads what the stand-in revier was last run with, and removes the
// file for the next run.
func argsOf(t *testing.T, file string) []string {
	t.Helper()
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("revier never ran on the host: %v", err)
	}
	_ = os.Remove(file)
	return strings.Split(strings.TrimSuffix(string(b), "\x00"), "\x00")
}

// Each name reaches the revier on the host as the one argument it was,
// whichever shell the user logs in with there: the line passes through that
// shell, then through $SHELL -lc, and a POSIX shell and fish each read it.
func TestAnAwkwardNameReachesTheHostAsOneWordInEveryLoginShell(t *testing.T) {
	for _, login := range loginShells(t) {
		t.Run(filepath.Base(login), func(t *testing.T) {
			file := asTheHost(t, login)
			names := make([]revier.ProjectName, len(awkward))
			for i, n := range awkward {
				names[i] = revier.ProjectName(n)
			}
			if _, err := ssh.New("buildbox").Survey(context.Background(), names); err != nil {
				t.Errorf("Survey: %v", err)
			} else if got, want := argsOf(t, file), append([]string{"list", "--json"}, awkward...); !slices.Equal(got, want) {
				t.Errorf("survey: on the host revier got %q, want %q", got, want)
			}

			for _, name := range awkward {
				argv := ssh.New("buildbox").RunCommand(revier.ProjectName(name), name)
				if err := exec.Command(argv[0], argv[1:]...).Run(); err != nil {
					t.Errorf("RunCommand %q: %v", name, err)
				} else if got, want := argsOf(t, file), []string{"run", name, "-p", name}; !slices.Equal(got, want) {
					t.Errorf("run: on the host revier got %q, want %q", got, want)
				}

				argv = ssh.PanelCommand("buildbox", revier.ProjectName(name), "agent", name)
				if err := exec.Command(argv[0], append(argv[1:], "--resume", "abc-123")...).Run(); err != nil {
					t.Errorf("PanelCommand %q: %v", name, err)
					continue
				}
				got := argsOf(t, file)
				want := []string{"agent", "exec", "-p", name, "--agent", name, "--tag", "", "--resume", "abc-123"}
				if len(got) == len(want) {
					want[7] = got[7]
				}
				if !slices.Equal(got, want) || !regexp.MustCompile(`^\S+\.\d+$`).MatchString(want[7]) {
					t.Errorf("panel: on the host revier got %q, want %q with a tag", got, want)
				}
			}
		})
	}
}
