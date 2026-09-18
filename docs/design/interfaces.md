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
    Remote  *Link  // set for a link: a project on another machine (D41)
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
    // Name is the identity a host gives a new instance: a tmux session name, a
    // kitty OS window title, the --class a browser is launched with. It is how
    // a host that assigns its own identity satisfies the Open invariant.
    Name string

    Launch []string   // argv, used when Match finds nothing; a runtime realization with Panels may omit it
    Dir    string     // working directory for Launch and every panel; the project path when empty
    Match  Match      // how to recognise an existing instance
    Panels []PanelSpec // runtime hosts only: the layout for a Home target

    // Inside names another target; this one is then a tab of that target's
    // instance, found by its name, and Match and Name are not used (D64).
    Inside TargetName
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

// Instance is one live thing a host holds: a kitty OS window, a tmux session, a
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
    ID    string // host-scoped: kitty socket/OS window id, tmux server pid/session id, WM window id
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

// WindowPlacer is an optional capability. A host that implements it positions
// a window revier launched (D24); geometry is four tokens in the host's own
// vocabulary. One that does not ignores every declared placement.
type WindowPlacer interface {
    Place(ctx context.Context, ref TargetRef, geometry []string) error
}

// WorkareaReader is an optional capability. A host that implements it reports
// the usable width of the primary monitor, from which the popup's size is
// decided before its launch (D76).
type WorkareaReader interface {
    WorkareaWidth(ctx context.Context) (int, error)
}
```

## PanelOpener

```go
// PanelOpener is an optional capability of a Runtime, detected by type
// assertion. A runtime that implements it can open a tab in an instance and
// focus one panel of it, which is how a target with Inside is reached (D64)
// and how an agent is opened beside a workspace (D65). OpenTab opens the tab
// last, running r.Launch or holding r.Panels with every later panel split
// into the first, each panel in its own Dir. An OpenTab that fails closes
// what it opened. Which tab belongs to which target is the core's: the
// runtime sets vars on the first panel and reports them back in Panel.Vars.
type PanelOpener interface {
    OpenTab(ctx context.Context, ref TargetRef, r Realization, vars map[string]string) (PanelID, error)
    FocusPanel(ctx context.Context, ref TargetRef, panel PanelID) error
    FocusedPanel(ctx context.Context, ref TargetRef) (PanelID, error)
}

// PanelFinder is an optional capability of a Runtime. FindPanel reports which
// of the instances the runtime just listed holds a panel id as the calling
// process names it, a zero ref for none: a kitty window id is one kitty
// process's, and the kitty a command was started from is the one it means
// (D65). It reads the listing it is handed, so a key press lists once.
type PanelFinder interface {
    FindPanel(instances []Instance, panel PanelID) (TargetRef, error)
}
```

## Closer

```go
// Closer is an optional capability of a Host, detected by type assertion.
// Close closes an instance the polite way the tool offers, which is how
// `revier shutdown` ends a workspace or a window (D78). An application may
// keep its window open; the next listing says whether it went.
type Closer interface {
    Close(ctx context.Context, ref TargetRef) error
}

// PanelCloser is an optional capability of a Runtime. ClosePanel closes one
// panel and leaves the rest of the instance, which is how a shutdown of the
// agents alone ends an agent without its workspace (D78).
type PanelCloser interface {
    ClosePanel(ctx context.Context, ref TargetRef, panel PanelID) error
}

// Hider is an optional capability of a WindowController. Hide takes a window
// off the screen without closing it, and Focus brings it back as it was: how
// the popup leaves on Esc and is raised by the next press (D86). Where the
// host cannot hide, the popup exits.
type Hider interface {
    Hide(ctx context.Context, ref TargetRef) error
}
```

## Attacher

```go
// Attacher is an optional capability of a Runtime, detected by type
// assertion. AttachCommand returns the argv that puts the calling terminal
// onto the instance, for the caller to exec: `revier open --attach` ends in
// it, and the ssh pane of a remote project runs that on the host (D40). A
// runtime whose instances are OS windows has nothing to attach to.
type Attacher interface {
    AttachCommand(ref TargetRef) ([]string, error)
}
```

## Remote

A revier on another machine (D40). It is asked what it knows, and nothing
else: it lists no instances, opens nothing and focuses nothing here. The
panels that show what runs there are this machine's runtime's (D84).

```go
type Remote interface {
    // Name is the host as the project file names it.
    Name() string

    // Survey reports the named projects as the remote revier sees them, in
    // one call. A name the remote does not know is an error.
    Survey(ctx context.Context, names []ProjectName) ([]ProjectView, error)

    // Conversations is Survey with each agent's Conversation filled in, for
    // a save. Naming them costs the remote a process a survey does not pay.
    Conversations(ctx context.Context, names []ProjectName) ([]ProjectView, error)

    // RunCommand is the argv that runs `revier run <action> -p <project>`
    // there, for the caller to run here with the terminal. The action is
    // the remote's configuration's to define.
    RunCommand(project ProjectName, action string) []string

    // PanelCommand is the argv a panel here runs to show the project's agent
    // or shell: `revier <kind> exec -p <project>` there. Arguments appended
    // to it reach that command as they are (D84).
    PanelCommand(project ProjectName, kind PanelKind) []string
}
```

## Panels and agent state

```go
type PanelID string

type PanelSpec struct {
    Kind    PanelKind
    Title   string
    Command []string
    Dir     string // the realization's Dir, filled at render; a restore and `revier agent new` set another (D62)
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

The Claude adapter is the worked example. Its state does not come from the panel
at all: Claude Code reports each session's status in `claude agents --json`, and
the probe uses the panel's `PID` to find the session and its `Title` only for
the activity line (D57). It first read a state glyph from `Title` and a hook
variable from `Vars`. The signal changed and the port did not, which a narrower
interface would not have allowed.

`StatusAttention` is the state the whole product exists to surface. It drives
the TUI sort order.

## Titled

An optional capability of an `AgentProbe`, detected by type assertion. A
link's agent has no process here, so no probe can inspect its panel; its title
crosses the ssh, and a probe that reads titles gives the activity line from
that alone (D84).

```go
type Titled interface {
    Activity(title string) string
}
```

## Resumable

An optional capability of an `AgentProbe`, detected by type assertion. A probe
that implements it lets a restored workspace start its agent on the
conversation it held rather than empty (D62). A probe that does not still
restores the workspace, with an empty agent.

```go
// SessionID is a harness's own name for one conversation, opaque to revier.
type SessionID string

// Conversation is a panel's conversation, and the directory the agent works
// in. Either is empty when the harness does not say.
type Conversation struct {
    ID  SessionID
    Dir string
}

type Resumable interface {
    // Sessions names the conversation each panel holds, in the panels'
    // order, with a zero Conversation for a panel that holds none.
    Sessions(ctx context.Context, panels []Panel) ([]Conversation, error)

    // ResumeCommand returns the argv that starts the harness on that
    // conversation, built from the panel as it is configured now.
    ResumeCommand(spec PanelSpec, id SessionID) []string
}
```

`ResumeCommand` receives the `PanelSpec` as the project declares it now, not an
argv the recording stored. A stored argv would freeze the configuration: a
project file that gained a model flag after the save would lose it on restore.
The harness's own resume flag never reaches the core.

`Sessions` takes every panel of a save at once because the answer may cost a
process: the Claude probe runs `claude agents --json` (D55). Twenty agents cost
that once, the rule `Instances` follows.

The directory is the agent's, not the pane's: an agent can work in a worktree
of the project it was opened in. A restore starts the agent there, through
`PanelSpec.Dir`, which is not read from a project file (D62). A conversation is
resumed only into a panel that a probe of the same harness claims, or that no
probe claims, so one harness's id never reaches another's command. An agent past the
declared layout gets a tab once the instance is open, through `PanelOpener` (D65).

There is no `Runtime` counterpart. Persistence across a reboot is either the
runtime's already or impossible for it, and in both cases revier adds nothing.

## Core view types

What `internal/core` produces, and what both the TUI and `--json` render.

```go
type ProjectView struct {
    Project     Project
    Running     bool
    PathExists  bool      // the checkout is on the machine that answered: this one, or the host (D40)
    Unreachable string    // why a remote project's host gave no answer; empty when it did (D40)
    Home        TargetRef // zero when not running
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
    Ref   TargetRef // the instance holding the panel; a panel id is one process's (D75)
    State AgentState
}
```

`ProjectView` is the only type the two renderers share. Adding a field serves
both surfaces at once.
