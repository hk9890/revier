package tui

import (
	"errors"
	"strings"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/pkg/revier"
)

// failed is an operation's error as the footer shows it: what failed, and
// where. The error a tool gave can quote its output or join several, and the
// footer is one line, so it names the operation and the logs screen holds the
// rest (decisions.md D157). The log line keeps the full text: Error is the
// error's own.
type failed struct {
	what    string
	project revier.ProjectName
	err     error
}

func (f *failed) Error() string { return f.err.Error() }
func (f *failed) Unwrap() error { return f.err }

// fail names what failed, on which project, or on none. It is a pointer, so
// two of them compare as two errors and not by what they hold, which can be
// a value no == takes.
func fail(what string, project revier.ProjectName, err error) error {
	if err == nil {
		return nil
	}
	return &failed{what: what, project: project, err: err}
}

// refused reports an error that is a refusal and not a failure: a target with
// no host here or no such target, and a target or a project its file refused.
// Its own text says why, and is short, so the footer keeps it.
func refused(p core.Project, err error) bool {
	if errors.Is(err, core.ErrNoHost) || errors.Is(err, core.ErrNoTarget) || p.Invalid != nil && errors.Is(err, p.Invalid) {
		return true
	}
	for i := range p.Targets {
		if why := p.TargetErr(i); why != nil && errors.Is(err, why) {
			return true
		}
	}
	return false
}

// short is the footer's text for an error. An operation's names what failed
// and, when asked, the key that opens the rest; any other error is its own
// text, which a refusal words for the footer, on one line: a second line
// pushes the frame past the terminal.
func short(err error, withKey bool) string {
	if f, ok := err.(*failed); ok {
		text := f.what + " failed"
		if f.project != "" {
			text += ": " + string(f.project)
		}
		if withKey {
			text += " (" + logsKey + " for details)"
		}
		return text
	}
	return strings.ReplaceAll(err.Error(), "\n", "; ")
}
