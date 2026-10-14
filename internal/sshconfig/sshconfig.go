// Package sshconfig reads the hosts an ssh configuration names: the list
// `ssh <tab>` offers, and the list the link dialog offers (decisions.md
// D45).
package sshconfig

import (
	"bufio"
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
// relative one is against the file's own directory. A file that does not
// exist names nothing.
func Hosts(path string) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	err := walk(path, seen, &out, 0)
	return out, err
}

// includeDepth bounds an Include chain, as ssh does, so a file that includes
// itself ends.
const includeDepth = 16

func walk(path string, seen map[string]bool, out *[]string, depth int) error {
	if depth > includeDepth {
		return nil
	}
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, rest, _ := strings.Cut(strings.ReplaceAll(line, "=", " "), " ")
		fields := strings.Fields(rest)
		switch strings.ToLower(key) {
		case "host":
			for _, h := range fields {
				if strings.ContainsAny(h, "*?!") || seen[h] {
					continue
				}
				seen[h] = true
				*out = append(*out, h)
			}
		case "include":
			for _, pattern := range fields {
				pattern = expandHome(pattern)
				if !filepath.IsAbs(pattern) {
					pattern = filepath.Join(filepath.Dir(path), pattern)
				}
				matches, err := filepath.Glob(pattern)
				if err != nil {
					return fmt.Errorf("%s: include %q: %w", path, pattern, err)
				}
				for _, included := range matches {
					if err := walk(included, seen, out, depth+1); err != nil {
						return err
					}
				}
			}
		}
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

// HostName is the HostName the file gives host, as ssh reads it: the first
// value in a Host block one of whose patterns matches host and none of whose
// negated ones does, Include lines followed. "" is a host the file gives
// none, which ssh connects to by its own name. A Match block is read as one
// that does not match, since its conditions are ssh's to evaluate.
func HostName(path, host string) (string, error) {
	name, _, err := hostName(path, host, true, 0)
	return name, err
}

// hostName walks one file for HostName. matching is whether the lines before
// the file's first block apply: they belong to the block the Include stands
// in, or, at the top, to every host.
func hostName(path, host string, matching bool, depth int) (string, bool, error) {
	if depth > includeDepth {
		return "", false, nil
	}
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	defer func() { _ = f.Close() }()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, rest, _ := strings.Cut(strings.ReplaceAll(line, "=", " "), " ")
		fields := strings.Fields(rest)
		switch strings.ToLower(key) {
		case "host":
			matching = hostMatches(fields, host)
		case "match":
			matching = false
		case "hostname":
			if matching && len(fields) > 0 {
				return fields[0], true, nil
			}
		case "include":
			if !matching {
				continue
			}
			for _, pattern := range fields {
				pattern = expandHome(pattern)
				if !filepath.IsAbs(pattern) {
					pattern = filepath.Join(filepath.Dir(path), pattern)
				}
				matches, err := filepath.Glob(pattern)
				if err != nil {
					return "", false, fmt.Errorf("%s: include %q: %w", path, pattern, err)
				}
				for _, included := range matches {
					if name, ok, err := hostName(included, host, true, depth+1); err != nil || ok {
						return name, ok, err
					}
				}
			}
		}
	}
	if err := sc.Err(); err != nil {
		return "", false, fmt.Errorf("%s: %w", path, err)
	}
	return "", false, nil
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
