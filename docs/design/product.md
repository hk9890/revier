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

Every operation in revier is the same one: **run-or-raise a named target; a
second press returns to the project's home.** Opening a project is
run-or-raise on its home target. Pressing the editor key is run-or-raise on its
editor target. There is no second mechanism.

## A target is logical; its realization is not

"Diff viewer" means the same thing in every project that declares one. What
the diff viewer *is* depends on which host can provide it (D14):

| Host | The same target, realized |
|---|---|
| Compositor (GNOME) | a Meld window, found by its window class, raised by the compositor |
| Runtime (kitty, tmux) | `nvim -d` in a pane, found by its title, focused by the terminal |

The key, the name, and the promise are identical. Only the resolution differs.
This is why the same binding works on a desktop and on a headless SSH box, and
why a terminal TUI and a GUI application are not two features.

## Two tiers of reach

**Keyed targets** are declared in the project's config and hold a fixed key.
Press it and you land there. Press it again and you are back at home. The same
key always means the same target.

**Attached instances** arrive at runtime — you opened a link, a tool spawned a
viewer. They get no key. One key opens the list of everything bound to the
current project, and you pick from it.

A key is a promise about where you land. Only a declared target can make that
promise, which is why the two tiers exist (D12).

## How an instance becomes bound

| Mechanism | Reliability | Used for |
|---|---|---|
| revier launches it | Deterministic. revier launches, waits for the window to appear, and binds it by its id, whatever its title becomes | Keyed targets |
| You attach it | Deterministic. You bind the focused instance to the current project | Anything |
| Claim on appear | Best effort. After an action runs, revier claims the one new instance that no target matches | An action that opens a link |

The third mechanism exists because the interesting case is the unreliable one.
Opening a link hands off to a browser that is already running, so no new process
appears to correlate against, and the result may even be a tab rather than a
window. Claim-on-appear runs in the TUI, the one long-lived process. No host
reports window events, so the TUI finds the new instance by comparing one
survey with the next (D87).

Declared targets live in the project's configuration as match rules.
Attachments live in revier's own state and last until the application
restarts, because instance ids do not survive a restart.

## Surface

The picker and the monitor are one surface (D8): a list of projects, sorted so
the ones that need you come first, and a pane for the project under the
cursor. Attention decides the order because an agent that waits for you is the
reason to open revier at all. Every command is in the
[CLI reference](../user-guide/cli.md).

The agents of every project are a second list behind the same key (D110). With
many agents across many projects the question is which agent, and the project
list answers it one project at a time. The second list orders every agent by
need. It is a list of its own, not a mode of the project list, which would hide
the projects it is for.

A pane shows one agent's panel as the terminal draws it (D111, D125). No
harness renders a running session for another process, and a transcript is one
harness's format; the panel is what every harness draws. The mirror shows the
whole screen, the harness's own frame included (D137), because a cut by one
harness's layout breaks on its next layout. Only colour passes from the screen
to the surface, so what an agent printed cannot repaint the surface that shows
it.

The popup is the surface in a window revier places and run-or-raises (D76).
Leaving it hides it, and the next press raises it as it was (D86). A cold start on
every press — a terminal, the TUI, and a first survey of every linked host —
is too slow for the one keystroke the product promises.

A shutdown saves the session first, because it is the moment before a
restart, and refuses while an agent works or waits for an answer, because that
agent would lose its turn (D78). The guard sits in the close path, so every way
to close passes it, and it reads the agents again at the close: the plan the
user confirmed is older than the press (D99). Nothing is killed; a window an
application keeps open stays open.

A surface without the focus refreshes slowly, since nobody reads it (D129). A
linked host is asked at a bounded rate, and less often while it does not
answer, because each round is a whole survey on that host (D130). This
machine's rows never wait for a linked host (D114).

## Scope

**In, for the first version**

- Project inventory from TOML files
- Run-or-raise for every target, both realizations
- Toggle back to home on a second keypress
- Attached instances through the project-scoped picker
- Agent state for Claude Code, shown per project
- kitty runtime, GNOME window control, Claude probe
- The popup the desktop key opens: the TUI in kitty on GNOME (D76), hidden and
  raised again (D86)
- `--json` output
- Projects on another machine, surveyed by the revier installed there (D40)

Saved sessions (D46), shutdown (D78), `revier doctor` (D85), `revier each`
(D32), `revier events` (D112) and the config assistant (D135) extend it.

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
panel and reports what it sees. The one agent it starts is its own config
assistant, a project revier builds in, so that it gets run-or-raise, the
survey and restore with no code of its own (D135).

## What degrades, and how far

Degradation is per target, not per feature. A target is available when a host
that can realize it is available.

| Environment | What works |
|---|---|
| kitty and GNOME | every target, both realizations |
| tmux over SSH, no desktop | every target with a runtime realization; window-only targets are unavailable |
| kitty, unsupported compositor | home and agent monitoring; window-only targets are unavailable, and under Wayland the home opens but is not raised |

The mechanism is portable. An individual target may not be, and its config says
so by which realizations it declares.

A mistake in the configuration degrades the same way: what is wrong is
disabled, and nothing above it is (D85). A rule about one target disables that
target, a rule about the project disables that project, and neither disables
the rest. revier is how you reach a broken project, so it must not break with
one, and what is disabled stays listed with its reason, because the surface is
the one place the reason can be read.
