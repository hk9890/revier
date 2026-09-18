package ssh_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/adapter/ssh"
	"github.com/hk9890/revier/pkg/revier"
)

// record replaces ssh with a function that keeps the argv and answers with
// out, or fails with err.
func record(r *ssh.Remote, out string, err error) *[][]string {
	var calls [][]string
	r.SetRunner(func(_ context.Context, args ...string) ([]byte, []byte, error) {
		calls = append(calls, args)
		return []byte(out), nil, err
	})
	return &calls
}

func TestSurveyAsksForTheNamedProjectsInOneCall(t *testing.T) {
	r := ssh.New("buildbox")
	calls := record(r, `[{"project":{"name":"demo","path":"/x"},"running":true,"path_exists":true,"targets":[]}]`, nil)

	views, err := r.Survey(context.Background(), []revier.ProjectName{"demo", "other"})
	if err != nil {
		t.Fatalf("Survey: %v", err)
	}
	want := []string{"revier", "list", "--json", "demo", "other"}
	if len(*calls) != 1 || !slices.Equal((*calls)[0], want) {
		t.Errorf("ran %v, want one call %v", *calls, want)
	}
	if len(views) != 1 || views[0].Project.Name != "demo" || !views[0].Running {
		t.Errorf("views = %+v, want demo running", views)
	}
}

// A save asks the same question with each agent's conversation in the answer.
func TestConversationsAsksTheSurveyForTheConversations(t *testing.T) {
	r := ssh.New("buildbox")
	calls := record(r, `[{"project":{"name":"demo"},"targets":[],"agents":[{"panel":"box.7","state":{"harness":"claude","status":"idle"},"conversation":{"id":"abc-123","dir":"/x/wt"}}]}]`, nil)

	views, err := r.Conversations(context.Background(), []revier.ProjectName{"demo"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"revier", "list", "--json", "--conversations", "demo"}
	if len(*calls) != 1 || !slices.Equal((*calls)[0], want) {
		t.Errorf("ran %v, want one call %v", *calls, want)
	}
	if c := views[0].Agents[0].Conversation; c == nil || c.ID != "abc-123" || c.Dir != "/x/wt" {
		t.Errorf("conversation = %+v, want abc-123 in /x/wt", c)
	}
}

func TestSurveyReportsTheHostOnBadJSON(t *testing.T) {
	r := ssh.New("buildbox")
	record(r, "not json", nil)
	_, err := r.Survey(context.Background(), []revier.ProjectName{"demo"})
	if err == nil || !strings.Contains(err.Error(), "buildbox") {
		t.Errorf("err = %v, want one naming the host", err)
	}
}

func TestSurveyPassesTheSSHFailureThrough(t *testing.T) {
	r := ssh.New("buildbox")
	record(r, "", errors.New("buildbox: connection refused"))
	_, err := r.Survey(context.Background(), []revier.ProjectName{"demo"})
	if err == nil || !strings.Contains(err.Error(), "connection refused") {
		t.Errorf("err = %v, want the ssh failure", err)
	}
}

// sshd runs a command in a shell that read no profile, where a PATH set in
// one - the directory Claude Code installs into - is missing. The command
// line runs in the user's login shell instead, as one word. The shell is
// $SHELL, which sshd sets, and not ${SHELL:-sh}: the line is parsed by that
// login shell, and fish rejects the braces.
func TestACommandRunsInTheLoginShell(t *testing.T) {
	got := ssh.Login("revier list --json 'my project'")
	want := `exec "$SHELL" -lc 'revier list --json '\''my project'\'''`
	if got != want {
		t.Errorf("login = %s, want %s", got, want)
	}
}

// A profile that writes to stdout - a greeting, a version manager's notice -
// would precede the JSON. What the command reads back is the command's own
// stdout alone, and the profile's goes to stderr.
func TestAReadCommandKeepsTheProfileOutOfItsStdout(t *testing.T) {
	line := ssh.Quiet("revier list --json far")
	if want := ssh.Login("revier list --json far >&3") + " 3>&1 1>&2"; line != want {
		t.Fatalf("quiet = %s, want %s", line, want)
	}
	home := t.TempDir()
	if err := os.WriteFile(home+"/.profile", []byte("echo welcome\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A login shell that prints from its profile, then runs the line.
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

// An action runs on the host with this terminal, so the argv asks ssh for a
// tty and carries the action and the project as one word each.
func TestRunCommandAsksForATTYAndQuotesItsWords(t *testing.T) {
	argv := ssh.New("buildbox").RunCommand("far", "pull all")
	want := append(append([]string{"ssh", "-t"}, ssh.Options()...), "--", "buildbox", ssh.Login("revier run 'pull all' -p far"))
	if !slices.Equal(argv, want) {
		t.Errorf("argv = %q, want %q", argv, want)
	}
}

// The remote shell joins ssh's words back into a line, so a word with a
// space, a quote or a shell character has to reach revier there as one
// argument.
func TestQuoteKeepsAWordWhole(t *testing.T) {
	cases := map[string]string{
		"demo":           "demo",
		"--json":         "--json",
		"run the tests":  "'run the tests'",
		"it's":           `'it'\''s'`,
		"a;rm -rf /":     "'a;rm -rf /'",
		"$HOME":          "'$HOME'",
		"":               "''",
		"demo:home":      "demo:home",
		"C#":             "'C#'",
		"x=y":            "x=y",
		"~/dev":          "'~/dev'",
		"say \"hi\" now": `'say "hi" now'`,
		"back\\slash":    `'back\slash'`,
		"user@host":      "user@host",
		"a\nb":           "'a\nb'",
		"glob*":          "'glob*'",
		"bang!":          "'bang!'",
		"back`tick":      "'back`tick'",
	}
	for in, want := range cases {
		if got := ssh.Quote(in); got != want {
			t.Errorf("quote(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestOptionsNeverAskForAPassword(t *testing.T) {
	opts := strings.Join(ssh.Options(), " ")
	if !strings.Contains(opts, "BatchMode=yes") || !strings.Contains(opts, "ConnectTimeout=") {
		t.Errorf("options = %q, want BatchMode and a connect timeout", opts)
	}
}

// The panel's command is a shell that becomes the ssh, so the pid it wrote
// into the tag is the pid the runtime reports for the panel. What runs on the
// host runs in the login shell there, and reaches it through three shells
// with a name that needs quoting still one word.
func TestAPanelCommandTagsWhatItStartsWithItsOwnPid(t *testing.T) {
	argv := ssh.PanelCommand("buildbox", "it's far", "agent")
	if len(argv) != 4 || argv[0] != "sh" || argv[1] != "-c" || !strings.HasPrefix(argv[2], "exec ssh -t ") {
		t.Fatalf("argv = %q, want sh -c 'exec ssh -t ...' sh", argv)
	}
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
