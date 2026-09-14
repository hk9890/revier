package ssh_test

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hk9890/revier/internal/adapter/ssh"
	"github.com/hk9890/revier/pkg/revier"
)

// record replaces ssh with a function that keeps the argv and answers with
// out, or fails with err.
func record(r *ssh.Remote, out string, err error) *[][]string {
	var calls [][]string
	r.SetRun(func(_ context.Context, args ...string) ([]byte, []byte, error) {
		calls = append(calls, args)
		return []byte(out), nil, err
	})
	return &calls
}

// A local deadline is the remote wait's timeout too: ending the ssh alone
// would leave `revier agent wait` polling on the host.
func TestWaitCarriesTheDeadlineAsTheRemoteTimeout(t *testing.T) {
	r := ssh.New("buildbox")
	calls := record(r, "running\n", nil)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := r.Wait(ctx, "demo", "stopped"); err != nil {
		t.Fatal(err)
	}
	args := (*calls)[0]
	i := slices.Index(args, "--timeout")
	if i < 0 || i+1 >= len(args) {
		t.Fatalf("ran %v, want --timeout <seconds>", args)
	}
	secs, err := strconv.ParseFloat(args[i+1], 64)
	if err != nil || secs <= 0 || secs > 30 {
		t.Errorf("--timeout %q, want the seconds left of 30", args[i+1])
	}
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

func TestPromptForwardsTheAddressAndTheText(t *testing.T) {
	r := ssh.New("buildbox")
	calls := record(r, "", nil)
	if err := r.Prompt(context.Background(), "demo:home", "-run the tests"); err != nil {
		t.Fatal(err)
	}
	want := []string{"revier", "agent", "prompt", "demo:home", "--", "-run the tests"}
	if len(*calls) != 1 || !slices.Equal((*calls)[0], want) {
		t.Errorf("ran %v, want %v", *calls, want)
	}
}

// A tab is opened by the revier on the host, for the address as the host
// knows it; a resume goes with an agent only when there is one.
func TestNewAgentAndNewShellRunTheCommandsOnTheHost(t *testing.T) {
	r := ssh.New("buildbox")
	calls := record(r, "", nil)
	ctx := context.Background()
	if err := r.NewAgent(ctx, "demo:home", "abc-123"); err != nil {
		t.Fatal(err)
	}
	if err := r.NewAgent(ctx, "demo", ""); err != nil {
		t.Fatal(err)
	}
	if err := r.NewShell(ctx, "demo"); err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"revier", "agent", "new", "-p", "demo:home", "--resume", "abc-123"},
		{"revier", "agent", "new", "-p", "demo"},
		{"revier", "shell", "new", "-p", "demo"},
	}
	if len(*calls) != len(want) {
		t.Fatalf("ran %v, want %v", *calls, want)
	}
	for i := range want {
		if !slices.Equal((*calls)[i], want[i]) {
			t.Errorf("call %d ran %v, want %v", i, (*calls)[i], want[i])
		}
	}
}

// An agent is focused by the revier on the host, for the address as the host
// knows it.
func TestFocusAgentRunsTheCommandOnTheHost(t *testing.T) {
	r := ssh.New("buildbox")
	calls := record(r, "", nil)
	if err := r.FocusAgent(context.Background(), "demo:7"); err != nil {
		t.Fatal(err)
	}
	want := []string{"revier", "agent", "focus", "demo:7"}
	if len(*calls) != 1 || !slices.Equal((*calls)[0], want) {
		t.Errorf("ran %v, want %v", *calls, want)
	}
}

func TestWaitReadsTheStatusTheRemotePrinted(t *testing.T) {
	r := ssh.New("buildbox")
	calls := record(r, "attention\n", nil)
	s, err := r.Wait(context.Background(), "demo", "stopped")
	if err != nil {
		t.Fatal(err)
	}
	if s != revier.StatusAttention {
		t.Errorf("status = %s, want attention", s)
	}
	want := []string{"revier", "agent", "wait", "demo", "--until", "stopped"}
	if len(*calls) != 1 || !slices.Equal((*calls)[0], want) {
		t.Errorf("ran %v, want %v", *calls, want)
	}
}

// An action runs on the host with this terminal, so the argv asks ssh for a
// tty and carries the action and the project as one word each.
func TestRunCommandAsksForATTYAndQuotesItsWords(t *testing.T) {
	argv := ssh.New("buildbox").RunCommand("far", "pull all")
	want := append(append([]string{"ssh", "-t"}, ssh.Options()...), "--", "buildbox", "revier run 'pull all' -p far")
	if !slices.Equal(argv, want) {
		t.Errorf("argv = %q, want %q", argv, want)
	}
}

func TestWaitRefusesAWordThatIsNoStatus(t *testing.T) {
	r := ssh.New("buildbox")
	record(r, "bash: revier: command not found\n", nil)
	if _, err := r.Wait(context.Background(), "demo", "idle"); err == nil {
		t.Error("want an error for a word that is no status")
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
