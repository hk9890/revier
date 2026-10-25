package revier

import "context"

// TermBinding is one key a terminal holds for revier: a press of Chord runs
// revier with Args, beside the terminal window it was pressed in.
//
// Chord is the core's canonical spelling (core.Chord). The adapter writes it
// in the terminal's own key names, and reads it back in a spelling
// core.ParseChord reads. Args is revier's argv after "revier", such as
// ["agent", "new", "dtop"]; how the terminal passes its window to revier is
// the adapter's.
type TermBinding struct {
	Chord string
	Args  []string
}

// TermMap is one key the terminal's main configuration maps itself, outside
// revier's file.
type TermMap struct {
	// Chord is the key, in a spelling core.ParseChord reads.
	Chord string
	// Line is the line of the main configuration that maps it, from 1.
	Line int
	// Condition is what limits the map to some windows, such as kitty's
	// `--when-focus-on var:in_tmux`. Empty when the map holds everywhere.
	Condition string
}

// TermKeyState is what the terminal's configuration holds of revier's keys.
type TermKeyState struct {
	// File is revier's own key file. FileExists says whether it is there,
	// and Bindings is what it holds, read back.
	File       string
	FileExists bool
	Bindings   []TermBinding
	// Config is the terminal's main configuration file, and ConfigExists
	// whether it is there.
	Config       string
	ConfigExists bool
	// Include is the line of Config that includes File, from 1; 0 when no
	// line does. IncludeOwned says whether it is the include revier wrote
	// and removes, or one the user wrote.
	Include      int
	IncludeOwned bool
	// Maps is every key Config maps itself, in file order. A later map of a
	// key wins over an earlier one, so a map after Include shadows revier's.
	Maps []TermMap
}

// TermKeys reads the keys a terminal holds for revier.
//
// It is read-only, as KeyBinder is: `revier term status` is given it, and a
// status command has to be one that cannot change the user's configuration.
// Writing is TermKeyWriter, which an implementation keeps behind a separate
// type.
type TermKeys interface {
	// Name is the terminal this reads, for messages.
	Name() string
	Read(ctx context.Context) (TermKeyState, error)
}

// TermKeyWriter changes the keys a terminal holds for revier.
type TermKeyWriter interface {
	TermKeys
	// Write makes File hold exactly the bindings, and makes Config include
	// File when no line of it does. A file that already holds what Write
	// would write is not written.
	Write(ctx context.Context, bindings []TermBinding) error
	// Remove deletes File and the include revier wrote. An include the user
	// wrote stays.
	Remove(ctx context.Context) error
}
