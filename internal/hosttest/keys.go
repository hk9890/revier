package hosttest

import (
	"context"
	"errors"

	"github.com/hk9890/revier/pkg/revier"
)

// FakeKeys is an in-memory KeyBinder. The desktop's shortcut store is the one
// thing `revier keys status` reads, so a fake of it drives the whole command
// with no desktop, which is what layer L2 needs.
type FakeKeys struct {
	name     string
	Bindings []revier.Binding
	// ListErr makes List fail, for the error path.
	ListErr error
	// ListCalls counts List calls, so a test can assert one report costs one
	// read.
	ListCalls int
}

// NewKeys returns a fake keybinder for the named desktop.
func NewKeys(name string, bindings ...revier.Binding) *FakeKeys {
	return &FakeKeys{name: name, Bindings: bindings}
}

func (f *FakeKeys) Name() string { return f.name }

func (f *FakeKeys) List(context.Context) ([]revier.Binding, error) {
	f.ListCalls++
	if f.ListErr != nil {
		return nil, f.ListErr
	}
	return f.Bindings, nil
}

// FakeWriter is a FakeKeys that also changes what it holds. It keeps the
// bindings it was given, so a test asserts on the desktop as it ends up rather
// than on the calls that got it there - which is what a user sees.
type FakeWriter struct {
	*FakeKeys
	// BindErr makes Bind fail, for the error path a plan has to survive.
	BindErr error
	// Calls records every change, in order, as "verb id".
	Calls []string
}

// NewWriter returns a fake desktop that can be written to.
func NewWriter(name string, bindings ...revier.Binding) *FakeWriter {
	return &FakeWriter{FakeKeys: NewKeys(name, bindings...)}
}

func (f *FakeWriter) Bind(_ context.Context, b revier.Binding) error {
	f.Calls = append(f.Calls, "bind "+b.ID)
	if f.BindErr != nil {
		return f.BindErr
	}
	b.Enabled = true
	if b.Where == "" {
		b.Where = "/custom-keybindings/" + b.ID + "/"
	}
	for i, existing := range f.Bindings {
		if existing.ID == b.ID {
			f.Bindings[i] = b
			return nil
		}
	}
	f.Bindings = append(f.Bindings, b)
	return nil
}

func (f *FakeWriter) Disable(_ context.Context, b revier.Binding) error {
	f.Calls = append(f.Calls, "disable "+label(b))
	for i, existing := range f.Bindings {
		if same(existing, b) {
			f.Bindings[i].Enabled = false
			return nil
		}
	}
	return nil
}

func (f *FakeWriter) Remove(_ context.Context, b revier.Binding) error {
	f.Calls = append(f.Calls, "remove "+label(b))
	if b.Source == revier.BindingBuiltin {
		return errors.New("a desktop setting is not an entry to remove")
	}
	out := f.Bindings[:0]
	for _, existing := range f.Bindings {
		if !same(existing, b) {
			out = append(out, existing)
		}
	}
	f.Bindings = out
	return nil
}

// Held reports the enabled shortcuts on a chord, as the desktop spells it.
func (f *FakeWriter) Held(chord string) []revier.Binding {
	var out []revier.Binding
	for _, b := range f.Bindings {
		if b.Chord == chord && b.Enabled {
			out = append(out, b)
		}
	}
	return out
}

// same reports whether two bindings are one shortcut. A built-in is one key
// of a setting that may hold several, as the desktop's own writer treats it.
func same(a, b revier.Binding) bool {
	if a.Source != b.Source {
		return false
	}
	if a.Source == revier.BindingBuiltin {
		return a.Where == b.Where && a.Label == b.Label && a.Chord == b.Chord
	}
	return a.ID == b.ID
}

func label(b revier.Binding) string {
	if b.ID != "" {
		return b.ID
	}
	return b.Label
}

// Custom builds an enabled custom shortcut, the common case in a test.
func Custom(chord, command, name string) revier.Binding {
	return revier.Binding{
		ID: name, Chord: chord, Command: command, Label: name,
		Where:   "/org/gnome/settings-daemon/plugins/media-keys/custom-keybindings/" + name + "/",
		Enabled: true, Source: revier.BindingCustom,
	}
}

// Builtin builds a desktop shortcut: one revier can report but not claim.
func Builtin(chord, schema, key string) revier.Binding {
	return revier.Binding{
		Chord: chord, Label: key, Where: schema,
		Enabled: true, Source: revier.BindingBuiltin,
	}
}
