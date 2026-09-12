package sshconfig_test

import (
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

func TestAMissingFileNamesNothing(t *testing.T) {
	hosts, err := sshconfig.Hosts(filepath.Join(t.TempDir(), "none"))
	if err != nil || len(hosts) != 0 {
		t.Errorf("hosts = %v, err = %v; want none and no error", hosts, err)
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
