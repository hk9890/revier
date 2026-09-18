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
	"os"
	"os/exec"
	"path/filepath"
	"strings"

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
// that login shell, and fish rejects the braces. The line stands between
// double quotes, which POSIX shells and fish read alike: inside single quotes
// fish reads `\'` as a quote, so the `'\”` a quoted word carries would end
// the line early there.
func login(line string) string {
	return `exec "$SHELL" -lc "` + doubleQuoted(line) + `"`
}

// quiet is login for a command whose output is read: what the profile writes
// on the way - a greeting, a version manager's notice - goes to stderr, and
// only the command's own stdout comes back, else a profile that says one
// word breaks the JSON.
func quiet(line string) string {
	return login(line+" >&3") + " 3>&1 1>&2"
}

// exec runs one command on the remote and returns what it wrote. The remote
// shell joins the words ssh is given back into one line, so each is quoted
// here to reach the remote revier as the one argument it was. A failure is
// classified: what ssh or the remote shell said, in one line that says what
// to do about it.
func (r *Remote) exec(ctx context.Context, args ...string) ([]byte, []byte, error) {
	words := make([]string, len(args))
	for i, a := range args {
		words[i] = quote(a)
	}
	full := append(flags(), "--", r.host, quiet(strings.Join(words, " ")))
	var out, errb bytes.Buffer
	c := exec.CommandContext(ctx, "ssh", full...)
	c.Stdout, c.Stderr = &out, &errb
	if err := c.Run(); err != nil {
		return nil, errb.Bytes(), classify(ctx, r.host, args, errb.String(), err)
	}
	return out.Bytes(), errb.Bytes(), nil
}

// quote wraps s for a POSIX shell: single quotes, with each single quote in s
// closed, escaped and reopened.
func quote(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\n'\"\\$`!#&|;<>(){}[]*?~") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// PanelCommand is the command of a link's panel here: `revier <kind> exec`
// on the host, in the login shell there, with this terminal.
//
// The tag names the panel to both machines. It is this machine's name and the
// pid of the ssh, which is the pid the runtime here reports for the panel, so
// the agent the host lists under the tag is found again in the panel that
// shows it, and nothing has to be recorded. The shell that expands the pid is
// replaced by the ssh, which keeps it.
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
func PanelCommand(host string, project revier.ProjectName, kind string) []string {
	const tag = "\x00"
	there := "revier " + kind + " exec -p " + quote(string(project)) + " --tag " + tag
	words := []string{"exec", "ssh", "-t"}
	for _, f := range connection() {
		words = append(words, quote(f))
	}
	script := strings.Join(words, " ") + " -- " + quote(host) + ` "` + doubleQuoted(login(there)) + `"`
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
	remote := strings.Join([]string{"revier", "run", quote(action), "-p", quote(string(project))}, " ")
	return append(append([]string{"ssh", "-t"}, flags()...), "--", r.host, login(remote))
}

// Survey asks the remote revier for the named projects, in one call.
func (r *Remote) Survey(ctx context.Context, names []revier.ProjectName) ([]revier.ProjectView, error) {
	return r.list(ctx, names)
}

// Conversations is the survey with `--conversations`.
func (r *Remote) Conversations(ctx context.Context, names []revier.ProjectName) ([]revier.ProjectView, error) {
	return r.list(ctx, names, "--conversations")
}

func (r *Remote) list(ctx context.Context, names []revier.ProjectName, flags ...string) ([]revier.ProjectView, error) {
	args := append([]string{"revier", "list", "--json"}, flags...)
	for _, n := range names {
		args = append(args, string(n))
	}
	out, _, err := r.run(ctx, args...)
	if err != nil {
		return nil, err
	}
	var views []revier.ProjectView
	if err := json.Unmarshal(out, &views); err != nil {
		return nil, fmt.Errorf("%s: revier list --json: %w", r.host, err)
	}
	return views, nil
}
