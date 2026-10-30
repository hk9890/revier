package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/internal/state"
)

// renameApp is an app over a configuration root with the one project a,
// whose home target is the runtime instance titled home.
func renameApp(t *testing.T, rt *hosttest.FakeRuntime) *app {
	t.Helper()
	root := configRoot(t, "", map[string]string{"a.toml": sound})
	cfg, projects, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	a := &app{cfg: cfg, cfgRoot: root, projects: projects, stateRoot: t.TempDir(), core: &core.Core{Runtime: rt}}
	return withState(t, a, &state.State{})
}

func exists(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Stat(path)
	return err == nil
}

// `revier rename a b` moves the file, and says where it is now.
func TestRenameMovesTheProjectFile(t *testing.T) {
	a := renameApp(t, hosttest.NewRuntime("tmux"))
	out := output(t, a, func() error { return cmdRename(context.Background(), a, []string{"a", "b"}) })
	b := filepath.Join(a.cfgRoot, "projects", "b.toml")
	if !exists(t, b) || exists(t, filepath.Join(a.cfgRoot, "projects", "a.toml")) {
		t.Errorf("after the rename, b.toml there: %v, a.toml gone: want both", exists(t, b))
	}
	if !strings.Contains(out, "renamed a to b") {
		t.Errorf("printed %q, want the rename named", out)
	}
}

// A rename is refused, and moves nothing, while the project runs, while its
// host cannot say whether it runs, or when the new name is not one.
func TestRenameIsRefused(t *testing.T) {
	for _, tc := range []struct {
		name string
		to   string
		rt   func() *hosttest.FakeRuntime
		want string
	}{
		{"running", "b", func() *hosttest.FakeRuntime {
			rt := hosttest.NewRuntime("tmux")
			rt.Add("home", "")
			return rt
		}, "a is running; close its targets before renaming it"},
		{"host unknown", "b", func() *hosttest.FakeRuntime {
			rt := hosttest.NewRuntime("tmux")
			rt.SetInstancesErr(errors.New("no server"))
			return rt
		}, "wait for the host before renaming it"},
		{"a space in the name", "b c", func() *hosttest.FakeRuntime { return hosttest.NewRuntime("tmux") }, "contains whitespace"},
		{"a slash in the name", "b/c", func() *hosttest.FakeRuntime { return hosttest.NewRuntime("tmux") }, "contains '/'"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := renameApp(t, tc.rt())
			err := cmdRename(context.Background(), a, []string{"a", tc.to})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want a refusal %q", err, tc.want)
			}
			entries, _ := os.ReadDir(filepath.Join(a.cfgRoot, "projects"))
			if len(entries) != 1 || entries[0].Name() != "a.toml" {
				t.Errorf("projects = %v, want a.toml alone", entries)
			}
		})
	}
}

// A name no project has is named back, and so is the built-in assistant,
// which has no file to move.
func TestRenameNamesWhatItCannotFind(t *testing.T) {
	a := renameApp(t, hosttest.NewRuntime("tmux"))
	if err := cmdRename(context.Background(), a, []string{"nope", "b"}); err == nil || !strings.Contains(err.Error(), `no project named "nope"`) {
		t.Errorf("err = %v, want no project named nope", err)
	}
	if err := cmdRename(context.Background(), a, []string{"a"}); err == nil || err.Error() != renameUsage {
		t.Errorf("err = %v, want the usage", err)
	}
	builtin := withAssistant(&a.projects, a.cfgRoot, a.stateRoot)
	if err := cmdRename(context.Background(), a, []string{string(builtin.Name), "b"}); err == nil || !strings.Contains(err.Error(), "built in") {
		t.Errorf("err = %v, want the built-in refused", err)
	}
}
