// Package ssh implements a Remote over an ssh command line: the revier on
// another machine is run there, and its answers read back.
//
// It is the only adapter that speaks to another revier, and the JSON that
// `revier list --json` prints is the whole contract between the two. Nothing
// about the remote machine's terminal, its tmux or its agents is interpreted
// here; the revier there did that.
package ssh

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/hk9890/revier/pkg/revier"
)

// Remote is a revier reached over ssh. Host is the destination as ssh
// resolves it, so an alias in ~/.ssh/config works and so does user@machine.
// The zero value is unusable; construct it with New.
type Remote struct {
	host string
	run  func(ctx context.Context, args ...string) ([]byte, error)
}

// New returns a remote for the host.
func New(host string) *Remote {
	r := &Remote{host: host}
	r.run = r.exec
	return r
}

func (r *Remote) Name() string { return r.host }

// options are the flags every call carries. BatchMode refuses to ask for a
// password, which a survey in the TUI could not answer; the connect timeout
// bounds a host that is down, where ssh would otherwise wait for the kernel
// to give up.
var options = []string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=5"}

// exec runs one command on the remote and returns its stdout. The remote
// shell joins the words ssh is given back into one line, so each is quoted
// here to reach the remote revier as the one argument it was.
func (r *Remote) exec(ctx context.Context, args ...string) ([]byte, error) {
	words := make([]string, len(args))
	for i, a := range args {
		words[i] = quote(a)
	}
	full := append(append([]string{}, options...), "--", r.host, strings.Join(words, " "))
	var out, errb bytes.Buffer
	c := exec.CommandContext(ctx, "ssh", full...)
	c.Stdout, c.Stderr = &out, &errb
	if err := c.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("%s: %s: %s", r.host, strings.Join(args, " "), msg)
	}
	return out.Bytes(), nil
}

// quote wraps s for a POSIX shell: single quotes, with each single quote in s
// closed, escaped and reopened.
func quote(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\n'\"\\$`!#&|;<>(){}[]*?~") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// RunCommand is the ssh that runs the action on the remote with this
// terminal: -t asks for a tty there, so an action that prompts or draws
// works as it would in a shell on the host.
func (r *Remote) RunCommand(project revier.ProjectName, action string) []string {
	remote := strings.Join([]string{"revier", "run", quote(action), "-p", quote(string(project))}, " ")
	return append(append([]string{"ssh", "-t"}, options...), "--", r.host, remote)
}

// Survey asks the remote revier for the named projects, in one call.
func (r *Remote) Survey(ctx context.Context, names []revier.ProjectName) ([]revier.ProjectView, error) {
	args := []string{"revier", "list", "--json"}
	for _, n := range names {
		args = append(args, string(n))
	}
	out, err := r.run(ctx, args...)
	if err != nil {
		return nil, err
	}
	var views []revier.ProjectView
	if err := json.Unmarshal(out, &views); err != nil {
		return nil, fmt.Errorf("%s: revier list --json: %w", r.host, err)
	}
	return views, nil
}

// Prompt runs `revier agent prompt` on the remote.
func (r *Remote) Prompt(ctx context.Context, address, text string) error {
	_, err := r.run(ctx, "revier", "agent", "prompt", address, "--", text)
	return err
}

// Wait runs `revier agent wait` on the remote, with no timeout of its own:
// ctx ending ends the ssh, and with it the wait.
func (r *Remote) Wait(ctx context.Context, address, until string) (revier.Status, error) {
	out, err := r.run(ctx, "revier", "agent", "wait", address, "--until", until)
	if err != nil {
		return revier.StatusUnknown, err
	}
	var s revier.Status
	word, _ := json.Marshal(strings.TrimSpace(string(out)))
	if err := json.Unmarshal(word, &s); err != nil {
		return revier.StatusUnknown, fmt.Errorf("%s: revier agent wait: %w", r.host, err)
	}
	return s, nil
}
