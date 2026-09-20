# Decisions

Decisions in force, each with the reason that decides it. A missing number was
replaced; its text is in git history. How an entry is written is
[../DOCUMENTING.md](../DOCUMENTING.md)'s.

### D1 — revier is a new project, not an extraction

The `os` command in the `setup` repository stays. revier reimplements what it
needs and ports nothing.

### D2 — the product is agent monitoring, not session management

The value is seeing every agent and reaching the one that needs you. Windows
and sessions are the substrate. This is what separates revier from `sesh` and
`tmuxinator`.

### D3 — Go

One static binary with a fast start. The bash-plus-python predecessor paid a
bootstrap cost on every keystroke.

### D4 — GNOME is an adapter, never core

GNOME needs a Shell extension and is the hardest host, not the reference one.
It ships first because it is the daily driver.

### D5 — TOML, and no backward compatibility

The `.session` `KEY=value` format has sharp edges. The existing files are
converted once by a script that is not part of the product.

### D6 — dev containers are out

Container execution is the heaviest and least general transport. SSH was
deferred here and is in since D40.

### D8 — one TUI, not a picker and a dashboard

The picker and the monitor are one surface: projects sorted by who needs you,
Enter opens.

### D12 — two tiers of binding

A key is a promise about where you land, so only a declared target holds one.
A window attached at runtime is reached through the surface.

### D13 — browser tabs are out of scope

A tab cannot be enumerated or activated from outside the browser. A bound page
is an app-mode window.

### D14 — targets are logical, realizations are per host

Replaces the earlier model of three ports and a project as a set of OS windows.
A binding is to a name: "diff viewer" is a Meld window on a desktop and a pane
in tmux. So there are two ports, `Host` and `AgentProbe`, and matching,
templates and toggle-back are the core's, so two adapters cannot disagree.

### D15 — `Open` produces what `Match` finds, and the core focuses after it

Without the first, run-or-raise opens a new instance on every press; a
multiplexer needs `Realization.Name` for it. The second holds because a window
manager need not focus what it launched.

### D16 — only the focus authority judges toggle-back; every instance is probed

Instance ids are host-scoped, so a runtime pane id compared to a window id is a
coincidence. An agent in a non-home target was invisible when only home was
probed.

### D17 — projects are prepared at load

Templates are rendered and matches compiled once, when the configuration is
read, so no refresh re-derives them and no adapter ever sees a `{{ }}`. What a
failure there costs is D85's. The TUI reads a file changed elsewhere on
restart.

### D18 — a realization starts in the project directory

A workspace that opens in kitty's own directory is the one thing a session file
never got wrong.

### D19 — a runtime that owns OS windows says so, and the window host raises them

On GNOME Wayland, kitty cannot raise its own window. With `OSWindows`, the core
finds the OS window by title and raises and judges focus there. PID lineage was
rejected: one kitty process owns many windows.

### D21 — a key binds to the instance it landed on, and a launch waits for its window

Replaces the claim-on-appear rule for targets. Titles move after launch, so a
rule matched on every press is weakest right after the launch. The new window is
bound by class. Claim-on-appear remains for actions only, in the TUI, within
seconds, and never for a window that is ambiguous or that a target matches.

### D22 — a window with no name of its own is identified by the window manager

kitty windows opened from a session file carry no name. The core pairs such a
window with the one window of the same process on the window host.

### D23 — the list component ranks and filters; the scrolling is ours

`bubbles/list` paginates, and a held arrow key flickers through pages. It
renders one page, and a viewport scrolls it.

### D24 — the compositor places what your script starts; revier places what revier starts

Replaces "placement is the compositor's". A compositor rule cannot name the
window a launch just produced. `place` applies once after a launch, never on a
raise, and a refusal is not an error.

### D25 — a binding is re-checked against the class before it is trusted

Window ids are reused. The title is never re-checked, because surviving a title
change is why a binding exists.

### D26 — the desktop's keyboard shortcuts are a port of their own

A shortcut store is not a `Host`, and on GNOME it is not the same tool. The
reader cannot write.

### D27 — revier adds keys, and takes one only with `--force`

Includes the corrections from the first desktop runs. `--force` takes the one
key revier needs, and leaves the other keys of a desktop default. revier keeps
no backup of what it displaced. The way back to the shell tool is both
`revier keys uninstall` and `os init`, because `os init` alone copies its
shortcuts beside revier's.

### D30 — revier writes project files, and clones a missing checkout

A project file travels between machines and its checkout does not. The clone
is the only write outside revier's own files, and never happens in a survey.

### D31 — a runtime can type into a panel; whether it may is the core's

`PanelWriter` is optional. The core refuses a panel with no agent, a shell in
the foreground, an attention or unknown state, or text of more than one line.
Each would type into the wrong thing.

### D32 — a command in every project is revier's

Replaces "bulk execution is a separate tool". Nobody built it, and the project
list is revier's. Projects run one at a time, because the chores write shared
state.

### D33 — the TUI takes the mouse

A popup that ignores the wheel reads as dead. Shift-drag still selects text.

### D35 — a key the terminal cannot deliver works on the desktop only

Without the kitty keyboard protocol, Ctrl+Shift+U arrives as Ctrl+U. A target
key yields to any meaning that press already has in the TUI.

### D36 — a click selects a row, and a second click opens it

Selecting a project costs nothing; opening one opens a window.

### D39 — the list is a table as wide as its content, and the pane takes the rest

Replaces a capped, centred frame and a full-width grid. Past its content width
the list was blank. The agent column gives way in steps before a name is cut.

### D40 — a remote project is surveyed and driven by the revier on its host

Containers stay out. An ssh-wrapping runtime would reimplement every adapter;
asking the remote revier for `list --json` costs one port, `Remote`. The
workspace that shows it is local, so focus and bindings see an ordinary
instance (D84).

### D41 — a link is its own kind of file

A file on the host defines a project; a file here refers to one. One shape for
both cost two workarounds.

### D44 — clearing the query puts the cursor back where it was

A query reads as a temporary view, and clearing it as its undo.

### D45 — a link is made from the host's own list

The ssh config and the remote revier know the hosts and projects. A failure is
said in revier's words with the next step, and ssh's errors are told apart by
exit status 255.

### D46 — the set of open projects is revier's; the contents of a pane are not

Which projects and targets were open is revier's model, and a reboot loses it.
Restore is run-or-raise over names, so a re-run is free.

### D48 — a tmux pane's variables arrive in one option revier owns

tmux cannot enumerate options in a format. `@revier` holds `NAME=value` pairs,
and the host does not know which ones a probe reads.

### D49 — actions about the installation stand on the top line, each with a key

A bare letter filters, so these actions had no key anyone could find. A button
is always a second way, never the only one.

### D50 — the pointer lights what it is over, a step below the selection

Hover says what a click would do, and it never selects.

### D51 — no header and no border; the rule carries the count

The header named the program just started, and the border repeated the
terminal's edges. What else the rule carries is D96's.

### D53 — config is a screen, and a change applies as it is made

`$EDITOR` opened an empty file with no list of settings. The file is edited
line by line, so comments survive. A runtime is probed before it is written.

### D54 — alt+h lists every key, read from where each key is declared

The help screen cannot name a key that a press does not match.

### D55 — a conversation id comes from `claude agents --json`

Replaces a `SessionStart` hook. Nothing to install, agents already running are
named, and a pid match tells two agents in one repository apart.

### D56 — actions are edited on the config screen

A command is one line split as a shell splits it. An edit is refused if the file
changed under the screen.

### D57 — a Claude agent's state comes from `claude agents --json`, not from its title

The title and `CS_STATE` do not always reach revier, and an agent at a
permission prompt then shows idle. The listing runs only when
`~/.claude/sessions/*.json` modification times change, and at least every ten
seconds. An unrecognised status is unknown, not idle.

### D58 — the link dialog names every link

The project's own name collides on machines that share projects. The dialog
offers `rs-<host>-<project>`.

### D59 — targets most projects share are declared once, in config.toml

Eighty-nine files repeated three targets. Tables merge key by key; any other
value, a list included, replaces the shared one, because a panel has no identity
to merge on.

### D60 — shared targets are edited on the config screen

A write is refused unless every project still loads, because a shared target is
part of every project.

### D61 — an action on one of revier's own keys is refused at load

The surface matches its own keys first, so such an action never ran and nothing
said so.

### D62 — a restore brings back every agent, in order, in the directory it worked in

Replaces restoring an agent by its declared panel position. Most agents are
opened by hand in tabs, and the position also counted other tools' tabs. The
resume is the probe's (`Resumable`), built from the project as it reads at
restore. An agent resumed from the repository root instead of its worktree edits
`main`, so the directory is recorded too.

### D63 — a named window does not hide its unnamed sibling, and a window that cannot be raised is not focused

Pairing by elimination keeps D22's refusal for real ambiguity. A focus with no
raise becomes a GNOME "is ready" notice, so the press fails with
`ErrUnraisable`.

### D64 — a target can be a tab inside another target

`inside` places the ticket viewer on a workspace tab. The tab's identity is a
variable revier sets, because a title or a position is not one. `PanelOpener` is
optional, and a runtime without it refuses; it does not open a window the user
did not choose.

### D65 — an agent opened beside a workspace is a tab, opened by one function for a key and for a restore

`revier agent new` and a restore open the same tab: a copy of the first
declared agent panel and a split of the first declared shell panel. One builder
keeps the key and the restore from drifting apart. `OpenTab` takes a panel
group, so no second capability exists. The key finds its workspace through the
runtime's optional `PanelFinder`, because a kitty window id is unique only in
one kitty process.

### D66 — a minimized window is a window revier can raise

`wctl activate` restores a minimized window and switches to another workspace.
Dropping hidden windows made an editor key launch a second copy. Only a window
mutter has not shown yet is dropped.

### D67 — every named window of the process must be seen before its unnamed sibling is paired

Narrows D63. A named window the host does not list may be the one left over, and
the unnamed window would take its title.

### D68 — a tab target's agent is recorded under the tab, and not resumed

Recorded under the workspace, it was restored twice. A tab target's argv is not
known to be the agent, so a resume could run `taskmgr-ui --resume <id>`.

### D69 — the log is slog's default logger, one JSON file per day under the state root

The standard library carries levels, fields and JSON, so no logging dependency
is added. Every process appends to one daily file, because a keypress is its own
process; `O_APPEND` keeps concurrent lines whole, which a size-rotating library
does not promise. Logging is not a port: it names no tool, so the core logs
directly. A poll that recurs logs a failure once until it changes or recovers,
and a slow run once a minute, so a host down for an hour does not bury the
operations.

### D70 — a tmux instance is a session, and its windows are tabs

One session held every workspace as a window, so a window was both an instance
and the only thing a tab could be, and two terminals attached to two workspaces
shared the session's current window and switched each other. A session per
instance gives tmux the shape of a kitty OS window: `OpenTab` adds a window,
and `attach-session` reaches one workspace. The name lives in `@revier-name`,
because tmux 3.4 rewrites `:` and `.` in a session name. tmux cannot say which
of several terminals is meant, so `Focus` switches only the terminal of the
pane it runs in, or the one terminal attached, and records the focus in
`@revier-focus` for `Focused` to report when neither decides.

### D72 — a drag selects a box of the screen and copies it; revier draws it

The terminal selects only with shift held, tells revier nothing, and clears the
selection when a refresh rewrites a line under it; the surface must keep
refreshing. So revier draws the box over the screen as the drag found it, and
copies with OSC 52. A button or target runs on release, so a drag from one runs
nothing.

### D73 — projects, targets and agents are sections, each with its own query

Replaces Tab moving into a pane of targets, and one query line that followed
the cursor. A query far from its rows did not say what it filtered, and the
agents could be read but not reached. Tab walks projects, targets and agents,
and shift+tab back, passing over a section with no rows. Each query sits over
its own rows and is kept while its project is, until a press opens something:
the query was the way there, so a popup raised again held the last search
instead of a new one. Then every query ends, each cursor stays on the row it
found, and a launch that fails later says so in the footer. The pane starts level with the
project query, so that query stands in the list's column and not over both. Enter opens a project, runs a
target, and goes to an agent. A pane with no target level stays: Enter on a
project opens its home.

### D74 — an agent is reached by making its tab current

`Focus` on the instance and `FocusPanel` already reach a tab target, so going
to an agent is those two and a raise, and needs no new runtime capability. The
instance comes first: on tmux its focus is what switches a terminal showing
another session.

### D75 — an agent is named by its instance and its panel

A panel id is unique only within the process that numbers it, and two kitty
processes of one project can each hold a panel 1, so an agent found again by
its id alone was ambiguous. The survey already reads each agent from its
instance, so `AgentView` carries that ref, and going to an agent uses the pair
rather than a second lookup. A link sends the host's ref back to the host,
which is the only side it means anything to.

### D76 — the popup and the fallback to it are revier's, in kitty on GNOME

Replaces the two shipped scripts, `revier-popup` and `revier-go`. revier
shipped them, bound keys to them, and changed them, so they were product code
in shell with no test. `revier popup` run-or-raises a kitty window of class
`revier-popup` and places it under D24: centred at the workarea's full height
and a fixed 1800 px width, or filling it when the workarea is narrower, decided
before the launch so the window never resizes. The width is fixed rather than a
share of the workarea, so that on a wide screen everything stays in one spot.
`go --picker` falls back to it, and plain `go` keeps its exit status for
scripts. sway is removed: the popup and the keys are GNOME's.

### D77 — a shortcut in an entry revier names is revier's, whatever it runs

Ownership was read from the command alone. The keys an older revier wrote ran
commands that are gone, so an install read them as somebody else's: `--force`
switched each off and wrote a second entry beside it, and uninstall never
removed them. An entry named `revier-<target>` is revier's, so install rewrites
it in place and uninstall deletes it. A shortcut elsewhere that runs exactly
revier's command stays revier's too.

### D78 — a shutdown saves the session first, and refuses a busy agent unless forced

`revier shutdown` and the TUI's shutdown wizard close every project, or one
project whole, or only its agents, or only what holds no agent. A shutdown is
the moment before a restart, so it saves the session when it differs from the
newest one; a second shutdown of the same desktop adds no file. An agent that
works or waits for an answer would lose its turn, so the CLI refuses before
the save and `--force` goes on; the TUI's confirm names the busy agents
instead, and confirming them is the force. A confirm is a decision taken on a
survey that has aged, so it reads its own steps' agents again before it saves
or closes anything. `Closer` and `PanelCloser` are optional and polite: a
window an application keeps open is named as still open, never killed. A
project shutdown leaves an instance another project also holds.

### D79 — a project row counts its agents by state, and says nothing else on the right

The row showed the worst state, its activity cut to fit, and under it the open
targets or a missing checkout. In a narrow popup the cut activity read as
noise, and a project with several agents showed only one. The right column now
holds one count per state, closest to needing you first. The activity, the
open targets and a missing checkout are the pane's; the folder glyph still
marks a missing checkout on the row.

### D80 — the file name is the project's name, and a rename moves the file

A `name` key could disagree with the file name, and no file ever used it that
way; it only doubled every rename. A file that still has one is refused at load
rather than read either way. A rename moves the file, writes a link's name on
the host when the link derived it, and moves the state entries and the saved
sessions. It is refused while the project runs, because templates and the link
pane match the name, and refused when a realization writes the name out beside
`{{.Name}}` - which `revier new` does for a name a regexp would read - since
that pattern would go on looking for the old name while the name followed.

### D81 — a project is edited on its own screen, and never in `$EDITOR`

Replaces opening the project file in `$EDITOR`, for the reason D53 gave for
`config.toml`. The screen shows a shared target merged, marks each value that is
`config.toml`'s, and writes only the values that differ from it, so
`config.toml` is never written from a project. A shared value cannot be emptied
from a project, because an absent key is the shared value.

### D82 — a target carries a realization per kind of project, and a link gets the remote one

Extends D59, which left a link outside the shared targets. The editor of a
project on another machine is VS Code over ssh where the local one is IntelliJ
on a path here, so the two cannot be one realization; two targets instead
would split the name and the key that must stay one. A target holds
`[target.window]` and `[target.runtime]` for a local project and
`[target.remote.*]` for a link, and a shared target with no remote part is
local-only. A project file writes the part of its own kind, and the other
part - a realization nothing would read - is refused at load. The derived ssh
pane fills what a link's home target leaves out, so a placement costs no
repetition of the launch.

### D83 — a link records what the host says about the project, and an argument that renders to nothing is refused

Replaces "a link has no git_url". A link's targets run here and reach the
project there, so an editor over ssh renders a path on the host and a page
renders the repository it is a clone of; neither names anything on this
machine, and only the host knows them. `revier link` asks the host anyway, so
it writes both into the file, and a checkout here is still refused. An argv
element that renders empty refuses its target, whatever left it empty: an
argument is read by position, so none of them is optional, and an empty one
reaches the program as the current directory or a missing operand and says so
nowhere. What that refusal costs is D85's.

### D84 — a link's workspace is panels here, each an ssh that becomes the host's panel

Replaces D71, the link half of D74, and the one pane attached to a tmux
workspace on the host. Tabs, scrollback, keys and focus in that pane were
tmux's inside the terminal's, and every act on an agent was a round trip to
the host. Each panel here now runs `revier agent exec` or `revier shell exec`
there, so the runtime here opens, focuses, types into and closes it, and
`Remote` keeps the questions only. An agent does not outlive its terminal;
restore resumes it, as it does after a reboot here. The two machines name one
agent by a tag, this machine's name and the pid of the panel's ssh, which the
process carries in its environment: nothing is recorded, and a title, `/clear`
or a resume cannot move it. The host lists such processes from `/proc` as a
host of its own beside its runtime, because no runtime there holds them. What
an agent starts inherits the tag, so the panel is the process nearest the one
started. sshd runs a command in a shell that read no profile, where `claude`
was not on the PATH and every agent read unknown, so each command runs in the
login shell.

### D85 — a refusal disables the smallest thing that is wrong

Replaces the refusal scope of D17 and D83. One project file whose shared web
target rendered `{{.GitURL}}` to nothing refused all ninety, so the surface,
all four desktop chords and `revier popup` were gone until that file was
fixed: the tool that reaches a broken project is the one thing that must not
break with it. A rule now refuses the target it names; a rule about the
project as a whole refuses the project; neither refuses the set. Each is
loaded and listed carrying its reason - `available: false` with a `Reason`,
or `Invalid` on the project - because a file that disappears from the surface
takes with it the one place its reason could be read. Its key still fails
loudly when pressed, since nothing may run from an argv that did not render.
A key nothing reads is not a refusal at all. `revier doctor` is where the
reasons are read in full. An edit revier itself makes is refused for what it
breaks and not for what was broken before it, so a file is repaired one target
at a time; what `revier new` and `revier link` write must come out whole.

### D86 — Esc hides the popup, and the next press raises it

Esc exited the surface, so every press was a cold start: a kitty process, the
TUI, and a first survey that is a round trip to every linked host - a second
from the key to the agent states. The popup now minimizes on Esc through the
window host, and `revier popup` raises it under D76's run-or-raise as it did an
open one, in tens of milliseconds, with the cursor and the query where they
were. Hidden, it surveys nothing, since nobody reads the answer and every survey
reaches every linked host; the terminal's focus report on the raise reads the
project files again, since a press used to load them, then runs one survey
and resumes the refresh. The surface knows it is the popup, and which project
to open on, by the environment its terminal is started with, read and dropped
at start so nothing it launches inherits it; the project is resolved at the
keypress, while the focused window is still the user's. In any other terminal,
and where the host cannot hide, Esc exits as before.

### D87 — claim-on-appear polls; there is no window-event port

`WindowWatcher` was a port no host implemented: `wctl` reports no events, and
the TUI carried a second claim path that only a test fake ever ran. The port
and the event path are removed, and the TUI's survey diff is the one claim
path. The port comes back with the first host that has events, when one is
built; its design is in git history.

### D88 — a link's panel argv is the remote port's

`internal/config` derived a link's home panels by calling the ssh adapter for
their argv, the one store-to-adapter import, and the core appended resume
arguments to that argv on an unstated assumption about its shell form. The
argv is `Remote.PanelCommand`, and the core asks the port for it when the
target resolves: config derives the panel kinds alone, one seam holds the
contract, and a second transport plugs in without touching config.

### D89 — a host that cannot list costs its own targets and no other's

One host's `Instances` failure - a `wctl` timeout - failed the whole survey
and every `go`, so the runtime view and the keys were gone exactly when the
desktop was the thing misbehaving. A survey now stands over the hosts that
answered: the failed host's targets show as unknown with its reason, its
refs are not taken as gone, and a press on one of them is refused with that
reason. The other hosts' targets go as before.

### D90 — opening a project is one decision, made in the core

The command line and the surface each decided what opening a project means,
and disagreed: on a missing checkout with nothing to clone from, `open`
refused while Enter raised a running workspace; on no home target, `open`
refused with a reason while Enter moved the cursor and said nothing.
`core.Open` decides once - refuse with the reason, clone, or go - and both
render it. A running workspace is raised, since raising touches no directory;
a fresh start into a missing directory is refused; a project with no home
shows the reason, and the surface also moves to the targets it has.

### D91 — one ledger, the state file, and every activation goes through it

The launch rule of D21 was written four times: `ActivateWaiting`, a ledger
in the command line over its startup state, a ledger in the surface for a
restore, and the surface's own message chain for a press. A rule copied that
often breaks silently when one copy is missed. `core.StateLedger` is the one
ledger, the state file read and written under its lock at every step, and
the command line, the surface and a restore all activate through
`ActivateWaiting` with it. What a survey settles in state - prune, claim,
expire - is `core.Settle`, the one function the surface and `revier list`
call.

### D92 — the surface leaves its start directory; a host sets no directory for it

The TUI is often started in a worktree and outlives it. Once the worktree is
removed, every process it starts inherits a directory that is gone, and git,
claude and a probe script each refuse to run. Giving each adapter a directory
of its own fixed only the adapters that had one, and every new adapter had to
copy it. The TUI reads its start project and then moves to the home
directory, or to "/" without one, so no child can inherit a directory that
vanishes.

### D93 — del closes, alt+del deletes, and a delete closes first

One key closes whatever row the cursor is on, and a modifier on it deletes
what the configuration holds of that row. A delete used to be refused while
anything ran. It now closes first, through the same plan as a shutdown,
because a refusal only sends the user to close by hand. A target of
`config.toml` is every project's, so no project's row deletes it. Unlinking
touches no file on the host. ctrl+del is not the key because bubbletea v1
does not read its sequence.

### D94 — a tab closes as every panel in it, and its busy check covers them all

A tab opened with a panel group holds panels that carry no target mark, so
closing the one panel the target is found by left the others on screen while
the shutdown said "closed". `Panel` now names the tab that holds it, and a
close step carries every panel of that tab and closes each through
`PanelCloser`: kitty ends a tab with its last window and tmux a window with its
last pane, so no close-a-tab capability is needed and tmux never kills a window
linked into other sessions. The step holds the agents of all those panels, so a
busy one beside the marked panel refuses it, and it counts as closed only once
no panel of it is listed. An agent's own step closes its tab the same way,
because the tab `revier agent new` opens carries no target mark to be closed
by; the workspace's own tab is the exception, where the shell a layout declares
beside the agent stays.

### D95 — an attachment is the window and the terminal inside it

The focused instance of D12 is a window, and a window carries no panels, so a
survey of a hand-attached terminal probed nothing and a shutdown ended the
agent in it unasked. `revier attach` and a claim now record the terminal
beside the window, when the window pairs with one beyond doubt - the pairing
D63 and D67 already refuse to guess at. An ambiguous window is recorded alone
and reports no agent, which is the honest answer. The two are one attached row,
the terminal's, because that is the side that holds the panels.

### D96 — the rule totals the agents on the list, in the rows' own column

Replaces D51's removal of the counts beside the rule's count. The rows show
each project's state, but ninety of them do not fit on a screen, so "how many
need me now" was a scroll. The totals stand in the column the rows count in,
one layout placing both, so a state's total sits over that state's counts.
They count the rows the filter left, and the rule's line runs through the slot
of a state with no agent.

### D97 — the surface opens with the rows above the starting project in view

The list is sorted with the projects that need the user first, so opening on
a project resolved from the working directory or the focused window put it
alone on the last line and said nothing about what else was waiting. It keeps
ten rows above it, fewer on a screen with no room, where it is the last row in
view: a row the user was taken to and cannot see is worse than no context.

### D98 — a deleted row hands the cursor to the row that took its place

Deleting the project under the cursor left it at the top of the list, from
restoring a selection by a name the list no longer holds. The cursor keeps its
position instead, or the last row when the deleted one was last. A close moves
no row away, so the cursor keeps following its project there: the cursor
follows what it was on while that is on the list, and the place otherwise.
