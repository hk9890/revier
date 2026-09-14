// Package checkout is what revier does to a project's directory rather than to
// its windows: it reads the origin a new project records, clones the
// repository of a project whose directory is missing, and trusts the mise
// configuration of a checkout it created or adopted.
//
// Cloning is the only write revier makes outside its own configuration and
// state. It runs in the foreground with git's output on the terminal, and only
// on an explicit open - never as a side effect of a survey.
package checkout

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/logging"
	"github.com/hk9890/revier/pkg/revier"
)

// ErrNoGitURL means a project's directory is missing and its file records no
// repository to clone it from. Only the user can fix that, by adding git_url.
var ErrNoGitURL = errors.New("the project file records no git_url to clone it from")

// Origin is the URL of dir's origin remote, or "" when dir has none. A URL
// config.ValidateGitURL refuses is dropped too, as the shell tool drops it
// (session_infer_git_clone_url): a remote with a token in it must not be
// copied into a file that is shared between machines.
//
// So is the origin of a repository dir is only inside. A clone of it lands at
// the project's path, so recorded for a subdirectory it would put the whole
// repository where the subdirectory was, nested one level too deep.
func Origin(dir string) string {
	prefix, err := exec.Command("git", "-C", dir, "rev-parse", "--show-prefix").Output()
	if err != nil || strings.TrimSpace(string(prefix)) != "" {
		return ""
	}
	out, err := exec.Command("git", "-C", dir, "remote", "get-url", "origin").Output()
	if err != nil {
		return ""
	}
	u := strings.TrimSpace(string(out))
	if config.ValidateGitURL(u) != nil {
		return ""
	}
	return u
}

// Ensure makes the project's directory exist. It does nothing when it does,
// and clones GitURL into it when it does not; it reports whether it cloned.
// git's own output goes to out, because a clone takes as long as it takes and
// the user has to see why nothing has opened yet.
//
// No deadline: the clone of a large repository outlasts any timeout a keypress
// command sets for its host calls.
func Ensure(p revier.Project, out io.Writer) (bool, error) {
	fi, err := os.Stat(p.Path)
	switch {
	case err == nil && fi.IsDir():
		return false, nil
	case err == nil:
		return false, fmt.Errorf("project %q: %s exists and is not a directory", p.Name, p.Path)
	case !errors.Is(err, fs.ErrNotExist):
		return false, err
	case p.GitURL == "":
		return false, fmt.Errorf("project %q: %s does not exist, and %w", p.Name, p.Path, ErrNoGitURL)
	}
	if err := os.MkdirAll(filepath.Dir(p.Path), 0o755); err != nil {
		return false, err
	}
	_, _ = fmt.Fprintf(out, "revier: %s is not on this machine; cloning %s\n", p.Path, p.GitURL)
	// "--" because a URL is data: one starting with a dash must not reach git
	// as an option.
	c := exec.Command("git", "clone", "--", p.GitURL, p.Path)
	c.Stdout, c.Stderr = out, out
	start := time.Now()
	err = c.Run()
	logging.Op("clone", start, err, "project", p.Name, "git_url", p.GitURL, "path", p.Path)
	if err != nil {
		return false, fmt.Errorf("project %q: git clone %s: %w", p.Name, p.GitURL, err)
	}
	Trust(p.Path, out)
	return true, nil
}

// Trust runs `mise trust` on dir, so the tools a checkout pins are active in
// the first shell that opens there instead of behind a prompt. It cannot fail
// the command it is part of: without mise there is nothing to trust, and a
// refusal is printed as a warning.
func Trust(dir string, out io.Writer) {
	if _, err := exec.LookPath("mise"); err != nil {
		return
	}
	msg, err := exec.Command("mise", "trust", dir).CombinedOutput()
	if err != nil {
		_, _ = fmt.Fprintf(out, "revier: warning: mise trust %s: %v\n%s", dir, err, msg)
	}
}
