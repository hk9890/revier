package config

import (
	"fmt"
	"os"
	"slices"
	"strings"
)

// RemoteMachine is one [[machine]] table of config.toml: a machine the
// remote screen and `revier link` list (decisions.md D154). Host is the ssh
// destination, as a link's host is.
type RemoteMachine struct {
	Host string `toml:"host"`
}

// validateMachines refuses a [[machine]] table with no host, with a host ssh
// would read as something else, or with a host an earlier table has. One
// error per table, nil for each one that stands.
func validateMachines(machines []RemoteMachine) []error {
	out := make([]error, len(machines))
	seen := map[string]bool{}
	for i, m := range machines {
		switch {
		case m.Host == "":
			out[i] = fmt.Errorf("machine %d: needs a host", i+1)
		case seen[m.Host]:
			out[i] = fmt.Errorf("machine %q: listed twice", m.Host)
		default:
			if err := validateHost(m.Host); err != nil {
				out[i] = fmt.Errorf("machine %q: host %w", m.Host, err)
			}
		}
		seen[m.Host] = true
	}
	return out
}

// MachineHosts is the hosts the remote screen and `revier link` list: every
// machine config.toml declares and no rule refused, in file order, without
// this machine. The file can be shared between machines, so each one is on
// the list and leaves itself off it (decisions.md D131, D154).
func (c *Config) MachineHosts() []string {
	return slices.DeleteFunc(slices.Clone(c.machines), ThisMachine().Is)
}

// Declares reports a host that a [[machine]] table names, this machine's
// own entry included.
func (c *Config) Declares(host string) bool {
	return slices.Contains(c.machines, host)
}

// AddMachine writes a new [[machine]] table for host at the end of
// config.toml. A host on the list already is refused, so a host added by
// hand since revier read the file is not added twice.
func AddMachine(root, host string) error {
	return editMachines(root, func(lines []string, _ []table, have []RemoteMachine) ([]string, []RemoteMachine, error) {
		if slices.Contains(have, RemoteMachine{Host: host}) {
			return nil, nil, fmt.Errorf("%s is on the list already", host)
		}
		literal, err := literalOf(host)
		if err != nil {
			return nil, nil, fmt.Errorf("machine.host: %w", err)
		}
		out := strings.TrimRight(strings.Join(lines, "\n"), "\n")
		if out != "" {
			out += "\n\n"
		}
		out += "[[machine]]\nhost = " + literal + "\n"
		return strings.Split(out, "\n"), append(slices.Clone(have), RemoteMachine{Host: host}), nil
	})
}

// RemoveMachine deletes the [[machine]] table for host. Its header and its
// values go; its comments stay.
func RemoveMachine(root, host string) error {
	return editMachines(root, func(lines []string, entries []table, have []RemoteMachine) ([]string, []RemoteMachine, error) {
		i := slices.Index(have, RemoteMachine{Host: host})
		if i < 0 {
			return nil, nil, fmt.Errorf("%s is not on the list", host)
		}
		t := entries[i]
		return dropLines(lines, t.header, t.end), slices.Delete(slices.Clone(have), i, i+1), nil
	})
}

// editMachines runs one change to the [[machine]] tables of config.toml, and
// writes the result only if it parses as Load parses it and decodes to the
// machines the change meant. Machines written in another form, such as an
// inline array, are refused: the line editor would not find them.
func editMachines(root string, change func(lines []string, entries []table, have []RemoteMachine) ([]string, []RemoteMachine, error)) error {
	path := File(root)
	old, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("%s: %w", path, err)
	}
	cfg, err := parse(old)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	lines := strings.Split(string(old), "\n")
	entries := entriesOf(lines, "machine")
	if len(entries) != len(cfg.Machines) {
		return fmt.Errorf("%s: the machines are not written as [[machine]] tables; change them by hand", path)
	}
	lines, want, err := change(lines, entries, cfg.Machines)
	if err != nil {
		return err
	}
	text := strings.Join(lines, "\n")
	got, err := parse([]byte(text))
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if err := problemsAdded(cfg, got); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if !slices.Equal(got.Machines, want) {
		return fmt.Errorf("%s: the machines did not come out as written; change them by hand", path)
	}
	return replaceFile(path, []byte(text))
}
