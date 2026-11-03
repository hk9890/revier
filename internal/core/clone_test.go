package core_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/pkg/revier"
)

// configured is a core with every exported field set, so a copy that drops
// one shows. A field added to Core and not set here fails the test, which
// names it.
func configured(t *testing.T) *core.Core {
	t.Helper()
	c := &core.Core{
		Runtime:   hosttest.NewRuntime("rt"),
		Window:    hosttest.New("wm"),
		Probes:    []revier.AgentProbe{hosttest.NewResumableProbe("claude", "claude")},
		Served:    hosttest.New("served"),
		Self:      func(revier.Panel) bool { return false },
		Run:       func(context.Context, []string, string) error { return nil },
		Machine:   "laptop",
		Remotes:   map[string]revier.Remote{"buildbox": hosttest.NewRemote("buildbox")},
		NewRemote: func(host string) revier.Remote { return hosttest.NewRemote(host) },
		Ledger:    &ledger{},
		KeyBinder: hosttest.NewKeys("gnome"),
		TermKeys:  hosttest.NewTermKeys(revier.TermKeyState{}),
	}
	v := reflect.ValueOf(c).Elem()
	for i := range v.NumField() {
		if f := v.Type().Field(i); f.IsExported() && v.Field(i).IsZero() {
			t.Fatalf("configured leaves Core.%s unset: set it, so the copy tests cover it", f.Name)
		}
	}
	return c
}

// sameField reports whether two values of one field are the same: a func by
// its code pointer, since funcs do not compare.
func sameField(a, b reflect.Value) bool {
	if a.Kind() == reflect.Func {
		return a.Pointer() == b.Pointer()
	}
	return reflect.DeepEqual(a.Interface(), b.Interface())
}

func assertKeeps(t *testing.T, was, got *core.Core, changed string) {
	t.Helper()
	w, g := reflect.ValueOf(was).Elem(), reflect.ValueOf(got).Elem()
	for i := range w.NumField() {
		f := w.Type().Field(i)
		if !f.IsExported() || f.Name == changed {
			continue
		}
		if !sameField(w.Field(i), g.Field(i)) {
			t.Errorf("the copy lost Core.%s", f.Name)
		}
	}
}

func TestWithLedgerKeepsEveryOtherField(t *testing.T) {
	c := configured(t)
	assertKeeps(t, c, c.WithLedger(&ledger{}), "Ledger")
}

func TestWithRuntimeKeepsEveryOtherField(t *testing.T) {
	c := configured(t)
	assertKeeps(t, c, c.WithRuntime(hosttest.NewRuntime("other")), "Runtime")
}
