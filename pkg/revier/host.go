package revier

import "context"

// Host provides instances of targets. A terminal or multiplexer provides them
// as panes and windows; a compositor provides them as OS windows. The core asks
// the same questions of both, which is why run-or-raise is one mechanism rather
// than two.
//
// An adapter holds no policy. Matching, template rendering, choosing between
// realizations, and toggle-back all live in the core, so two adapters cannot
// disagree about what a match means.
type Host interface {
	Name() string

	// Probe reports whether this host is usable in the current environment,
	// returning nil when it is. Selection depends on it being honest: an
	// adapter that probes successfully will be used.
	Probe(ctx context.Context) error

	// Instances lists everything this host currently holds.
	//
	// This is the hot path. The TUI calls it on every refresh, so an
	// implementation issues one bulk query - `kitten @ ls`, `tmux list-panes
	// -a -F`, `swaymsg -t get_tree` - never one call per project.
	Instances(ctx context.Context) ([]Instance, error)

	// Open creates an instance from an already-rendered realization.
	Open(ctx context.Context, r Realization) (TargetRef, error)

	Focus(ctx context.Context, ref TargetRef) error

	// Focused reports this host's current instance. It is authoritative only
	// for the host that owns OS focus, so the core prefers a
	// WindowController's answer when one is configured.
	Focused(ctx context.Context) (TargetRef, error)
}

// Runtime hosts the terminal side: panes, layouts, and the panels an AgentProbe
// reads. It is the only host whose instances carry panels.
type Runtime interface {
	Host
	Capabilities() Capabilities
}

// WindowController hosts foreign OS windows. It adds nothing to Host; the named
// type exists so the wiring states which role an adapter fills.
type WindowController interface {
	Host
}

// Capabilities describes what a runtime can do. A runtime that cannot arrange
// panes reports Layout false and opens a single pane; the core handles that
// case rather than the adapter faking it.
type Capabilities struct {
	Layout     bool // can arrange panels from a PanelSpec list
	Persistent bool // an instance survives its client exiting

	// OSWindows reports that every instance is an OS window, and that the
	// host gives it the same title a WindowController reports for it. The
	// core then treats the two listings as one window seen from two sides:
	// it raises the OS window through the window host, which a terminal on
	// Wayland cannot do for itself, and it judges toggle-back for a runtime
	// target by OS focus. A multiplexer inside a terminal cannot claim this.
	OSWindows bool
}

// Instance is one live thing a host holds: a kitty OS window, a tmux pane, a
// Meld window. Panels is populated by runtime hosts only.
type Instance struct {
	Ref    TargetRef `json:"ref"`
	Title  string    `json:"title"`
	Class  string    `json:"class,omitempty"`
	PID    int       `json:"pid,omitempty"`
	Panels []Panel   `json:"panels,omitempty"`
}

// TargetRef points at a live instance. Host names which host produced it, so
// the core routes Focus back without tracking the association separately.
type TargetRef struct {
	Host  string `json:"host"`
	ID    string `json:"id"`
	Title string `json:"title,omitempty"`
}

// IsZero reports whether the ref points at nothing.
func (r TargetRef) IsZero() bool { return r.Host == "" && r.ID == "" }

// WindowWatcher is an optional capability, detected by type assertion. A host
// that implements it enables claim-on-appear, which is how a window opened by
// `xdg-open` becomes bound to the project it was opened from. A host that does
// not still supports declared targets and explicit attach.
type WindowWatcher interface {
	Watch(ctx context.Context) (<-chan WindowEvent, error)
}

// WindowPlacer is an optional capability, detected by type assertion. A host
// that implements it can position a window it did not open, which is how a
// launched workspace lands where the user expects rather than where the
// compositor put it (decisions.md D24). A host that does not implement it
// ignores every declared placement.
//
// Geometry is four tokens - x, y, width, height - in the host's own
// vocabulary of pixels and workarea-relative words.
type WindowPlacer interface {
	Place(ctx context.Context, ref TargetRef, geometry []string) error
}

type WindowEvent struct {
	Kind     WindowEventKind
	Instance Instance
}

type WindowEventKind uint8

const (
	WindowOpened WindowEventKind = iota
	WindowClosed
	WindowFocused
)
