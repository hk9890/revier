package hosttest

import (
	"context"

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

// Custom builds an enabled custom shortcut, the common case in a test.
func Custom(chord, command, label string) revier.Binding {
	return revier.Binding{
		Chord: chord, Command: command, Label: label,
		Where:   "/org/gnome/settings-daemon/plugins/media-keys/custom-keybindings/" + label + "/",
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
