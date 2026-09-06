package revier

import "context"

// BindingSource says where the desktop keeps a shortcut, because the two
// places behave differently. A custom binding is one a user added and revier
// may one day claim; a built-in belongs to the desktop itself and holding a
// chord there is a conflict revier reports rather than resolves.
type BindingSource string

const (
	BindingCustom  BindingSource = "custom"
	BindingBuiltin BindingSource = "builtin"
)

// Binding is one desktop keyboard shortcut, exactly as the desktop stores it.
//
// Chord is the desktop's own spelling - GNOME writes "<Shift><Control>u" -
// and is not normalised here: comparing two spellings is policy, so it
// happens in the core (see core.Chord).
type Binding struct {
	// Chord is the key combination, in the desktop's spelling.
	Chord string
	// Command is what the chord runs. A built-in has none: it names an action
	// inside the desktop rather than a process.
	Command string
	// Label is what the desktop shows for the binding: a custom entry's name,
	// or a built-in's settings key.
	Label string
	// Where the desktop keeps it: a dconf path, or a schema name.
	Where string
	// Enabled is whether the desktop will act on the chord. A custom entry
	// can exist in the store and still be ignored, which is invisible in the
	// settings UI and is the state this field exists to report.
	Enabled bool
	Source  BindingSource
}

// KeyBinder reads the desktop's keyboard shortcuts.
//
// It is read-only on purpose. `revier keys status` answers "who holds this
// chord", which needs listing and nothing else; claiming a chord is a later
// change and will add its own methods here.
type KeyBinder interface {
	// Name is the desktop this reads, for messages.
	Name() string
	// List returns every shortcut the desktop holds, in one bulk read. The
	// same rule applies as to Host.Instances: the cost may not scale with the
	// number of projects.
	List(ctx context.Context) ([]Binding, error)
}
