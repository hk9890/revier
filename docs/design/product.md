# Product

## The job

You run several projects at once. Each one holds a coding agent, a shell, and
the things that belong with them — an editor, a diff viewer, a page. The agents
work unattended for minutes at a time. The questions you ask all day are:

> Which of my agents is working, which is idle, and which one is waiting for me?
>
> Where is the editor for this project, and how do I get back to the terminal?

revier answers both, in one keystroke each.

A *Revier* is a hunting ground or a patrolled district. Your projects are the
territory. revier is how you walk it.

## Model

| Term | Meaning |
|---|---|
| **Project** | A directory, a name, and the targets bound to it |
| **Target** | A named thing you reach with a key: the workspace, an editor, a diff viewer, a page |
| **Home** | The project's workspace target — the terminal you live in and return to |
| **Panel** | One pane inside a runtime target: an agent, a shell, a tool |
| **Agent state** | What an agent panel is doing: idle, running, or waiting for you |
| **Session** | A recorded set: which projects were open, which of their targets, and each agent's conversation and directory (D46, D62) |

Every operation in revier is the same one: **run-or-raise a named target, and
remember where you came from.** Opening a project is run-or-raise on its home
target. Pressing the editor key is run-or-raise on its editor target. There is
no second mechanism.

## A target is logical; its realization is not

`ctrl+shift+d` means "diff viewer" in every project that declares one. What the
diff viewer *is* depends on which host can provide it:

| Host | The same target, realized |
|---|---|
| Compositor (GNOME) | a Meld window, found by WM class, raised by the compositor |
| Runtime (kitty, tmux) | `nvim -d` in a pane, found by title, focused by the terminal |

The key, the name, and the promise are identical. Only the resolution differs.
This is why the same binding works on a desktop and on a headless SSH box, and
why a terminal TUI and a GUI application are not two features.

Both realizations are in use from the first version on one machine: a ticket
viewer is a terminal TUI in a kitty tab, an editor is an IntelliJ window.

## Two tiers of reach

**Keyed targets** are declared in the project's config and hold a fixed key.
Press it and you land there. Press it again and you are back at home. The same
key always means the same target.

**Attached instances** arrive at runtime — you opened a link, a tool spawned a
viewer. They get no key. One key opens the list of everything bound to the
current project, and you pick from it.

A key is a promise about where you land. Only a declared target can make that
promise, which is why the two tiers exist.

## How an instance becomes bound

| Mechanism | Reliability | Used for |
|---|---|---|
| revier launches it | Deterministic. revier launches, waits for the window of the target's class to appear, and binds it by id; every later press finds it by that id, whatever its title becomes | Keyed targets |
| You attach it | Deterministic. `revier attach` binds the focused instance to the current project | Anything |
| Claim on appear | Best effort. After an action runs, revier claims the next new instance no target matches, for a short interval | `xdg-open` from the terminal, as an action |

The third mechanism exists because the interesting case is the unreliable one.
Opening a link hands off to a browser that is already running, so no new process
appears to correlate against, and the result may even be a tab rather than a
window. Claim-on-appear settles an action's launch, and it runs in the TUI, the
one long-lived process. The TUI diffs successive surveys and the claim lands
within two refresh intervals, about two seconds; no host reports window events
(D87). The claim happens only while the TUI is open, only within five seconds
of the action, only for a window no declared target matches, and only when one such
window appeared.

A keyed target's launch is settled the same way but by the launching process
itself, which waits for the window and binds it, so it needs no TUI. The TUI
finishes the binding only when the window took longer than that wait.

A window carries no panels, so an attachment or a claim records the terminal
inside the window beside it (D95). It does so only when the window pairs with
one beyond doubt, the pairing D63 and D67 refuse to guess at; an ambiguous
window is recorded alone and reports no agent, which is the honest answer.
The two are one attached row, the terminal's, because that side holds the
panels. The row is chosen from the window's side, and the terminal the window
names has to be attached too. Asked from the terminal's side, which window
holds this terminal is a question a shared title answers twice, and the
survey then folded the wrong window away and showed one attachment as two
rows, in the pane and in a close plan.

Declared targets live in the project's TOML as match rules. Attachments are
per-session and live in revier's own state, because instance ids do not survive
an application restart.

## Surface

```
revier                 the TUI: every project, its agent state, its targets
revier open [name]     run-or-raise the home target, without UI
revier new [name]      write a project file for the current directory
revier go <target>     run-or-raise a named target in the current project;
                       --picker opens the popup when no project resolves
revier popup           run-or-raise the TUI in a kitty window revier places (D76)
revier run <action>    run a configured action in the current project
revier attach          bind the focused instance to the current project
revier list            what a person here reads: the agents a panel here shows
revier list --json     the survey whole, which the revier on another machine
                       reads about its own links (D104)
revier status          the project for the current directory
revier doctor          every file that did not load whole, and its fix (D85)
revier each -- <cmd>   run one command in every project's directory (D32)
revier events          what revier did in each project, one JSON line per
                       event, with what each linked host recorded (D112)
revier session save    record the projects that are open now (D46)
revier session restore open what a saved session recorded
revier session list    the saved sessions, newest first
revier shutdown        save the session when it changed, then close (D78)
```

The TUI is one surface: a list of projects, sorted so the ones needing
attention come first, and a pane beside it for the project under the cursor.
The cursor follows its project as the rows reorder while the project is open.
When the project closes or is deleted the cursor keeps its place, on the row
that moved up or the last row, and comes back from the pane to the list; a
full shutdown that closed everything puts it on the first row (D109).
A row says a project is open while anything here holds it - a target, or a
terminal attached by hand - which is `ProjectView.Held`, while run-or-raise
and a restore act on the home target alone. It counts the agents a panel here
shows, which are the agents the surface can go to, type into and close. An
agent of a link that was opened on its host, and one this machine serves to a
terminal elsewhere, are in the survey and on no row (D104). Every surface over
this machine's projects draws only the agents a panel here shows,
`Core.Shown`; the survey keeps the rest, which `revier list --json` owes the
revier on the other machine about the agents its own links started here, and
which a shutdown reads to see that this machine is busy. The link dialog's
pane is the host's inventory rather than this machine's projects, and draws
the host's word whole. So every agent a surface shows sits in a panel of an
instance `Held` counts, and "closed and no agents" is an invariant of the row
rather than a coincidence of the filter.
The pane starts level with the project query.

A link's agents are the host's and this machine's, added together (D101). A
terminal attached to a link by hand is a terminal of this machine and is
probed here. The link's own targets are not: they are panels running an ssh,
and what the agent on the far side does is its host's word (D84). The two
sides can still meet on one agent, since whether a probe claims a panel
running an ssh is the probe's business and the host names that agent under
the tag the panel gave it. They are told apart by the panel each landed on,
not by assuming they cannot meet, and the local reading stands.

The pane ends in what one of the project's agents said last, and how long ago
(D106, D107). Without a keypress it shows the agent that needs the user; else
one with a message to show, at rest before working; the latest to speak among
equals. Moving the agent cursor chooses, and the choice holds until another
project is under the list's cursor. A message longer than the room keeps its
end, under an ellipsis. Its text is set no wider than 100 columns, and a wide
pane lays it beside the facts, level with the name, once it has all 100. It is
drawn as text and styled in revier's own colours: nothing the message carries
is styling, and nothing in it speaks to the terminal (D108). A remote project's
agent is not read, and the pane says so.

The key that opens the surface, pressed on it, puts the agent list in the
project list's place, and pressed again the projects; the bar's first button
does the same and names where it goes (D110). A terminal too narrow for every
button of the bar shows the first ones whole and ends in a burger, which turns
the bar to the rest; it has no key, since each button behind it has its own
(D49). The agent list is every agent a
panel here shows, one row each: its state, what it is on, and how long ago it
spoke, over its project and the directory it works in when that is not the
project's own. An agent with no activity line yet shows the first line of what
it said last. The rows stand by state - waiting for the user, working, at rest,
unknown - and those waiting and at rest by when each spoke, the latest first;
the working and the unknown stand by project. The cursor stays on its agent
through every reordering, and the rule totals the rows by state. Typing filters
them, Enter goes to the agent, and del closes its tab. The list opens on its
first row, and the project list keeps its cursor and query across the switch.
An agent the transcript says nothing about - another harness, a link's - has no
age and stands last in its state.

Beside the agent list the pane names the agent as it names a project - the
agent's activity as the title, then its state, project, path, git URL, harness
and when it spoke - and under that mirrors the agent's panel (D111): the end of
what its terminal shows, read once a second, in the terminal's own colours. A
line wider than the pane continues on the next. The wheel scrolls back into the
panel's scrollback, which is read only while the view is scrolled into it, and
the view holds its lines as more are written under them. A link's agent is
mirrored as any other, since the panel is here. A panel no runtime here can
read leaves a note in the mirror's place.

The list and the pane's agents each have a query over their rows, kept while
their project is, until a press opens something; then both end, and each
cursor stays on the row it found. Tab and shift+tab move between the two, and
alt+t to the targets, which have no query; Tab from the targets goes back to
the projects (D105). One click selects a row of the list or the pane, and a
second opens it.

Enter on a project opens its home target, or on one with no home puts the
cursor on its targets; on a target it runs it, and on an agent it makes its tab
current and raises its window (D74). alt+e, or the project's name over the
pane, opens the project screen: the project file as the config screen is
`config.toml`, with the shared targets shown beside what the project changes
of them (D81). del closes the row under the cursor - a project, a target, an
attached window or an agent's tab - and alt+del deletes what the configuration
holds of it: a project's file, which for a link is the link alone, or a target
the project file declares. What is open closes first (D93).

The popup is the surface in a kitty window of class `revier-popup`, which
`revier popup` run-or-raises and places under D24 (D76). It is centred at the
workarea's full height and a fixed 1800 px width, or fills a narrower
workarea, decided before the launch so the window never resizes. The width is
fixed rather than a share of the workarea, so that on a wide screen everything
stays in one spot. `go --picker` falls back to the popup, and plain `go` keeps
its exit status for scripts.

Esc minimizes the popup through the window host, and the next press raises it
with the cursor and the query where they were (D86), on the projects whichever
list it was hidden on (D110). Neither plays the shell's animation where the
host can skip it. A press while the focused window shows the surface is typed into
its panel as the key that switches the list, because the desktop takes the key
before the terminal sees it. That window is the popup, or a terminal whose
current panel runs revier with no command, paired with its window as an
attachment is (D95); there no popup opens. A runtime that cannot type into the
panel leaves the press what it was: the popup, raised or opened. Hidden, it
surveys nothing, since every survey reaches every linked host; the terminal's focus
report on the raise reads the project files again, since a press used to load
them, then runs one survey and resumes the refresh. The surface knows it is
the popup, and which project to open on, from the environment its terminal is
started with, read and dropped at start so nothing it launches inherits it.
The project is resolved at the keypress, while the focused window is still the
user's. In any other terminal, and where the host cannot hide, Esc exits.

The sessions screen is the three `revier session` commands on the surface: the
saved sessions, a pane with the restore plan for the one under the cursor,
Enter to restore it and a save button on its top line (D46, D49).

The shutdown wizard is `revier shutdown` on the surface: every project or one,
then what of it - the whole, only its agents, or only what holds no agent -
then the plan to confirm, and the result shown with the surface still up
(D78). A window an application keeps open is named as still open, never
killed, and a project's shutdown leaves an instance another project also
holds. Every close reads the plan's agents again first, its
own steps only, and the forced press reads them too: one busy since saves
nothing and closes nothing, and the plan comes back with it named, where
confirming it is the force (D99). A del that would have closed at once asks
there too. A step whose agents that survey could not read - its host did not
list, its project is no longer surveyed, its link host did not answer - is
left open and named in the result, and the rest of the plan closes: one dead
remote host does not stop the other projects.

The guard is in `core.Shutdown`, so `revier shutdown`, the wizard and del all
pass it (D99). A busy agent refuses the whole plan, because the user is about
to be asked about it and the plan is what they read. The recheck is the
freshest survey there is, so what closes, the saved session and the order
that closes this process's own terminal last are all read off it, not off the
listing the plan was drawn from. Forcing says not to refuse and nothing else:
a force that skipped the reading wrote back into the session a window the
user had closed by hand while the confirm stood, and kept a step marked
unread by the refusal before it. A shutdown left with nothing to close saves
nothing, since it changes nothing and a survey that could not read the desktop
is not the one to write a session from. Whether anything closes is read from
each step's action and not from the reason it carries: a step no host can
close carries no reason and closes nothing either, and a shutdown of nothing
but those would otherwise record the desktop as shut down.

A step closes a tab as every panel in it (D94). `Panel` names the tab that
holds it, and the step carries every panel of that tab and closes each
through `PanelCloser`: kitty ends a tab with its last window and tmux a window
with its last pane. The step holds the agents of all those panels, so a busy
one beside the marked panel refuses it, and it counts as closed only once no
panel of it is listed. An agent's own step closes its tab the same way,
because the tab `revier agent new` opens carries no target mark to be closed
by. The workspace's own tab is the exception, where the shell a layout
declares beside the agent stays. An instance that names no own panel (D100)
is read as that exception too: taking the tab there is a guess, and the wrong
guess closes the declared shell, while the guess the other way leaves one
shell of an added tab for the next shutdown. The panels are the tab as the
close finds it, not as the plan drew it: the recheck reads a step's panels
again with its agents, so a panel added between the confirm and the close
closes with it, and an agent working there refuses the close like any other.
The recheck adds and drops no step, which is what D78 holds fixed.

## Scope

**In, for the first version**

- Project inventory from TOML files
- Run-or-raise for every target, both realizations
- Toggle back to home on a second keypress
- Attached instances through the project-scoped picker
- Agent state for Claude Code, shown per project
- kitty runtime, GNOME window control, Claude probe
- The popup the desktop key opens: the TUI in kitty on GNOME, sized to show
  the detail pane (D76), hidden on Esc and raised by the next press (D86)
- `--json` output
- Projects on another machine, surveyed by the revier installed there (D40)

Saved sessions (D46), shutdown (D78), `revier doctor` (D85), `revier each`
(D32) and `revier events` (D112) extend it.

**Out, deliberately**

| Left out | Why |
|---|---|
| Dev container execution | The heaviest, least general transport. It belongs to whatever launches the shell, not to a project switcher. |
| The contents of a pane across a reboot | The runtime owns persistence. tmux has it, kitty does not, and revier does not paper over the difference. The *set* of open projects is revier's own model and is recorded (D46). |
| Browser tabs | A tab cannot be enumerated or activated from outside the browser. An app-mode window can, which is what a bound page is. |

## Non-goals

revier is not a terminal multiplexer and does not replace one. It sits above
whatever runtime you already use and gives it a project-shaped view.

revier does not run your agent, wrap it, or proxy its output. It observes a
panel and reports what it sees.

## What degrades, and how far

Degradation is per target, not per feature. A target is available when a host
that can realize it is available.

| Environment | What works |
|---|---|
| kitty and GNOME | every target, both realizations |
| tmux over SSH, no desktop | every target with a runtime realization; window-only targets are unavailable |
| kitty, unsupported compositor | home and agent monitoring; window-only targets are unavailable |

The mechanism is portable. An individual target may not be, and its config says
so by which realizations it declares.

A mistake in the configuration degrades the same way. What is wrong is
disabled; nothing above it is (D85). revier is how you reach a broken project,
so it must not break with one. An edit revier itself makes is refused for what
it breaks and not for what was broken before it, so a file is repaired one
target at a time; what `revier new` and `revier link` write must come out
whole.

| What is wrong | What it costs |
|---|---|
| a key nothing reads | nothing: it is ignored |
| a rule about one target | that target, which reports `invalid` with its reason; pressing its key fails and launches nothing |
| a rule about the project — no home, no path, an unusable name | that project, listed with its reason; its sound targets still resolve |
| a project file that is not TOML | that project, listed by its file name with the parse error |
| `config.toml` itself unreadable | everything: there is nothing left to read the rest with |

`revier doctor` reports every one of them, with the edit that answers it.
Every other command runs, warns on stderr that some files have problems, and
names `doctor`.
