// The cleanup of an Open or an OpenTab whose step fails, against a kitty that
// keeps the windows it launched. No recorded output and no kitty.
package kitty_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/hk9890/revier/pkg/revier"
)

// statefulKitty is a kitty that keeps the windows the host launches, with their
// user vars, and closes the ones a `var:` match names. fail picks the call
// that fails; a launch it picks makes its window all the same, as one whose
// answer the end of the press's context cut off.
type statefulKitty struct {
	osWindows  []*fakeOSWindow
	nextWindow int
	calls      []string
	fail       func(args []string) bool
}

type fakeOSWindow struct {
	id      int
	name    string
	windows []fakeWindow
}

type fakeWindow struct {
	id   int
	vars map[string]string
}

func (k *statefulKitty) run(ctx context.Context, _, _ string, args ...string) ([]byte, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	k.calls = append(k.calls, strings.Join(args, " "))
	var out []byte
	switch args[0] {
	case "ls":
		var listing []map[string]any
		for _, w := range k.osWindows {
			var windows []map[string]any
			for _, win := range w.windows {
				windows = append(windows, map[string]any{"id": win.id, "user_vars": win.vars})
			}
			listing = append(listing, map[string]any{"id": w.id, "wm_name": w.name,
				"tabs": []map[string]any{{"windows": windows}}})
		}
		out, _ = json.Marshal(listing)
	case "launch":
		out = []byte(strconv.Itoa(k.launch(args[1:])) + "\n")
	case "close-window":
		k.close(args[2])
	}
	if k.fail != nil && k.fail(args) {
		return nil, errors.New("kitty went away")
	}
	return out, nil
}

func (k *statefulKitty) launch(args []string) int {
	win := fakeWindow{id: k.nextWindow, vars: map[string]string{}}
	k.nextWindow++
	var name string
	into := 0
	for i := 0; i < len(args)-1; i++ {
		switch args[i] {
		case "--var":
			key, value, _ := strings.Cut(args[i+1], "=")
			win.vars[key] = value
		case "--os-window-name":
			name = args[i+1]
		case "--match":
			into, _ = strconv.Atoi(strings.TrimPrefix(args[i+1], "window_id:"))
		}
	}
	if slices.Contains(args, "--type=os-window") {
		k.osWindows = append(k.osWindows, &fakeOSWindow{id: 100 + win.id, name: name, windows: []fakeWindow{win}})
		return win.id
	}
	for _, w := range k.osWindows {
		if slices.ContainsFunc(w.windows, func(x fakeWindow) bool { return x.id == into }) {
			w.windows = append(w.windows, win)
		}
	}
	return win.id
}

// close closes the windows a match of `id:` and `var:` terms joined by "or"
// names.
func (k *statefulKitty) close(match string) {
	matches := func(x fakeWindow) bool {
		for _, term := range strings.Split(match, " or ") {
			if id, ok := strings.CutPrefix(term, "id:"); ok && id == strconv.Itoa(x.id) {
				return true
			}
			if v, ok := strings.CutPrefix(term, "var:"); ok {
				name, value, _ := strings.Cut(v, "=")
				if x.vars[name] == value {
					return true
				}
			}
		}
		return false
	}
	for _, w := range k.osWindows {
		w.windows = slices.DeleteFunc(w.windows, matches)
	}
	k.osWindows = slices.DeleteFunc(k.osWindows, func(w *fakeOSWindow) bool { return len(w.windows) == 0 })
}

// windowIDs are the ids of the windows each OS window holds, by OS window id.
func (k *statefulKitty) windowIDs() map[int][]int {
	ids := map[int][]int{}
	for _, w := range k.osWindows {
		for _, win := range w.windows {
			ids[w.id] = append(ids[w.id], win.id)
		}
	}
	return ids
}

// An Open whose launch, title or closing ls fails closes the OS window it
// opened, every window in it: left running, the half-built OS window carries
// the workspace's name, and the next press would raise it, an agent with no
// shell beside it. A launch can fail after kitty made its window, when the end
// of the press's context ends kitten before kitty's answer, so the windows are
// found by the mark every launch of this Open gave them, not by the ids Open
// got back. An OS window of the same name that another press opened, before
// or meanwhile, carries another mark and stays. The failing step can be the
// end of the context, so the close has a context of its own.
func TestOpenClosesTheOSWindowItOpenedWhenAStepFails(t *testing.T) {
	for _, tc := range []struct {
		name string
		fail func(args []string, lists int) bool
	}{
		{"first launch unanswered", func(args []string, _ int) bool { return slices.Contains(args, "--type=os-window") }},
		{"title", func(args []string, _ int) bool { return args[0] == "set-window-title" }},
		{"later launch unanswered", func(args []string, _ int) bool { return slices.Contains(args, "--type=window") }},
		{"closing ls", func(args []string, lists int) bool { return args[0] == "ls" && lists > 1 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHost()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			h.SetSockets(func() []string { return []string{"unix:/run/user/1000/revier-kitty-4000"} })
			k := &statefulKitty{nextWindow: 7, osWindows: []*fakeOSWindow{
				{id: 1, name: "revier-kitty", windows: []fakeWindow{{id: 1}}},
				{id: 2, name: "session:demo", windows: []fakeWindow{{id: 5}}},
			}}
			lists, failed := 0, false
			k.fail = func(args []string) bool {
				if args[0] == "ls" {
					lists++
				}
				if failed || !tc.fail(args, lists) {
					return false
				}
				failed = true
				cancel()
				return true
			}
			h.SetRunner(func(ctx context.Context, socket, stdin string, args ...string) ([]byte, error) {
				if args[0] == "launch" && slices.Contains(args, "--type=os-window") {
					// Another press opens the same workspace meanwhile.
					k.osWindows = append(k.osWindows, &fakeOSWindow{id: 3, name: "session:demo",
						windows: []fakeWindow{{id: 6, vars: map[string]string{"REVIER_KITTY_OPEN": "other"}}}})
				}
				return k.run(ctx, socket, stdin, args...)
			})
			_, err := h.Open(ctx, revier.Realization{
				Name: "session:demo", Match: revier.Match{Title: "^session:demo$"},
				Panels: []revier.PanelSpec{
					{Kind: revier.PanelAgent, Title: "agent", Command: []string{"claude"}},
					{Kind: revier.PanelShell, Title: "shell"},
				},
			})
			if err == nil || !strings.Contains(err.Error(), "kitty went away") {
				t.Fatalf("err = %v, want the failing step's error", err)
			}
			want := map[int][]int{1: {1}, 2: {5}, 3: {6}}
			if got := k.windowIDs(); !reflect.DeepEqual(got, want) {
				t.Errorf("windows left = %v, want %v: Open's own closed, and no other's\ncalls: %q", got, want, k.calls)
			}
		})
	}
}

// The mark a launch gives a window is no panel variable: a listing reports a
// panel's vars without it.
func TestAPanelsVarsLeaveTheOpenMarkOut(t *testing.T) {
	h := newHost()
	h.SetSockets(func() []string { return []string{"unix:/run/user/1000/revier-kitty-4000"} })
	k := &statefulKitty{osWindows: []*fakeOSWindow{{id: 2, name: "session:demo", windows: []fakeWindow{
		{id: 5, vars: map[string]string{"REVIER_KITTY_OPEN": "M", "revier_home": "home"}},
	}}}}
	h.SetRunner(k.run)
	got, err := h.Instances(context.Background())
	if err != nil {
		t.Fatalf("Instances: %v", err)
	}
	if len(got) != 1 || len(got[0].Panels) != 1 {
		t.Fatalf("Instances = %+v, want one instance with one panel", got)
	}
	if vars := got[0].Panels[0].Vars; !reflect.DeepEqual(vars, map[string]string{"revier_home": "home"}) {
		t.Errorf("Vars = %v, want revier_home alone", vars)
	}
}

// A tab whose launch fails closes every window the tab opened, the one of a
// launch whose answer was lost included: the core names that agent as not
// added, and it must not run on in a tab without its shell. The failing step
// can be the end of the press's context, so the close has a context of its
// own.
func TestOpenTabClosesWhatItOpenedWhenALaunchFails(t *testing.T) {
	for _, tc := range []struct {
		name   string
		failAt int
	}{
		{"tab launch unanswered", 1},
		{"panel launch unanswered", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHost()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			h.SetSockets(func() []string { return []string{"unix:/run/user/1000/revier-kitty-4000"} })
			k := &statefulKitty{nextWindow: 7, osWindows: []*fakeOSWindow{
				{id: 2, name: "session:demo", windows: []fakeWindow{{id: 5}}},
			}}
			launches := 0
			k.fail = func(args []string) bool {
				if args[0] != "launch" {
					return false
				}
				launches++
				if launches != tc.failAt {
					return false
				}
				cancel()
				return true
			}
			h.SetRunner(k.run)
			_, err := h.OpenTab(ctx, revier.TargetRef{Host: "kitty", ID: "/run/user/1000/revier-kitty-4000/2"}, revier.Realization{
				Panels: []revier.PanelSpec{
					{Kind: revier.PanelAgent, Command: []string{"claude", "--resume", "b"}},
					{Kind: revier.PanelShell},
				},
			}, nil)
			if err == nil || !strings.Contains(err.Error(), "kitty went away") {
				t.Fatalf("err = %v, want the launch's error", err)
			}
			if got, want := k.windowIDs(), map[int][]int{2: {5}}; !reflect.DeepEqual(got, want) {
				t.Errorf("windows left = %v, want %v: the tab's windows closed\ncalls: %q", got, want, k.calls)
			}
		})
	}
}
