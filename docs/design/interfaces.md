# Interfaces

Everything here lives in `pkg/revier`. The rationale for the port set is in
[architecture.md](architecture.md); this document is the contract.

## Project and target

```go
type ProjectName string
type TargetName string

// Project is a directory and the set of targets bound to it.
type Project struct {
    Name    ProjectName
    Path    string
    GitURL  string // what `revier open` clones when Path is missing (D30)
    Targets []Target
    Vars    map[string]string // template values for launches
}

// Target is a named thing you reach with a key: the workspace, an editor, a
// diff viewer, a page. What a target *is* depends on the host that provides
// it; the name and the key do not.
type Target struct {
    Name TargetName
    Key  string // empty: reachable only from the project picker
    Home bool   // the project's workspace; toggle-back returns here

    // Prefer names the host to use when both realizations are available:
    // "window" or "runtime". Empty applies the default rule.
    Prefer string

    Window  *Realization
    Runtime *Realization
}

// Realization is how one host provides a target.
type Realization struct {
    // Name is the identity a host gives a new instance: a tmux window name, a
    // kitty OS window title, the --class a browser is launched with. It is how
    // a host that assigns its own identity satisfies the Open invariant.
    Name string

    Launch []string   // argv, used when Match finds nothing; a runtime realization with Panels may omit it
    Dir    string     // working directory for Launch and every panel; the project path when empty
    Match  Match      // how to recognise an existing instance
    Panels []PanelSpec // runtime hosts only: the layout for a Home target
}

// Match recognises an instance. An empty field does not constrain; every
// non-empty field must match. Title and Class are regular expressions.
type Match struct {
    Class string // WM class on X11, app_id on Wayland; window hosts only
    Title string
    PID   int
}
```

## Host

One interface, two kinds of implementation. A terminal or multiplexer provides
targets as panes and windows; a compositor provides them as OS windows. The core
does not care which answered.

```go
type Host interface {
    Name() string

    // Probe reports whether this host is usable in the current environment.
    // It returns nil when it is.
    Probe(ctx context.Context) error

    // Instances lists everything this host currently holds. The core matches
    // these against project realizations itself, so an adapter never
    // implements matching, and the TUI refresh costs one call per host rather
    // than one per project.
    Instances(ctx context.Context) ([]Instance, error)

    // Open creates an instance. The Realization it receives is already
    // rendered: hosts never see a template.
    //
    // The invariant: what Open creates, the same realization's Match must
    // find. A host that breaks it opens a second instance on every keypress.
    Open(ctx context.Context, r Realization) (TargetRef, error)

    Focus(ctx context.Context, ref TargetRef) error

    // Focused reports this host's current instance. It is authoritative only
    // for the host that owns OS focus, so the core prefers the
    // WindowController's answer when one is configured.
    Focused(ctx context.Context) (TargetRef, error)
}

// Instance is one live thing a host holds: a kitty OS window, a tmux pane, a
// Meld window. Panels is populated by runtime hosts only.
type Instance struct {
    Ref    TargetRef
    Title  string
    Class  string
    PID    int
    Panels []Panel
}

// TargetRef points at a live instance. Host names which host produced it, so
// the core can route Focus back without tracking it separately.
type TargetRef struct {
    Host  string
    ID    string // host-scoped: kitty window id, tmux pane id, WM window id
    Title string
}
```

## Runtime and WindowController

```go
// Runtime hosts the terminal side: panes, layouts, and the panels an
// AgentProbe reads.
type Runtime interface {
    Host
    Capabilities() Capabilities
}

type Capabilities struct {
    Layout     bool // can arrange panels from a PanelSpec list
    Persistent bool // an instance survives its client exiting

    // OSWindows: every instance is an OS window, titled as a WindowController
    // reports it. The core then raises a runtime instance through the window
    // host and judges its toggle-back by OS focus. A multiplexer cannot claim it.
    OSWindows bool
}

// WindowController hosts foreign OS windows. It adds nothing to Host; the
// named type exists so wiring states which role an adapter fills.
type WindowController interface {
    Host
}

// WindowWatcher is an optional capability, detected by type assertion. A host
// that implements it enables claim-on-appear. One that does not still supports
// declared targets and explicit attach.
type WindowWatcher interface {
    Watch(ctx context.Context) (<-chan WindowEvent, error)
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
```

## Panels and agent state

```go
type PanelID string

type PanelSpec struct {
    Kind    PanelKind
    Title   string
    Command []string
}

type PanelKind string

const (
    PanelAgent PanelKind = "agent"
    PanelShell PanelKind = "shell"
    PanelTool  PanelKind = "tool"
)

// Panel is one live pane inside a runtime instance. A probe reads this and
// nothing else.
type Panel struct {
    ID      PanelID
    Kind    PanelKind
    Title   string            // live OSC title, as the process last set it
    Vars    map[string]string // runtime-provided: kitty user vars, tmux options
    PID     int
    Command []string          // foreground argv, when the runtime can see it
}

// AgentProbe derives agent state from a panel.
type AgentProbe interface {
    Name() string

    // Match reports whether this probe understands the panel. The first
    // matching probe wins, in configured order.
    Match(p Panel) bool

    Inspect(ctx context.Context, p Panel) (AgentState, error)
}

type AgentState struct {
    Harness  string    // "claude", "opencode"
    Status   Status
    Activity string    // one-line live summary the agent set itself
    Since    time.Time // when Status last changed
}

type Status uint8

const (
    StatusUnknown   Status = iota
    StatusIdle             // agent present, at rest
    StatusRunning          // working
    StatusAttention        // waiting for the human
)
```

`Inspect` receives the whole `Panel` rather than a narrow signal, because which
signal is trustworthy differs per harness and is not obvious.

The Claude adapter is the worked example. Claude Code sets a live window title
carrying a state glyph, and it also sets a user variable from its hooks. The
title glyph is a *level* — it describes the state right now. The hook variable
records *edges*, so it reports `busy` indefinitely after a turn interrupted with
Esc. The probe reads the glyph and treats the hook variable as advisory. A
narrower interface would have forced the wrong signal.

`StatusAttention` is the state the whole product exists to surface. It drives
the TUI sort order.

## Core view types

What `internal/core` produces, and what both the TUI and `--json` render.

```go
type ProjectView struct {
    Project Project
    Running bool
    Home    TargetRef // zero when not running
    Targets []TargetView
    Agents  []AgentView
}

// TargetView is one target and the instance backing it, if any. Host names
// which realization won. Attached is true for an instance bound at runtime
// rather than declared in config, which has no Name and no Key.
type TargetView struct {
    Name     TargetName
    Key      string
    Host     string
    Ref      TargetRef // zero when not running
    Attached bool
}

type AgentView struct {
    Panel PanelID
    State AgentState
}
```

`ProjectView` is the only type the two renderers share. Adding a field serves
both surfaces at once.
