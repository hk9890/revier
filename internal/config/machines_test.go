package config_test

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/config"
)

const twoMachines = `# the machines I work on
[[machine]]
host = "buildbox" # the fast one

[[machine]]
host = "farbox"
`

// The remote screen lists the machines config.toml declares, in file order.
func TestMachineHostsAreTheDeclaredMachines(t *testing.T) {
	cfg, err := config.Read(writeConfig(t, twoMachines))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := cfg.MachineHosts(), []string{"buildbox", "farbox"}; !slices.Equal(got, want) {
		t.Errorf("machines = %v, want %v", got, want)
	}
	if len(cfg.Problems) != 0 {
		t.Errorf("problems = %v, want none", cfg.Problems)
	}
}

// config.toml can be shared between machines, so each machine is on the
// list; it leaves itself off, and its own entry is no problem.
func TestThisMachineIsNotListed(t *testing.T) {
	machine, err := os.Hostname()
	if err != nil {
		t.Skip("no hostname:", err)
	}
	cfg, err := config.Read(writeConfig(t, twoMachines+"\n[[machine]]\nhost = \""+machine+"\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := cfg.MachineHosts(), []string{"buildbox", "farbox"}; !slices.Equal(got, want) {
		t.Errorf("machines = %v, want %v", got, want)
	}
	if !cfg.Declares(machine) {
		t.Errorf("Declares(%q) = false, want this machine's entry declared", machine)
	}
	if len(cfg.Problems) != 0 {
		t.Errorf("problems = %v, want none", cfg.Problems)
	}
}

// A table with no host, a host ssh reads as a flag, and a host listed twice
// are each a problem of their own, and only they are left off the list.
func TestABadMachineIsAProblemOfItsOwn(t *testing.T) {
	text := twoMachines + `
[[machine]]

[[machine]]
host = "-oProxyCommand=x"

[[machine]]
host = "buildbox"
`
	cfg, err := config.Read(writeConfig(t, text))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := cfg.MachineHosts(), []string{"buildbox", "farbox"}; !slices.Equal(got, want) {
		t.Errorf("machines = %v, want %v", got, want)
	}
	var got []string
	for _, err := range cfg.Problems {
		got = append(got, err.Error())
	}
	for _, want := range []string{"machine 3: needs a host", `machine "-oProxyCommand=x": host`, `machine "buildbox": listed twice`} {
		if !slices.ContainsFunc(got, func(s string) bool { return strings.HasPrefix(s, want) }) {
			t.Errorf("problems = %q, want one that starts %q", got, want)
		}
	}
}

// A machine added is a new [[machine]] table at the end; nothing before it
// moves, and the comments stay.
func TestAddMachineAppendsATable(t *testing.T) {
	root := writeConfig(t, twoMachines)
	if err := config.AddMachine(root, "router"); err != nil {
		t.Fatal(err)
	}
	want := twoMachines + "\n[[machine]]\nhost = \"router\"\n"
	if got := readConfig(t, root); got != want {
		t.Errorf("config.toml =\n%s\nwant\n%s", got, want)
	}
}

func TestAddMachineWritesAMissingFile(t *testing.T) {
	root := t.TempDir()
	if err := config.AddMachine(root, "buildbox"); err != nil {
		t.Fatal(err)
	}
	if got, want := readConfig(t, root), "[[machine]]\nhost = \"buildbox\"\n"; got != want {
		t.Errorf("config.toml = %q, want %q", got, want)
	}
}

// A host on the list already, added by hand since revier read the file, is
// not added twice.
func TestAddMachineRefusesAHostOnTheList(t *testing.T) {
	root := writeConfig(t, twoMachines)
	if err := config.AddMachine(root, "farbox"); err == nil {
		t.Error("AddMachine of a host on the list: want an error")
	}
	if got := readConfig(t, root); got != twoMachines {
		t.Errorf("config.toml =\n%s\nwant it unchanged", got)
	}
}

// A machine removed takes its table with it; the comments around it stay.
func TestRemoveMachineKeepsTheComments(t *testing.T) {
	root := writeConfig(t, twoMachines)
	if err := config.RemoveMachine(root, "buildbox"); err != nil {
		t.Fatal(err)
	}
	got := readConfig(t, root)
	if strings.Contains(got, `"buildbox"`) || !strings.Contains(got, "# the machines I work on") || !strings.Contains(got, "# the fast one") || !strings.Contains(got, `host = "farbox"`) {
		t.Errorf("config.toml =\n%s\nwant buildbox gone, farbox and both comments kept", got)
	}
	cfg, err := config.Read(root)
	if err != nil {
		t.Fatal(err)
	}
	if hosts := cfg.MachineHosts(); !slices.Equal(hosts, []string{"farbox"}) {
		t.Errorf("machines = %v, want [farbox]", hosts)
	}
}

func TestRemoveMachineRefusesAHostNotOnTheList(t *testing.T) {
	root := writeConfig(t, twoMachines)
	if err := config.RemoveMachine(root, "router"); err == nil {
		t.Error("RemoveMachine of a host not on the list: want an error")
	}
}

// Machines written as an inline array are not found by the line editor, so
// a write is refused rather than land twice.
func TestAMachineWriteRefusesAnInlineArray(t *testing.T) {
	root := writeConfig(t, "machine = [{ host = \"buildbox\" }]\n")
	if err := config.AddMachine(root, "farbox"); err == nil || !strings.Contains(err.Error(), "by hand") {
		t.Errorf("AddMachine = %v, want a refusal to change them by hand", err)
	}
}
