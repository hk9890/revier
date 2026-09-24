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
revier session save    record the projects that are open now (D46)
revier session restore open what a saved session recorded
revier session list    the saved sessions, newest first
revier shutdown        save the session when it changed, then close (D78)
```

The TUI is one surface: a list of projects, sorted so the ones needing
attention come first, and a pane beside it for the project under the cursor.
A row says a project is open while anything here holds it - a target, or a
terminal attached by hand - and counts the agents a panel here shows, which
are the agents the surface can go to, type into and close. An agent of a link
that was opened on its host, and one this machine serves to a terminal
elsewhere, are in the survey and on no row (D104).
The list and the pane's targets and agents are three sections, each with its
own query over its rows; Tab walks them and shift+tab walks back (D73). Enter on
a project opens its home target, on a target runs it, and on an agent makes its
tab current and raises its window (D74). alt+e, or the project's name over the
pane, opens the project screen: the project file as the config screen is
`config.toml`, with the shared targets shown beside what the project changes of
them (D81). del closes the row under the cursor - a project, a target, an
attached window or an agent's tab - and alt+del deletes what the configuration
holds of it: a project's file, which for a link is the link alone, or a target
the project file declares. What is open closes first (D93).
The sessions screen is the three `revier session` commands on the surface: the
saved sessions, a pane with the restore plan for the one under the cursor, Enter
to restore it and a save button on its top line (D46, D49). The shutdown wizard
is `revier shutdown` on the surface: every project or one, then what of it, then
the plan to confirm, and the result shown with the surface still up (D78).
Every close reads the plan's agents again first, its own steps only, and the
forced press reads them too: one busy since saves nothing and closes nothing,
and the plan comes back with it named, where confirming it is the force (D99).
A del that would have closed at once asks there too. A step whose agents that survey could not read - its host did
not list, its project is no longer surveyed, its link host did not answer - is
left open and named in the result, and the rest of the plan closes: one dead
remote host does not stop the other projects.

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
so it must not break with one.

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
