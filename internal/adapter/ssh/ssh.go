// Package ssh implements a Remote over an ssh command line: the revier on
// another machine is run there, and its answers read back.
//
// It is the only adapter that speaks to another revier, and the JSON that
// `revier list --json` and `revier events` print is the whole contract
// between the two. Nothing
// about the remote machine's terminal, its runtime or its agents is interpreted
// here; the revier there did that.
package ssh

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/hk9890/revier/internal/procgroup"
	"github.com/hk9890/revier/internal/shell"
	"github.com/hk9890/revier/pkg/revier"
)

// Remote is a revier reached over ssh. Host is the destination as ssh
// resolves it, so an alias in ~/.ssh/config works and so does user@machine.
// The zero value is unusable; construct it with New.
type Remote struct {
	host string
	run  func(ctx context.Context, args ...string) (stdout, stderr []byte, err error)
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

// alive are the flags that probe the connection, so a network that went away
// ends it within a minute rather than when the kernel gives up. Keepalives
// are the master's: a call over the shared connection is probed by whichever
// call opened it, so every call carries them.
var alive = []string{"-o", "ServerAliveInterval=15", "-o", "ServerAliveCountMax=4"}

// connection are the flags of the connection every call makes: probed, and
// shared with every other call to the host. A survey runs on every refresh,
// and a handshake is a quarter of a second where a call over the master is
// under ten milliseconds. The master outlives the process that opened it by
// ControlPersist, so a popup raised minutes later still surveys over it. The
// socket lives under the runtime directory, which is the user's alone and is
// gone at logout; the path must stay under the socket limit, so its name is
// a hash of the destination. Without the directory - no runtime directory,
// or one that refuses a subdirectory - every call connects on its own: a
// directory under /tmp is not the user's alone.
func connection() []string {
	flags := append([]string{}, alive...)
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		return flags
	}
	dir = filepath.Join(dir, "revier")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return flags
	}
	return append(flags, "-o", "ControlMaster=auto", "-o", "ControlPath="+filepath.Join(dir, "ssh-%C"), "-o", "ControlPersist=10m")
}

// flags are options and connection together: what every call that reads an
// answer carries.
func flags() []string {
	return append(append([]string{}, options...), connection()...)
}

// login wraps a command line in the login shell of the user on the remote.
// sshd runs a command in a shell that reads no profile, so a PATH set there -
// ~/.local/bin, where Claude Code installs - is missing: the remote revier
// then found no `claude`, and every agent read as unknown. sshd always sets
// SHELL, and it is $SHELL and not ${SHELL:-sh} because the line is parsed by
// that login shell, and fish rejects the braces. The line is one word,
// quoted by shell.Quote, which a POSIX shell and fish read alike: the line
// passes through the login shell and then through $SHELL -lc, and a user's
// login shell can be either.
func login(line string) string {
	return `exec "$SHELL" -lc ` + shell.Quote(line)
}

// quiet is login for a command whose output is read: what the profile writes
// on the way - a greeting, a version manager's notice - goes to stderr, and
// only the command's own stdout comes back, else a profile that says one
// word breaks the JSON.
func quiet(line string) string {
	return login(line+" >&3") + " 3>&1 1>&2"
}

// exec runs one command on the remote and returns what it wrote, also when
// it failed: a remote revier can answer in part and still fail. The remote
// shell joins the words ssh is given back into one line, so each is quoted
// here to reach the remote revier as the one argument it was. A failure is
// classified: what ssh or the remote shell said, in one line that says what
// to do about it. A failure the remote revier ended itself is a failedThere.
func (r *Remote) exec(ctx context.Context, args ...string) ([]byte, []byte, error) {
	words := make([]string, len(args))
	for i, a := range args {
		words[i] = shell.Quote(a)
	}
	full := append(flags(), "--", r.host, quiet(strings.Join(words, " ")))
	var out, errb bytes.Buffer
	c := exec.CommandContext(ctx, "ssh", full...)
	c.Stdout, c.Stderr = &out, &errb
	// A call over the shared connection hands its output to the master,
	// which holds it open until the remote command ends, or until the
	// keepalives give a dead network up a minute later. The call is over at
	// its context, so the wait for its output is bounded too.
	c.WaitDelay = waitDelay
	if err := procgroup.Run(c); err != nil {
		failed := classify(ctx, r.host, args, errb.String(), err)
		if status(err) == revierStatus {
			failed = failedThere{failed}
		}
		return out.Bytes(), errb.Bytes(), failed
	}
	return out.Bytes(), errb.Bytes(), nil
}

// revierStatus is the status a remote revier exits with when it fails. ssh
// passes it through. A call that was killed, a connection that dropped and
// a revier that panicked each end with another status.
const revierStatus = 1

// failedThere is a call that the remote revier ended itself, with
// revierStatus: what it wrote is all it had to say.
type failedThere struct{ error }

func (f failedThere) Unwrap() error { return f.error }

// waitDelay is how long a call whose context ended waits for its output to
// close.
const waitDelay = time.Second

// PanelCommand is the command of a link's panel here: `revier <kind> exec`
// on the host, in the login shell there, with this terminal.
//
// The tag names the panel to both machines. It is this machine's name and the
// pid of the ssh, which is the pid the runtime here reports for the panel, so
// the agent the host lists under the tag is found again in the panel that
// shows it, and nothing has to be recorded. The shell that expands the pid is
// replaced by the ssh, which keeps it.
//
// A named agent is passed as `--agent`, quoted for both shells, since the
// name is the user's to choose.
//
// Arguments after the command go to `revier <kind> exec` as they are, which
// is how a restore passes --resume. They cross two shells unquoted, so the
// core passes only words that need no quoting.
//
// The connection is probed, so a network that went away ends the ssh, and
// with it the agent, within a minute rather than when the kernel gives up.
// It goes over the shared master when one is up, so the panel shows as soon
// as its terminal does. It is not in batch mode: a panel can answer a
// prompt.
func (r *Remote) PanelCommand(project revier.ProjectName, kind revier.PanelKind, agent string) []string {
	return PanelCommand(r.host, project, string(kind), agent)
}

// PanelCommand is Remote.PanelCommand for a host, the form the tests pin.
func PanelCommand(host string, project revier.ProjectName, kind, agent string) []string {
	const tag = "\x00"
	there := "revier " + kind + " exec -p " + shell.Quote(string(project))
	if agent != "" {
		there += " --agent " + shell.Quote(agent)
	}
	there += " --tag " + tag
	words := []string{"exec", "ssh", "-t"}
	for _, f := range connection() {
		words = append(words, shell.Quote(f))
	}
	script := strings.Join(words, " ") + " -- " + shell.Quote(host) + ` "` + doubleQuoted(login(there)) + `"`
	return []string{"sh", "-c", strings.ReplaceAll(script, tag, `$(uname -n).$$ $*`), "sh"}
}

// doubleQuoted is s as it stands between double quotes in a POSIX shell.
func doubleQuoted(s string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`, `$`, `\$`, "`", "\\`").Replace(s)
}

// RunCommand is the ssh that runs the action on the remote with this
// terminal: -t asks for a tty there, so an action that prompts or draws
// works as it would in a shell on the host.
func (r *Remote) RunCommand(project revier.ProjectName, action string) []string {
	remote := strings.Join([]string{"revier", "run", shell.Quote(action), "-p", shell.Quote(string(project))}, " ")
	return append(append([]string{"ssh", "-t"}, flags()...), "--", r.host, login(remote))
}

// Version asks the remote revier for its version: the word after "revier"
// in what `revier version` prints, which every revier has.
func (r *Remote) Version(ctx context.Context) (string, error) {
	out, _, err := r.run(ctx, "revier", "version")
	if err != nil {
		return "", err
	}
	fields := strings.Fields(string(out))
	if len(fields) < 2 || fields[0] != "revier" {
		return "", fmt.Errorf("%s: revier version: %q is no version", r.host, strings.TrimSpace(string(out)))
	}
	return fields[1], nil
}

// Survey asks the remote revier for the named projects, in one call. A name
// it does not know is left out of the answer.
func (r *Remote) Survey(ctx context.Context, names []revier.ProjectName) ([]revier.ProjectView, error) {
	return r.list(ctx, names)
}

// Conversations is the survey with `--conversations`.
func (r *Remote) Conversations(ctx context.Context, names []revier.ProjectName) ([]revier.ProjectView, error) {
	return r.list(ctx, names, "--conversations")
}

// Events asks the remote revier for what it recorded itself.
func (r *Remote) Events(ctx context.Context, days int) ([]revier.Event, error) {
	out, _, err := r.run(ctx, "revier", "events", "--local", "--days", strconv.Itoa(days))
	if err != nil {
		return nil, err
	}
	var recorded []revier.Event
	dec := json.NewDecoder(bytes.NewReader(out))
	for dec.More() {
		var e revier.Event
		if err := dec.Decode(&e); err != nil {
			return nil, fmt.Errorf("%s: revier events: %w", r.host, err)
		}
		recorded = append(recorded, e)
	}
	return recorded, nil
}

func (r *Remote) list(ctx context.Context, names []revier.ProjectName, flags ...string) ([]revier.ProjectView, error) {
	args := append([]string{"revier", "list", "--json"}, flags...)
	for _, n := range names {
		args = append(args, string(n))
	}
	out, stderr, err := r.run(ctx, args...)
	if err != nil {
		return partial(names, out, stderr, err)
	}
	var views []revier.ProjectView
	if err := json.Unmarshal(out, &views); err != nil {
		return nil, fmt.Errorf("%s: revier list --json: %w", r.host, err)
	}
	return views, nil
}

// partial is the answer of a list of names that failed. A remote revier that
// knows some of the names lists those and fails for the others, and one that
// knows none lists nothing: a name it does not know is one link's fault,
// which the core reports for that link alone (decisions/0085). Every other
// failure is the host's.
//
// The answer counts only when each name it leaves out has a line of its own
// that says the remote revier does not know it: a revier older than 0.23
// ended at the first name it did not know and listed nothing, also the names
// it knows. It counts only when nothing revier wrote after those lines is
// more than a warning, since a survey that failed after them has no answer.
// And it counts only when that revier ended the call itself: it writes the
// lines before its survey starts, so a call that was killed or lost its
// connection after them has no answer either.
func partial(names []revier.ProjectName, out, stderr []byte, err error) ([]revier.ProjectView, error) {
	if !errors.As(err, new(failedThere)) {
		return nil, err
	}
	refused, ok := refusedNames(stderr)
	if !ok {
		return nil, err
	}
	var views []revier.ProjectView
	if len(bytes.TrimSpace(out)) != 0 && json.Unmarshal(out, &views) != nil {
		return nil, err
	}
	for _, n := range names {
		listed := slices.ContainsFunc(views, func(v revier.ProjectView) bool { return v.Project.Name == n })
		if !listed && !refused[n] {
			return nil, err
		}
	}
	return views, nil
}

// noProjectNamed is how the remote revier starts the line for a name it does
// not know, before the quoted name. A login profile can write to stderr too,
// so only the lines revier wrote count.
const noProjectNamed = "revier: no project named "

// refusedNames is the names the remote revier said it does not know, and
// whether those lines are the last it wrote that are not a warning.
func refusedNames(stderr []byte) (map[revier.ProjectName]bool, bool) {
	refused := map[revier.ProjectName]bool{}
	last := ""
	for line := range strings.Lines(string(stderr)) {
		if !strings.HasPrefix(line, "revier: ") || strings.HasPrefix(line, "revier: warning: ") {
			continue
		}
		last = line
		quoted, ok := strings.CutPrefix(line, noProjectNamed)
		if !ok {
			continue
		}
		if prefix, err := strconv.QuotedPrefix(quoted); err == nil {
			name, _ := strconv.Unquote(prefix)
			refused[revier.ProjectName(name)] = true
		}
	}
	return refused, strings.HasPrefix(last, noProjectNamed)
}
