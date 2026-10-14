package sshconfig_test

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/hk9890/revier/internal/sshconfig"
)

func write(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// The list is what `ssh <tab>` offers: every Host, in order, once, without
// the patterns, which are rules and not destinations.
func TestHostsAreTheDestinationsInFileOrder(t *testing.T) {
	path := write(t, t.TempDir(), "config", `
# work
Host buildbox
    HostName 10.0.0.7
Host *.example.com
    User hans
Host lab1 lab2
Host=buildbox
Host !private
Host *
    ControlMaster auto
`)
	hosts, err := sshconfig.Hosts(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"buildbox", "lab1", "lab2"}
	if !slices.Equal(hosts, want) {
		t.Errorf("hosts = %v, want %v", hosts, want)
	}
}

// An Include is followed, a relative one against the file's own directory,
// so hosts split across files are all offered.
func TestHostsFollowIncludes(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "conf.d"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "conf.d"), "work", "Host farbox\n")
	path := write(t, dir, "config", "Include conf.d/*\nHost buildbox\n")

	hosts, err := sshconfig.Hosts(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"farbox", "buildbox"}; !slices.Equal(hosts, want) {
		t.Errorf("hosts = %v, want %v", hosts, want)
	}
}

// `Include ~/.ssh/...` is the form a tool that drops a file beside the
// configuration writes, and ssh reads the tilde as the home directory.
func TestHostsFollowAnIncludeUnderTheHomeDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".ssh"), 0o700); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(home, ".ssh"), "extra", "Host farbox\n")
	path := write(t, t.TempDir(), "config", "Include ~/.ssh/extra\nHost buildbox\n")

	hosts, err := sshconfig.Hosts(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"farbox", "buildbox"}; !slices.Equal(hosts, want) {
		t.Errorf("hosts = %v, want %v", hosts, want)
	}
}

func TestAMissingFileNamesNothing(t *testing.T) {
	hosts, err := sshconfig.Hosts(filepath.Join(t.TempDir(), "none"))
	if err != nil || len(hosts) != 0 {
		t.Errorf("hosts = %v, err = %v; want none and no error", hosts, err)
	}
}

// An Include chain is followed sixteen deep, as ssh follows it: the file at
// depth sixteen is read, the one under it is not.
func TestAnIncludeChainIsFollowedSixteenDeep(t *testing.T) {
	dir := t.TempDir()
	for i := range 18 {
		write(t, dir, fmt.Sprintf("c%d", i), fmt.Sprintf("Host h%d\nInclude c%d\n", i, i+1))
	}
	hosts, err := sshconfig.Hosts(filepath.Join(dir, "c0"))
	if err != nil {
		t.Fatal(err)
	}
	var want []string
	for i := range 17 {
		want = append(want, fmt.Sprintf("h%d", i))
	}
	if !slices.Equal(hosts, want) {
		t.Errorf("hosts = %v, want h0 through h16", hosts)
	}
}

// A file that includes itself ends.
func TestAnIncludeLoopEnds(t *testing.T) {
	dir := t.TempDir()
	path := write(t, dir, "config", "Include config\nHost buildbox\n")
	hosts, err := sshconfig.Hosts(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"buildbox"}; !slices.Equal(hosts, want) {
		t.Errorf("hosts = %v, want %v", hosts, want)
	}
}

// HostName is the name ssh connects to for a host: the first value of a
// block that matches it, a pattern or an Include included, and none for a
// host the file gives no name.
func TestHostNameIsTheFirstValueOfAMatchingBlock(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "work", "Host lab*\n    HostName lab.example.com\n")
	path := write(t, dir, "config", `
Include work
Host buildbox
    HostName 10.0.0.7
Host buildbox
    HostName ignored.example.com
Host !private *.box
    HostName wild
Match host dt
    HostName from-match
Host dt
    HostName dt.local
`)
	for host, want := range map[string]string{
		"buildbox":   "10.0.0.7",
		"far.box":    "wild",
		"private":    "",
		"dt":         "dt.local",
		"lab7":       "lab.example.com",
		"elsewhere":  "",
		"buildbox2":  "",
		"other.box2": "",
	} {
		got, err := sshconfig.HostName(path, host)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("HostName(%q) = %q, want %q", host, got, want)
		}
	}
}

// Match all ends a Host block and matches every host again, which is how a
// file includes others for every host after its blocks.
func TestHostNameReadsAMatchAllBlock(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "hosts", "Host lab\n    HostName lab.example.com\n")
	path := write(t, dir, "config", "Host buildbox\n    User me\nMatch all\n    Include hosts\n")
	got, err := sshconfig.HostName(path, "lab")
	if err != nil {
		t.Fatal(err)
	}
	if got != "lab.example.com" {
		t.Errorf("HostName(lab) = %q, want lab.example.com", got)
	}
}
