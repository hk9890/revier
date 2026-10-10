# Decisions

Decisions in force, each with the reason that decides it. A missing number was
replaced or withdrawn; its text is in git history. How an entry is written is
[../DOCUMENTING.md](../DOCUMENTING.md)'s.

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
in tmux. Matching, templates and toggle-back are the core's, so two adapters
cannot disagree.

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

### D23 — the list component filters; the scrolling is ours

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
no backup of what it displaced; putting a displaced shortcut back is the job
of the tool that wrote it.

### D30 — revier writes project files, and clones a missing checkout

A project file travels between machines and its checkout does not. The clone
is the only write outside revier's own files, and never happens in a survey.

### D31 — a runtime can type into a panel; whether it may is the core's

`PanelWriter` is optional. The core refuses a panel with no agent and a shell
in the foreground, and for a prompt an attention or unknown state and text of
more than one line. Each would type into the wrong thing. A named key is
D117's.

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
directly. A poll that recurs logs each cause of a failure once until it
recovers, and a slow run once a minute, so a host down for an hour does not
bury the operations, whether it fails one way or three in turn.

### D70 — a tmux instance is a session, and its windows are tabs

One session held every workspace as a window, so a window was both an instance
and the only thing a tab could be, and two terminals attached to two workspaces
shared the session's current window and switched each other. A session per
instance gives tmux the shape of a kitty OS window: `OpenTab` adds a window,
and `attach-session` reaches one workspace.

### D72 — a drag selects a box of the screen and copies it; revier draws it

The terminal selects only with shift held, tells revier nothing, and clears the
selection when a refresh rewrites a line under it; the surface must keep
refreshing. So revier draws the box over the screen as the drag found it, and
copies with OSC 52. A button or target runs on release, so a drag from one runs
nothing.

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
in shell with no test. `revier popup` run-or-raises a kitty window that revier
places before the launch, and `go --picker` falls back to it. sway is removed:
the popup and the keys are GNOME's.

### D77 — a shortcut in an entry revier names is revier's, whatever it runs

Ownership was read from the command alone. The keys an older revier wrote ran
commands that are gone, so an install read them as somebody else's: `--force`
switched each off and wrote a second entry beside it, and uninstall never
removed them. An entry named `revier-<target>` is revier's, so install rewrites
it in place and uninstall deletes it. A shortcut elsewhere that runs exactly
revier's command stays revier's too.

### D78 — a shutdown saves the session first, and refuses a busy agent unless forced

A shutdown is the moment before a restart, so it saves the session when it
differs from the newest one. An agent that works or waits for an answer would
lose its turn, so the CLI refuses before the save and `--force` goes on; the
TUI's confirm names the busy agents instead, and confirming them is the force.
`Closer` and `PanelCloser` are optional and polite, so nothing is killed.

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
would split the name and the key that must stay one. So a target carries a
local part and a remote part, and a link reads the remote one.

### D83 — a link records what the host says about the project, and an argument that renders to nothing is refused

Replaces "a link has no git_url". A link's targets run here and reach the
project there, so an editor over ssh renders a path on the host and a page
renders the repository it is a clone of. Only the host knows them, so `revier
link` writes both into the file, and a checkout here is still refused. An argv
element that renders empty refuses its target: an argument is read by
position, so none is optional, and an empty one reaches the program as the
current directory or a missing operand and says so nowhere.

### D84 — a link's workspace is panels here, each an ssh that becomes the host's panel

Replaces D71, the link half of D74, and the one pane attached to a tmux
workspace on the host. Tabs, scrollback, keys and focus in that pane were
tmux's inside the terminal's, and every act on an agent was a round trip to
the host. Each panel here now runs `revier agent exec` or `revier shell exec`
there, so the runtime here opens, focuses, types into and closes it, and
`Remote` keeps the questions only. The two machines name the agent by a tag
its process carries, so nothing is recorded and no title can move it.

### D85 — a refusal disables the smallest thing that is wrong

Replaces the refusal scope of D17 and D83. One project file whose shared web
target rendered `{{.GitURL}}` to nothing refused all ninety, and the surface,
the desktop chords and `revier popup` went with it: the tool that reaches a
broken project must not break with it. A rule refuses the target it names, a
rule about the project refuses the project, and neither refuses the set. Each
stays listed with its reason, because a file that leaves the surface takes
with it the one place its reason could be read.

### D86 — Esc hides the popup, and the next press raises it

Esc exited the surface, so every press was a cold start: a kitty process, the
TUI, and a first survey that is a round trip to every linked host - a second
from the key to the agent states. The popup now minimizes on Esc through the
window host, and `revier popup` raises it under D76's run-or-raise in tens of
milliseconds, with the cursor and the query where they were. Hidden, it
surveys nothing, since nobody reads the answer.

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

### D92 — a process leaves a start directory that is gone; a host sets no directory for it

The TUI is often started in a worktree and outlives it, and a shell can lose
its directory under a command; every child then inherits a directory that is
gone, and git, claude and a probe script each refuse to run. Giving each
adapter a directory of its own fixed only the adapters that had one, and every
new adapter had to copy it. So each process settles its own directory before
it starts anything, and no host sets one.

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
the shutdown said "closed". A close step now carries every panel of the tab
and closes each through `PanelCloser`; the tab ends with its last panel, so no
close-a-tab capability is needed and tmux never kills a window linked into
other sessions. The step's busy check covers every panel it carries.

### D95 — an attachment is the window and the terminal inside it

The focused instance of D12 is a window, and a window carries no panels, so a
survey of a hand-attached terminal probed nothing and a shutdown ended the
agent in it unasked. `revier attach` and a claim now record the terminal
beside the window when the window pairs with one beyond doubt, and the two
are one row, the terminal's, since that side holds the panels. Rejected:
folding from the terminal's side, where a shared title names two windows.

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

### D99 — the busy guard is in the close path, not in the surface that asks

The guard lived in the wizard's confirm, so `revier shutdown` and the instant
del both closed on a plan drawn before the session was saved and before the
press landed: the race the guard exists to close. `core.Shutdown` now reads
the plan's agents again and refuses the whole plan while one is busy, unless
forced; forcing says not to refuse and nothing else. A step whose agents it
cannot read refuses itself alone, because refusing the plan let one dead
remote host stop eighty-nine projects until `--force` turned the guard off.

### D100 — the workspace's own panel is marked, not guessed at

`ownPanel` took the first panel carrying no tab mark, so in a workspace whose
tab holds a shell beside the tab target's panel it picked that shell: a return
home from a tab landed in the wrong pane, and the own-tab rule of a shutdown
(D94) read the same guess. revier now marks the first panel of every instance it opens with the
target it was opened for, through `Realization.Vars`, as `OpenTab` already
marks a tab's. The guess stays only for an instance opened before the mark.

### D101 — a link's view is the host's agents and this machine's, added together

`local := p.Remote == nil` skipped the probe for a link, and the host's answer
then replaced the view's agents outright, so a terminal attached to a link by
hand reported none and a shutdown ended a busy agent in it unasked. An
attachment is probed for a link too, and the host's agents are added to what
was read here. Where both name one agent, the local reading stands: it read
the panel itself, and the host's word crossed a machine.

### D102 — an agent's state is what it does now, not since when

`AgentState` carried a `Since` for how long the status had held. Nothing ever
rendered it: no row, no pane and no `--json` consumer in this tree reads one,
and the probe contract `extending.md` states never asked a script for it. The
one probe that filled it stopped when its listing turned out to say only what
an agent does now. A field on a port that no surface reads is a field every
out-of-tree probe author fills for nothing, so it is gone; a product that
wants an age can add it back with the reader that needs it.

### D103 — the top line's right edge says which revier is running

A released binary and one built from source draw the same surface, and nothing
on it said which was which. The version stands at the right end of the first
line: that line already carries what is about the installation rather than
about the rows (D49), and its right side is the only place no column claims.
It is not the header D51 removed - that named the program, which the user
knows - and where less than two columns of air are left it is dropped, because
the buttons are what the line is for.

### D104 — a surface shows the agents a panel here shows, and the survey keeps the rest

A link's host lists every agent in its project, including the ones opened on
that machine, and the row counted them all: a project drawn as closed, counting
an agent `ErrAgentElsewhere` refused to reach, and the same on the other side
for an agent this machine serves to a terminal elsewhere. Every surface over
this machine's projects draws `Core.Shown`, the agents a panel of this
machine's runtime shows; the survey keeps the rest, which the other machine's
revier and a shutdown still read.

### D105 — Tab walks the projects and the agents; the targets are one chord away

Replaces D73, where Tab walked the projects, the targets and the agents, each
under a query. The agents are what the surface exists to reach, and a third
stop put them two presses off; a project has a handful of targets, so a query
over them found nothing a glance does not. The targets have no query, and a
pane row takes D36's clicks, so one target can be chosen for del and alt+del.

### D106 — what an agent said last is read from its transcript, for display only

When an agent spoke last orders the agents (D110, D124), and `revier agent
read` prints what it said (D116). The Claude probe reads both from the
session's transcript, whose format is Claude Code's own and changes between
releases, so the read is display only (`Detailed`). Rejected: the agent view's
summary, which Claude Code writes for background sessions alone (3 of 27 when
measured); `claude -p --resume`, a model request per agent; a hook, which
every user would install.

### D108 — an agent's message is drawn as text, never as instructions to the terminal

A message is whatever the agent wrote, and agents quote what tools print: a
captured `git diff --color`, a progress line rewritten with a carriage return,
a title an OSC set. Passed through, each of those repaints the surface that is
showing it. So every escape and control character comes off before the pane
measures or styles a line, and the message is set in revier's own colours.
Rejected: trusting the harness, which does not own what a tool wrote into the
transcript it keeps.

### D109 — the cursor follows an open project, and keeps its place when the project closes

A refresh restores the cursor by name, so a project that closed took the
cursor down among the stopped rows, away from the open ones the user works in.
The place is what the user was at: the cursor stays there, on the row that
moved up, and a project that left the list hands it on the same way. An open
project keeps the cursor through every reordering, attention included, so the
rule has one test: is it still open. Replaces D98.

### D110 — the agents of every project are a second list, behind the key that opens the surface

With thirty agents in ten projects the question is which agent, and the
project list answers it one project at a time. The second list orders every
agent by need: waiting for the user, working, at rest, unknown. The first and
third stand by when each spoke, the latest first, read through `Detailed`; a
working agent speaks all the time, so those stand by project. The surface
always opens on the projects. The desktop takes its key before any terminal
does, so `revier popup` types it into the surface the focused window shows.
Rejected: a mode of the project list, which hides the projects it is for.

### D111 — the agent list's pane mirrors the agent's panel, with its colour and nothing else

No harness renders a running session for another process, and a transcript is
one harness's format (D106). The panel is what every harness draws and what a
link's ssh carries, so the pane reads it from the runtime (`PanelReader`) once
a second, and its scrollback only while the user is scrolled into it. The
screen is laid out for the agent's window, wider than the pane, so a long line
continues on the next. Only SGR sequences pass, and every line is closed: a
screen holds whatever a tool printed (D108). Rejected: cutting a long line,
which loses the end of every sentence.

### D112 — what revier did in a project is an event, kept apart from the log

How a project was used is asked over weeks, and the log is pruned after two
and worded for diagnosis. So a short fixed list of operations, and the
conversation each agent holds, are appended to one file that is never pruned.
`revier events` prints the lines and counts nothing: what "used" means is the
reader's. A linked host is asked for its own lines, one hop. A line names what
happened and where, never what was said. Rejected: agents writing their own
entries, which a forge or the transcript already records better.

### D113 — a query keeps the list's order, and the fuzzy score ranks only within it

The fuzzy scorer alone put a stopped project above an open one, because it
gives a letter after a separator more than a letter beside the last. A query
is a view over the list (D44), so its rows stay in the groups the list sorts
them into. Within a group a row that holds the query whole stands above one
that holds its letters apart, and then the score decides.

### D114 — a surface surveys this machine apart from the linked hosts

One survey that waited for every linked host held each local row for the
slowest of them: a host that did not answer kept a closed project listed as
open for many seconds. So a refresh lists this machine alone, each host is
asked beside it by itself, one round at a time, and its last answer is laid
over each new listing. A command that runs once still takes it all as one.
Rejected: painting a close from its own result, which mends one operation and
leaves every other row waiting on the host.

### D115 — a close asks only the linked hosts it is about

Every close surveyed twice and each survey waited for every linked host, so
a host that was gone cost a close on this machine two waits with nothing of a
link open, and a third in the save when one was. A link with nothing open
here has nothing a close could end. The plan's survey asks the hosts of the
links open here, or the one project's being closed, the recheck the hosts
its plan has a step for, and a save's ask of the hosts has ten seconds.
Rejected: shorter phase budgets, which cut a slow desktop short and still
waited once a phase.

### D116 — a script reads what an agent said last, and its panel on request

A script that prompted an agent saw the turn end and never the answer.
`revier agent read` prints the probe's `Detailed` message, the clean text.
D106 holds: revier prints it and decides nothing by it, and a message that
cannot be read is a refusal that names `--screen`. `--screen` is the panel
through `PanelReader`, which every harness and a link's panel have, as text
alone (D108). Rejected: the screen as the default, which hands the caller a
harness's chrome to parse.

### D117 — a named key is typed into an agent whatever its state

`revier agent send-keys` answers the dialog D31 keeps a prompt out of and
interrupts a working agent, so the states that refuse a prompt cannot refuse a
key. The caller names every key, so nothing is typed that it did not choose,
and reading the panel first is the caller's. The panel is still an agent's
(D31). The keys go one at a time with a pause, because an Esc directly before
another key is alt and that key.

### D118 — a script's agent tab opens without the focus

`revier agent new --no-focus` is for a script whose user types in another
window. The tab opens, nothing is focused or raised, and the address of the
new agent is printed, since the script has nothing else to find it by. It
needs no window that can be raised (D63). So `OpenTab` leaves the keyboard
where it is on every runtime, and going to a tab is `FocusPanel`'s. A kitty
OS window without the keyboard still shows the tab it was given: putting the
old one back asks the desktop for the focus, which is what the flag avoids.

### D119 — a host says shell or tool of a live panel, never agent

tmux named four harnesses and reported their panes as `agent`; kitty reported
the same panes as `tool`, so one pane had two kinds. A host reads a program's
name and nothing of what it does. `revier.IsShell` is the one list of shells,
with the dash of a login shell removed, and every other program is a `tool`.
Whether a tool is an agent is a probe's answer. Rejected: one shared list of
harnesses, a second registry beside the probes that a probe declared in config
is never in.

### D120 — the core holds the one ledger, and no caller hands it state

Replaces D91. The launch rule of D21 was written four times, and twelve core
operations took bindings and attachments from a caller, which the command line
filled from the state it loaded at startup. `Core.Ledger` is the one way state
reaches the core: an operation reads it once at its start, a press is
`ActivateWaiting`, and what a survey settles - prune, claim, expire - is
`Core.Settle`. The ledger is storage, `internal/ledger` over the state file;
the rules stay in the core and `internal/state`. Rejected: a ledger with one
method for each rule, which every fake writes again.

### D121 — a use case across stores is written once, in `internal/app`

The command line and the TUI each wrote the same sequences - create a project,
link one, rename one, run an action - and the copies differed: one stamped an
action's launch and its event with two times. `internal/app` holds a sequence
that crosses the project files, a checkout, the state, the saved sessions or
the events; a surface takes the input and shows the result. The core stays
free of files, so the sequences are not there. The event of a use is recorded
by the operation that did the thing, through the ledger (D120); a surface
records only the conversations its survey saw.

### D123 — revier starts one agent of its own, to write its configuration

A user no longer fills in setup screens: they tell the coding agent they have
what they want. `revier assist`, and alt+a, hand the terminal to Claude Code in
a directory under the state root, briefed with the files, with `revier doctor`
as its check and with the project under the cursor. It is a hand-over, as an
action's is; the command needs no configuration to load, so it also repairs
one. Only this agent is revier's: a project's agent is still only observed.
Rejected: a terminal drawn in the surface, an emulator to maintain; a project
of its own, which a configuration that does not load takes with it.

### D124 — the project pane mirrors one agent's panel, as the agent list's pane does

One agent drawn two ways, its message set from the transcript beside the
projects and its panel beside the agents, read as two agents, and a transcript
is one harness's (D106). So one mirror (D111) serves both panes. The project
pane mirrors an agent before any keypress: one that needs the user, then one
at rest, working, unknown, the latest to speak among equals; the user's choice
overrides the pane's. Rejected: the turn an agent is in above the mirror,
which the screen shows already. Replaces D107 and D122.
