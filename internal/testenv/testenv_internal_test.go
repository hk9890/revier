package testenv

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// scratch is the directory Run makes before the tests run.
func scratch(t *testing.T) (dir, config string) {
	t.Helper()
	dir = t.TempDir()
	config = filepath.Join(dir, "config")
	if err := os.Mkdir(config, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(config, "config.toml"), []byte(noHosts), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir, config
}

func TestAScratchNoTestWroteIntoListsNothing(t *testing.T) {
	dir, config := scratch(t)
	if got := writtenSince(dir, config); len(got) != 0 {
		t.Errorf("writtenSince = %v, want nothing", got)
	}
}

func TestAWriteIntoTheScratchIsListed(t *testing.T) {
	dir, config := scratch(t)
	project := filepath.Join(config, "projects", "x.toml")
	if err := os.MkdirAll(filepath.Dir(project), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(project, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(dir, "state")
	if err := os.Mkdir(state, 0o755); err != nil {
		t.Fatal(err)
	}
	got := writtenSince(dir, config)
	for _, want := range []string{filepath.Dir(project), project, state} {
		if !slices.Contains(got, want) {
			t.Errorf("writtenSince = %v, want %s in it", got, want)
		}
	}
}

func TestAChangedScratchConfigIsListed(t *testing.T) {
	dir, config := scratch(t)
	file := filepath.Join(config, "config.toml")
	if err := os.WriteFile(file, []byte("[hosts]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := writtenSince(dir, config); !slices.Equal(got, []string{file}) {
		t.Errorf("writtenSince = %v, want [%s]", got, file)
	}
}
