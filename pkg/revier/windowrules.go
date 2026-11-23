package revier

import "context"

// WindowRule is one placement rule of the window host's: the host places a
// window that matches it as the window appears, before revier finds it.
// revier writes the rules for the popup's class alone, into a file of its
// own.
type WindowRule struct {
	// Class is the window class the rule matches. Empty for a rule that
	// matches by something else.
	Class string `json:"class,omitempty"`
	// MinWidth limits the rule to a monitor at least this many logical
	// pixels wide. 0 matches any monitor.
	MinWidth int `json:"min_width,omitempty"`
	// Place is the rule's x, y, width and height, in the spelling
	// WindowPlacer.Place reads, such as ["center", "top", "1800", "100%"].
	// Empty for a rule that places nothing.
	Place []string `json:"place,omitempty"`
	// Overlay keeps the window above every other window and out of the
	// window switcher, and hides it when another window takes the focus.
	Overlay bool `json:"overlay,omitempty"`
	// Other says the rule holds something the fields above do not carry:
	// a second match, a tile, a workspace. Such a rule is the user's.
	Other bool `json:"other,omitempty"`
	// Raw is the rule as the host stores it, all of it, so that the user can
	// put back a rule that `revier setup install` removed. Empty in a rule
	// revier builds to write.
	Raw string `json:"raw,omitempty"`
}

// WindowRuleFile is one rules file of the window host's and the rules in it,
// in file order.
type WindowRuleFile struct {
	Path  string       `json:"path"`
	Rules []WindowRule `json:"rules"`
}

// WindowRuleSet is every rules file of the window host's that bears on
// revier's, in the order the host tries them: the first rule that matches a
// window is the one it applies.
type WindowRuleSet struct {
	// User is the user's own rules file, which the host tries first. Its
	// Rules are empty when UserBroken says why the host would not load it:
	// the host then loads the other files without it.
	User       WindowRuleFile `json:"user"`
	UserBroken string         `json:"user_broken,omitempty"`
	// Earlier is each file of another program's that the host tries after
	// the user's and before revier's, in that order. A file the host would
	// not load is left out: it hides nothing.
	Earlier []WindowRuleFile `json:"earlier"`
	// Own is revier's own file. Its Rules are empty when OwnExists is false,
	// and when OwnBroken says why the host would not load it.
	Own       WindowRuleFile `json:"own"`
	OwnExists bool           `json:"own_exists"`
	OwnBroken string         `json:"own_broken,omitempty"`
}

// WindowRules reads the window host's placement rules.
//
// It is read-only, as KeyBinder and TermKeys are: `revier setup status` is
// given it, and a status command has to be one that cannot change the
// user's configuration. Writing is WindowRuleWriter.
type WindowRules interface {
	// Name is the host this reads, for messages.
	Name() string
	Read(ctx context.Context) (WindowRuleSet, error)
}

// WindowRuleWriter changes the window host's placement rules.
type WindowRuleWriter interface {
	WindowRules
	// RemoveUser deletes the rule at index in the user's file.
	RemoveUser(ctx context.Context, index int) error
	// WriteOwn replaces revier's own file with rules, whole: the host never
	// reads half of it, and never a file it would refuse.
	WriteOwn(ctx context.Context, rules []WindowRule) error
	// RemoveOwn deletes revier's own file. A file already gone is no error.
	RemoveOwn(ctx context.Context) error
}
