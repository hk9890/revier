// Package sshconfig reads the hosts an ssh configuration names: the list
// `ssh <tab>` offers, and the list the link dialog offers (decisions.md
// D42).
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

// expandHome resolves a leading "~" in an Include path against the home
// directory, which is how ssh reads it and the form a tool that drops a file
// into ~/.ssh/config writes. A home that cannot be resolved leaves the path
// as it stands, which then matches nothing.
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
