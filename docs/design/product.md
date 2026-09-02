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

Every operation in revier is the same one: **run-or-raise a named target, and
remember where you came from.** Opening a project is run-or-raise on its home
target. Pressing the editor key is run-or-raise on its editor target. There is
no second mechanism.

## A target is logical; its realization is not

`ctrl+shift+d` means "diff viewer" in every project that declares one. What the
diff viewer *is* depends on which host can provide it:

| Host | The same target, realized |
|---|---|
| Compositor (GNOME, sway, Hyprland) | a Meld window, found by WM class, raised by the compositor |
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
one long-lived process. Its latency depends on the window host. A host that
reports window events (sway) claims within a second of the window appearing.
GNOME reports none through `wctl`, so there the TUI diffs successive surveys and
the claim lands within two refresh intervals, about two seconds. In both cases
the claim happens only while the TUI is open, only within five seconds of the
action, only for a window no declared target matches, and only when one such
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
revier go <target>     run-or-raise a named target in the current project
revier run <action>    run a configured action in the current project
revier attach          bind the focused instance to the current project
revier list [--json]   machine-readable inventory
revier status          the project for the current directory
```

The TUI is one surface with two levels. The top level lists projects, sorted so
the ones needing attention come first. Entering a project lists its targets and
attached instances. Enter activates.

## Scope

**In, for the first version**

- Project inventory from TOML files
- Run-or-raise for every target, both realizations
- Toggle back to home on a second keypress
- Attached instances through the project-scoped picker
- Agent state for Claude Code, shown per project
- kitty runtime, GNOME window control, Claude probe
- `--json` output

**Out, deliberately**

| Left out | Why |
|---|---|
| Dev container execution | The heaviest, least general transport. It belongs to whatever launches the shell, not to a project switcher. |
| SSH sessions | Cut from the first version. A remote runtime is a host like any other and can be added without a redesign. |
| Bulk command execution across projects | A different tool that happens to share the project list. Reachable through `revier list --json`. |
| Popup window geometry | The compositor places windows. The rule that opens revier as a popup is user configuration. |
| Session persistence across reboot | The runtime owns persistence. tmux has it, kitty does not, and revier does not paper over the difference. |
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
