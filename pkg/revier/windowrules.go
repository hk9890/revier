package revier

import "context"

// WindowRule is one placement rule of the window host's: the host places a
// window that matches it as the window appears, before revier finds it.
// revier writes the rules for the popup's class alone (decisions.md D189).
type WindowRule struct {
	// Class is the window class the rule matches. Empty for a rule that
	// matches by something else.
	Class string
	// MinWidth limits the rule to a monitor at least this many logical
	// pixels wide. 0 matches any monitor.
	MinWidth int
	// Place is the rule's x, y, width and height, in the spelling
	// WindowPlacer.Place reads, such as ["center", "top", "1800", "100%"].
	// Empty for a rule that places nothing.
	Place []string
	// Overlay keeps the window above every other window and out of the
	// window switcher, and hides it when another window takes the focus.
	Overlay bool
	// Other says the rule holds something the fields above do not carry:
	// a second match, a tile, a workspace. Such a rule is the user's.
	Other bool
	// Raw is the rule as the host stores it, all of it, so that the user can
	// put back a rule of theirs that `revier setup install --force`
	// replaced. Empty in a rule revier builds to write.
	Raw string
}

// WindowRules reads the window host's placement rules.
//
// It is read-only, as KeyBinder and TermKeys are: `revier setup status` is
// given it, and a status command has to be one that cannot change the
// user's configuration. Writing is WindowRuleWriter.
type WindowRules interface {
	// Name is the host this reads, for messages.
	Name() string
	// Read returns every rule, in the order the host tries them: the first
	// rule that matches a window is the one it applies.
	Read(ctx context.Context) ([]WindowRule, error)
}

// WindowRuleWriter changes the window host's placement rules.
type WindowRuleWriter interface {
	WindowRules
	// Add inserts the rule before the rule at index at; at equal to the
	// number of rules appends it.
	Add(ctx context.Context, at int, rule WindowRule) error
	// Remove deletes the rule at index.
	Remove(ctx context.Context, index int) error
}
