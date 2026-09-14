package ssh

import (
	"context"
	"errors"
	"os/exec"
	"strings"
)

// A failed ssh writes its reason to stderr, and the reason is nearly always
// one of a handful: the host key is not known, the key was refused, the
// machine is not there, or it is there and has no revier. Passing that text
// through named the fault in ssh's words and left the reader to work out what
// to do about it. classify says the same thing in one line, cause then
// remedy, and passes anything it does not know through as it stands.
//
// OpenSSH is not translated, so matching its own words holds wherever ssh
// runs: the macOS and the Windows builds say the same. Its own failures are
// told apart from the remote's by the status it exits with - 255 is ssh's
// and nothing else's - so a remote revier that says "permission denied"
// about a file of its own is not read as a refused key. The remote's own
// reasons are matched on the status first, which every POSIX shell and
// cmd.exe agree on, and on words only where they differ.

// sshStatus is the status ssh exits with when the failure is its own.
const sshStatus = 255

// notFound are the statuses a shell uses for a command it cannot find: 127 in
// every POSIX shell, 9009 in cmd.exe.
var notFound = map[int]bool{127: true, 9009: true}

// sshReasons are the failures ssh itself reports, in the words it reports
// them in, each with what the reader can do about it. The first match wins,
// so a longer phrase comes before a shorter one it contains.
var sshReasons = []struct{ match, say string }{
	{"host key verification failed", "host key not known - run `ssh HOST` once to accept it"},
	{"could not resolve hostname", "hostname does not resolve"},
	{"permission denied", "ssh refused the key - revier cannot type a password, so set up key auth"},
	{"connection timed out", "no answer on port 22"},
	{"operation timed out", "no answer on port 22"},
	{"connection refused", "nothing is listening on port 22"},
	{"no route to host", "the machine is not reachable"},
}

// shellReasons are what the machine at the other end says when it answers but
// runs nothing: a git host, which has a shell that takes git and no more.
var shellReasons = []struct{ match, say string }{
	{"does not provide shell access", "no shell there - it is a git host, not a machine"},
	{"invalid command", "no shell there - it is a git host, not a machine"},
}

// classify turns one failed call into the line the reader sees. args is the
// command that was run, kept only in the fallback, where the reason is
// unknown and the whole of what happened is worth reading.
func classify(ctx context.Context, host string, args []string, stderr string, err error) error {
	msg := strings.TrimSpace(stderr)
	low := strings.ToLower(msg)
	if status(err) == sshStatus {
		if say, ok := match(sshReasons, low); ok {
			return reason(host, say)
		}
	}
	if say, ok := match(shellReasons, low); ok {
		return reason(host, say)
	}
	if isNotFound(low, err) {
		return reason(host, "no revier on the PATH ssh gives - put mise or asdf shims in ~/.zshenv")
	}
	if isOlder(low) {
		return reason(host, "the revier there has no `"+command(args)+"` - update revier on HOST")
	}
	if errors.Is(err, exec.ErrNotFound) {
		return reason(host, "no ssh on this machine")
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return reason(host, "no answer before the wait ran out")
	}
	if msg == "" {
		msg = err.Error()
	}
	return errors.New(host + ": " + strings.Join(args, " ") + ": " + oneLine(msg))
}

func match(reasons []struct{ match, say string }, low string) (string, bool) {
	for _, r := range reasons {
		if strings.Contains(low, r.match) {
			return r.say, true
		}
	}
	return "", false
}

// status is what the command exited with, or -1 where it did not run at all.
func status(err error) int {
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode()
	}
	return -1
}

// isNotFound reports whether the remote shell could not find revier. It is
// not the same as revier not being installed, and the message does not say
// it is: a shim under a version manager is on the PATH of an interactive
// shell and not on the one an ssh command gets, so the machine that answers
// "command not found" is as often as not a machine with revier on it.
//
// The words count only where they name revier. A revier that ran, and failed
// because a command of its own was not found, says the same words about that
// command, and its PATH is not the fault.
func isNotFound(low string, err error) bool {
	if low == "" {
		return notFound[status(err)]
	}
	return strings.Contains(low, "command not found: revier") ||
		strings.Contains(low, "revier: command not found") ||
		strings.Contains(low, "revier: not found") ||
		strings.Contains(low, "'revier' is not recognized") ||
		strings.Contains(low, "unknown command: revier")
}

// isOlder reports whether the remote revier ran and did not know the command:
// its words for a command a newer revier added, printed after its whole usage.
func isOlder(low string) bool {
	return strings.Contains(low, `unknown command "`) || strings.Contains(low, `unknown agent command "`)
}

// command is the revier command args run, without its flags and arguments:
// "shell new" of `revier shell new -p far`.
func command(args []string) string {
	var words []string
	for _, a := range args[1:] {
		if strings.HasPrefix(a, "-") {
			break
		}
		words = append(words, a)
	}
	return strings.Join(words, " ")
}

func reason(host, say string) error {
	return errors.New(host + ": " + strings.ReplaceAll(say, "HOST", host))
}

// oneLine folds a reason of several lines into one, because the footer that
// shows it has one line. ssh's own reasons are one line already; a remote
// shell's need not be.
func oneLine(s string) string {
	lines := strings.FieldsFunc(s, func(r rune) bool { return r == '\n' || r == '\r' })
	for i, l := range lines {
		lines[i] = strings.TrimSpace(l)
	}
	return strings.Join(lines, "; ")
}
