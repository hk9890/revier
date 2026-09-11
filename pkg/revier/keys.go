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
	// ID is the desktop's own name for the entry, which is what makes a
	// shortcut addressable: GNOME's `custom0`, or `revier-home` for one revier
	// wrote. A built-in has none - it is a setting, not an entry.
	ID string
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
// It is read-only, and an implementation of it must stay that way: this is
// what `revier keys status` is given, and a status command an agent may run
// against a live desktop has to be one that cannot change it. Writing is
// KeyWriter, which an implementation keeps behind a separate type.
type KeyBinder interface {
	// Name is the desktop this reads, for messages.
	Name() string
	// List returns every shortcut the desktop holds, in one bulk read. The
	// same rule applies as to Host.Instances: the cost may not scale with the
	// number of projects.
	List(ctx context.Context) ([]Binding, error)
}

// KeyWriter changes the desktop's keyboard shortcuts.
//
// The three verbs are separate because they are not reversible in the same
// way. Removing an entry destroys what it held; switching one off leaves every
// word of it in the desktop's store, so the thing that wrote it can put it
// back. Which one a shortcut deserves is policy: revier removes only what it
// wrote itself, and switches off what belongs to somebody else.
type KeyWriter interface {
	KeyBinder
	// Bind creates or replaces the shortcut named by b.ID and makes the
	// desktop act on it. b.Chord is in the desktop's spelling, b.Where is
	// ignored: where a new entry goes is the desktop's choice, not revier's.
	Bind(ctx context.Context, b Binding) error
	// Disable stops the desktop acting on a shortcut, leaving its definition
	// in place. On a built-in this takes b.Chord out of the setting, and
	// leaves any other key the setting holds.
	Disable(ctx context.Context, b Binding) error
	// Remove deletes a shortcut and everything it held. It is refused for a
	// built-in, which is a desktop setting and not an entry to delete.
	Remove(ctx context.Context, b Binding) error
}
