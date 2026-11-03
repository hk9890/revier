// Package sshconfig reads the hosts an ssh configuration names: the list
// `ssh <tab>` offers, and the list the remote screen's "new" offers
// (decisions.md D154). It also reads the HostName ssh connects to for a host,
// by which revier knows a link host that is this machine (D131).
package sshconfig

import (
	"bufio"
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Path is the user's ssh configuration file, or what REVIER_SSH_CONFIG
// names instead: the override a test, and scripts/drive, run against.
func Path() (string, error) {
	if p := os.Getenv("REVIER_SSH_CONFIG"); p != "" {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".ssh", "config"), nil
}

// Hosts lists the destinations the file names, in file order, each once.
// A `Host` line names one or more; a pattern - with a `*`, a `?` or a `!` -
// is a rule and not a destination, and is left out. `Include` lines are
// followed: a leading `~` is the home directory, as ssh reads it, and a
// relative one is against the directory of the file at path, as Read says.
// A file that does not exist names nothing.
func Hosts(path string) ([]string, error) {
	f, err := Read(path)
	if err != nil {
		return nil, err
	}
	var out []string
	f.hosts(map[string]bool{}, &out)
	return out, nil
}

func (f File) hosts(seen map[string]bool, out *[]string) {
	for _, l := range f.lines {
		switch l.key {
		case "host":
			for _, h := range l.fields {
				if strings.ContainsAny(h, "*?!") || seen[h] {
					continue
				}
				seen[h] = true
				*out = append(*out, h)
			}
		case "include":
			for _, inc := range l.included {
				inc.hosts(seen, out)
			}
		}
	}
}

// includeDepth bounds an Include chain, as ssh does, so a file that includes
// itself ends.
const includeDepth = 16

// File is an ssh configuration as far as Hosts and HostName read it, Include
// lines followed, read once so that a caller can ask it about every link
// host.
type File struct{ lines []line }

// line is one Host, Match, HostName or Include line; an Include carries the
// files it names.
type line struct {
	key      string
	fields   []string
	included []File
}

// Read reads the file at path and every file it includes. A file that does
// not exist is an empty File. An Include that cannot be read is left out, as
// ssh leaves it out, and the rest is read: the File holds every line that
// could be read, and the error is the first Include that could not.
//
// A relative Include is against the directory of the file at path, the
// user's configuration file, in every file it includes too: ssh_config(5)
// reads one in a user configuration file against ~/.ssh, the directory of
// ~/.ssh/config.
func Read(path string) (File, error) {
	return read(path, filepath.Dir(path), 0)
}

func read(path, dir string, depth int) (File, error) {
	if depth > includeDepth {
		return File{}, nil
	}
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return File{}, nil
	}
	if err != nil {
		return File{}, err
	}
	defer func() { _ = f.Close() }()

	var out File
	var unread error
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		// ssh separates a keyword from its value by any whitespace, or by one
		// "=".
		fields := strings.Fields(strings.ReplaceAll(sc.Text(), "=", " "))
		if len(fields) == 0 || strings.HasPrefix(fields[0], "#") {
			continue
		}
		l := line{key: strings.ToLower(fields[0]), fields: fields[1:]}
		switch l.key {
		case "host", "match", "hostname":
		case "include":
			for _, pattern := range l.fields {
				pattern = expandHome(pattern)
				if !filepath.IsAbs(pattern) {
					pattern = filepath.Join(dir, pattern)
				}
				matches, err := filepath.Glob(pattern)
				if err != nil {
					unread = cmp.Or(unread, fmt.Errorf("%s: include %q: %w", path, pattern, err))
					continue
				}
				for _, included := range matches {
					inc, err := read(included, dir, depth+1)
					unread = cmp.Or(unread, err)
					l.included = append(l.included, inc)
				}
			}
		default:
			continue
		}
		out.lines = append(out.lines, l)
	}
	if err := sc.Err(); err != nil {
		return out, fmt.Errorf("%s: %w", path, err)
	}
	return out, unread
}

// HostName is the HostName the file gives host, as ssh reads it: the first
// value in a Host block one of whose patterns matches host and none of whose
// negated ones does, Include lines followed. "" is a host the file gives
// none, which ssh connects to by its own name. A Match block is read as one
// that does not match, since its conditions are ssh's to evaluate, except
// Match all, which matches every host.
func (f File) HostName(host string) string {
	name, _ := f.hostName(host, true)
	return name
}

// hostName walks one file for HostName. matching is whether the lines before
// the file's first block apply: they belong to the block the Include stands
// in, or, at the top, to every host.
func (f File) hostName(host string, matching bool) (string, bool) {
	for _, l := range f.lines {
		switch l.key {
		case "host":
			matching = hostMatches(l.fields, host)
		case "match":
			matching = len(l.fields) == 1 && strings.EqualFold(l.fields[0], "all")
		case "hostname":
			if matching && len(l.fields) > 0 {
				return l.fields[0], true
			}
		case "include":
			if !matching {
				continue
			}
			for _, inc := range l.included {
				if name, ok := inc.hostName(host, true); ok {
					return name, ok
				}
			}
		}
	}
	return "", false
}

// hostMatches reports a Host line's patterns matching host: one of them
// does, and no negated one does.
func hostMatches(patterns []string, host string) bool {
	matched := false
	for _, p := range patterns {
		negated := strings.HasPrefix(p, "!")
		if ok, _ := filepath.Match(strings.TrimPrefix(p, "!"), host); ok {
			if negated {
				return false
			}
			matched = true
		}
	}
	return matched
}

// expandHome resolves a leading "~" in an Include path against the home
// directory, which is how ssh reads it and the form a tool that drops a file
// into ~/.ssh/config writes. A home that cannot be resolved leaves the path
// as it stands, which then matches nothing.
//
// It is config.ExpandHome again on purpose. This package reads one file and
// imports nothing of revier, so config, core and session can each ask it
// about a link's host; an import of config here would close that.
func expandHome(p string) string {
	if p != "~" && !strings.HasPrefix(p, "~/") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	return filepath.Join(home, strings.TrimPrefix(p, "~"))
}
