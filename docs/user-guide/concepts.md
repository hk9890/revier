# Concepts

revier is built from a few ideas. Read this page once, and the rest of the
guide is detail.

## Every operation is the same one

Every operation in revier is the same one: **run-or-raise a named target, and
remember where you came from.**

- Opening a project is run-or-raise on its home target.
- Pressing the editor key is run-or-raise on its editor target.
- Pressing the same key again, while that target has the focus, returns you to
  home.

There is no second mechanism. A desktop key, `revier go`, `revier open`, and
`Enter` in the TUI all end in the same run-or-raise.

## A project

A project is a directory, a name, and the targets bound to it. Each project is
one TOML file under `~/.config/revier/projects/`, and the file name is the
project's name: `demo.toml` is the project `demo`. `revier new` writes one.

A project can also live on another machine. Its file is then a link: it names
the host, and the revier installed there does the work. [Links](links.md)
covers it.

To act, revier must first know which project you mean. It asks, in this order:

1. The project you named, with a name or `-p <name>`.
2. The project whose directory holds the current directory. Where projects
   nest, the deepest one wins.
3. The project of the focused window.
4. The project revier acted on last.

A desktop key has no useful directory, so for a key it is the focused window,
then the last project. `revier status` prints the project the current directory
resolves to.

## A target

A target is a named thing you reach with a key: the workspace, an editor, a
diff viewer, a page. Most targets have a `key`. Press it and you land there.

A target is logical. `ctrl+shift+d` means "diff viewer" in every project that
declares one. What the diff viewer *is* depends on which host can provide it:

| Host | The same target, realized |
|---|---|
| A window host (GNOME) | a Meld window, found by its window class, raised by the desktop |
| A runtime host (kitty, tmux) | `nvim -d` in a terminal, found by its title, focused by the terminal |

The key, the name and the promise are the same. Only the result differs. This is
why one binding works on a desktop and on a headless box over SSH. A target can
declare one realization or both. When both hosts are present, the window wins,
unless the target sets `prefer = "runtime"`.

A target opens only when nothing matches it. `launch` runs only when `match`
finds no open instance, so every target is run-or-raise, not a launcher.

Targets most projects share, such as an editor, are declared once in
`~/.config/revier/config.toml`. A project file then changes only the fields it
needs. [Projects](projects.md) covers this.

## Home and the workspace

Every project has one home target. Home is the workspace: the terminal you live
in and return to. Toggle-back always returns there.

The workspace is a runtime instance. In kitty it is an OS window, and in tmux
it is a session. The file `revier new` writes calls it `session:<name>` and
opens it with one tab, `agent`.

A tab holds panels. A panel is one pane of the terminal: an agent, a shell, or
a tool. The `agent` tab holds two panels: Claude Code, and a shell beside it.
`revier agent new` and `revier shell new` add more tabs to an open workspace.

## Run-or-raise and toggle-back

A press of a target's key does one of three things:

- **Run.** Nothing matches the target, so revier launches it, waits for it to
  appear, and gives it the focus.
- **Raise.** The target is open but has no focus, so revier brings it to the
  front.
- **Toggle back.** The target already has the focus, so revier goes to the
  project's home.

A press of the home key never toggles away from home.

Toggle-back fires only when revier is sure the target has the focus, since a
wrong guess sends you home when you asked to go somewhere. On a desktop, the
window host says which window has the focus. A tmux session cannot say which
window holds it, so with a window host present a tmux target does not toggle
back.

## Agent states

An agent is a panel that runs a coding agent. A probe reads each agent panel
and reports one of four states:

| State | In the TUI | Means |
|---|---|---|
| `attention` | needs you | The agent asked you something and waits: a permission, a question. |
| `running` | working | The agent is in a turn. |
| `idle` | idle | The agent finished its turn and waits for the next prompt. |
| `unknown` | unknown | No probe could tell. |

revier reads the state of Claude Code 2.1.212 or newer, with nothing to
install. opencode is
recognised as an agent, and always shows as unknown. A probe of your own can
report the state of another agent: see [Probes](extending/probes.md).

The TUI sorts the projects with an agent that needs you first. `revier agent
wait --until` also takes `stopped`, which means `idle` or `attention`: the
agent is not working.

## Two tiers of reach

revier reaches two kinds of thing.

**Keyed targets** are declared in a project file and hold a fixed key. The same
key always means the same target. A key is a promise about where you land, and
only a declared target can make that promise.

**Attached instances** arrive while you work: you opened a link, a tool opened
a viewer. They get no key. They appear under the project in the TUI, and you
reach them from there.

## How an instance becomes bound

An instance is one open window, session or tab. revier binds an instance to a
project in one of three ways:

| How | Reliability | Used for |
|---|---|---|
| revier launches it | Certain. revier waits for the target's window to appear and binds it by its id. Later presses find it by that id, whatever its title becomes. | Keyed targets |
| You attach it | Certain. `revier attach` binds the focused window to the current project. It needs a window host. | Anything |
| Claim on appear | Best effort. After an action runs, the TUI claims the next new window that no target matches. | An action that opens a page with `xdg-open` |

Claim on appear exists because opening a link often goes to a browser that
already runs: no new process starts, and the result can be a tab rather than a
window. The claim happens only while the TUI is open, only within five seconds
of the action, only for a window no declared target matches, and only when
exactly one such window appeared. The TUI notices the window within about two
seconds.

When you attach a terminal window, revier also attaches the terminal inside it,
so the agent in it shows on the project. It does this only when the window and
the terminal pair without doubt. Otherwise the window is attached alone, and
shows no agent.

Attachments live in revier's state, not in the project file, because a window
id does not survive a restart of its application. A saved session does not
record them.

## Window host and runtime host

revier talks to two kinds of host:

- **The runtime host** runs terminals: kitty or tmux. It opens the workspace
  and its tabs, and it lets a probe read each panel.
- **The window host** manages the desktop's windows: GNOME, through `wctl`. It
  opens and raises window targets, says which window has the focus, and raises
  the terminal window a runtime instance lives in.

revier picks the first host of each kind that works on this machine: kitty,
then tmux, and GNOME. `[hosts]` in `config.toml` names them in another order,
or turns one off. [Configuration](configuration.md) has the fields.

The desktop keys are a third thing: GNOME's keyboard shortcuts, which
`revier keys install` writes. revier is one process per keypress. There is no
daemon. The TUI is the one process that stays.

## What degrades, and how far

A target is available when a host that can realize it is available. What is
missing costs only the targets that need it:

| Environment | What works |
|---|---|
| kitty and GNOME | every target, both realizations |
| tmux over SSH, no desktop | every target with a runtime realization; window-only targets are unavailable |
| kitty, another desktop | home and agent state; window-only targets are unavailable |

A mistake in the configuration degrades the same way. A mistake disables only
what is wrong, because revier is how you reach a broken project, so it must not
break with one:

| What is wrong | What it costs |
|---|---|
| a field name revier does not know, such as a typo | nothing: the field is ignored |
| a rule about one target | that target. It reports `invalid` with its reason, and its key says why when pressed and launches nothing. |
| a rule about the project: no home, no path, an unusable name | that project. It is listed with its reason, and its sound targets still resolve. |
| a project file that is not TOML | that project. It is listed by its file name, with the parse error. |
| `config.toml` itself does not load | everything: nothing is left to read the rest with |

Every other project, the TUI and the desktop keys keep working. Other commands
warn on stderr that some files have problems. `revier doctor` prints every
problem with the edit that fixes it. [Troubleshooting](troubleshooting.md)
starts there.

## Where revier keeps its files

revier keeps what you write apart from what it learns.

What you write, under `~/.config/revier/`:

| Path | Holds |
|---|---|
| `config.toml` | Settings for the whole machine: the TUI, hosts, shared targets, actions, statuses, probes. |
| `projects/<name>.toml` | One project, or one link to a project on another machine. |

What revier writes, under `~/.local/state/revier/`:

| Path | Holds |
|---|---|
| `state.json` | The last project, and the attached instances. Safe to lose: revier starts over without it. |
| `logs/revier-YYYY-MM-DD.log` | The log. One JSON line per record, one file per day, kept 14 days. Every revier process writes to the same file. |
| `events.jsonl` | What revier did in each project, which `revier events` prints. Never pruned. |
| `sessions/` | Saved sessions, one TOML file each. |
| `runs/` | The output of each `revier each` run. |
| `assist/` | The folder `revier assist` starts Claude Code in. |

The ssh connection revier shares with a linked host is a socket under
`$XDG_RUNTIME_DIR/revier/`.

`XDG_CONFIG_HOME` and `XDG_STATE_HOME` move the two roots as usual:
`$XDG_CONFIG_HOME/revier` and `$XDG_STATE_HOME/revier`.
[Command line](cli.md) lists the environment variables revier reads.
