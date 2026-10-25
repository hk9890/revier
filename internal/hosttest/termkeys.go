package hosttest

import (
	"context"
	"slices"

	"github.com/hk9890/revier/pkg/revier"
)

// FakeTermKeys is an in-memory TermKeys: a terminal configuration a test
// sets up and reads back, so layer L2 drives `revier term` with no files.
type FakeTermKeys struct {
	State revier.TermKeyState
	// ReadErr makes Read fail, for the error path.
	ReadErr error
}

// NewTermKeys returns a fake terminal holding state.
func NewTermKeys(state revier.TermKeyState) *FakeTermKeys {
	return &FakeTermKeys{State: state}
}

func (f *FakeTermKeys) Name() string { return "fake-terminal" }

func (f *FakeTermKeys) Read(context.Context) (revier.TermKeyState, error) {
	return f.State, f.ReadErr
}

// FakeTermWriter is a FakeTermKeys that changes what it holds as a terminal
// adapter does: Write puts the include on the first line when there is none,
// and the main configuration's own lines move down below it.
type FakeTermWriter struct {
	*FakeTermKeys
	// WriteErr makes Write and Remove fail before they change anything.
	WriteErr error
	// IncludeErr makes Write fail after it wrote the key file, before the
	// include: a run that failed half way.
	IncludeErr error
	// Calls records every change, in order: "write" or "remove".
	Calls []string
}

// NewTermWriter returns a fake terminal holding state that can be written.
func NewTermWriter(state revier.TermKeyState) *FakeTermWriter {
	return &FakeTermWriter{FakeTermKeys: NewTermKeys(state)}
}

func (f *FakeTermWriter) Write(_ context.Context, bindings []revier.TermBinding) error {
	f.Calls = append(f.Calls, "write")
	if f.WriteErr != nil {
		return f.WriteErr
	}
	f.State.FileExists = true
	f.State.Bindings = slices.Clone(bindings)
	if f.IncludeErr != nil {
		return f.IncludeErr
	}
	if f.State.Include == 0 {
		f.State.ConfigExists = true
		f.State.Include, f.State.IncludeOwned = 2, true
		f.shift(2)
	}
	return nil
}

func (f *FakeTermWriter) Remove(context.Context) error {
	f.Calls = append(f.Calls, "remove")
	if f.WriteErr != nil {
		return f.WriteErr
	}
	f.State.FileExists = false
	f.State.Bindings = nil
	if f.State.IncludeOwned {
		f.State.Include, f.State.IncludeOwned = 0, false
		f.shift(-2)
	}
	return nil
}

func (f *FakeTermWriter) shift(by int) {
	for i := range f.State.Maps {
		f.State.Maps[i].Line += by
	}
}
