package core_test

import (
	"testing"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/pkg/revier"
)

func projectAt(name, path string) core.Project {
	return core.Project{Project: revier.Project{Name: revier.ProjectName(name), Path: path}}
}

// dirs is a filesystem as Select sees it: path to real directory. A path
// missing from it does not exist.
func dirs(m map[string]string) func(string) (string, bool) {
	return func(p string) (string, bool) {
		real, ok := m[p]
		return real, ok
	}
}

func skips(picks []core.Pick) map[revier.ProjectName]core.Skip {
	out := map[revier.ProjectName]core.Skip{}
	for _, p := range picks {
		out[p.Project.Name] = p.Skip
	}
	return out
}

func TestSelectRunsEveryProjectThatHasADirectory(t *testing.T) {
	picks := core.Select([]core.Project{
		projectAt("a", "/a"), projectAt("gone", "/gone"), projectAt("b", "/b"),
	}, dirs(map[string]string{"/a": "/a", "/b": "/b"}), nil)

	want := map[revier.ProjectName]core.Skip{"a": "", "gone": core.SkipMissing, "b": ""}
	for name, skip := range want {
		if got := skips(picks)[name]; got != skip {
			t.Errorf("%s: skip = %q, want %q", name, got, skip)
		}
	}
	// The summary and the run both read this order; a map-ordered selection
	// would print ninety projects in a different order every time.
	for i, name := range []revier.ProjectName{"a", "gone", "b"} {
		if picks[i].Project.Name != name {
			t.Fatalf("pick %d = %s, want %s: the project order must survive", i, picks[i].Project.Name, name)
		}
	}
}

// A project on another machine has no directory here to run in, whatever
// its path resolves to on this one: the path is in the host's terms.
func TestSelectLeavesARemoteProjectOut(t *testing.T) {
	far := projectAt("far", "/a")
	far.Remote = &revier.Link{Host: "buildbox", Project: "far"}
	picks := core.Select([]core.Project{far, projectAt("a", "/a")}, dirs(map[string]string{"/a": "/a"}), nil)

	if got := skips(picks); got["far"] != core.SkipRemote || got["a"] != "" {
		t.Errorf("skips = %v, want far remote and a run", got)
	}
}

// Two projects on one checkout - a symlink, or two project files for one
// repository - are one directory. The command runs there once.
func TestSelectRunsOneDirectoryOnce(t *testing.T) {
	picks := core.Select([]core.Project{
		projectAt("first", "/repo"), projectAt("alias", "/link-to-repo"),
	}, dirs(map[string]string{"/repo": "/repo", "/link-to-repo": "/repo"}), nil)

	if picks[0].Skip != "" {
		t.Errorf("first claimant skipped: %q", picks[0].Skip)
	}
	if picks[1].Skip != core.SkipDuplicate || picks[1].SameAs != "first" {
		t.Errorf("alias = %+v, want a duplicate of first", picks[1])
	}
}

// The filter runs a process in the directory, so it is never asked about one
// that does not exist or that another project already covers.
func TestSelectAsksTheFilterOnlyAboutDirectoriesThatWouldRun(t *testing.T) {
	var asked []string
	keep := func(dir string) bool {
		asked = append(asked, dir)
		return dir == "/git"
	}
	picks := core.Select([]core.Project{
		projectAt("git", "/git"), projectAt("plain", "/plain"),
		projectAt("gone", "/gone"), projectAt("again", "/git-link"),
	}, dirs(map[string]string{"/git": "/git", "/plain": "/plain", "/git-link": "/git"}), keep)

	want := map[revier.ProjectName]core.Skip{
		"git": "", "plain": core.SkipFiltered, "gone": core.SkipMissing, "again": core.SkipDuplicate,
	}
	for name, skip := range want {
		if got := skips(picks)[name]; got != skip {
			t.Errorf("%s: skip = %q, want %q", name, got, skip)
		}
	}
	if len(asked) != 2 || asked[0] != "/git" || asked[1] != "/plain" {
		t.Errorf("filter asked about %v, want [/git /plain]", asked)
	}
}
