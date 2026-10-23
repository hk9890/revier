# Concepts

Read this page once. Each word below has one meaning in the whole guide.

## One operation

Every operation in revier is the same one: **run-or-raise a named target.** A
desktop key, `revier go`, `revier open` and Enter in the TUI all do it.

A press of a target's key does one of three things:

- **Run.** Nothing matches the target. revier launches it.
- **Raise.** The target is open but has no focus. revier raises it.
- **Toggle back.** The target has the focus. revier goes to the project's
  home, not to the window you were in before.

A press on the home target itself only raises it.

## Hosts

revier drives two kinds of program, called hosts:

- **The runtime host** runs terminals: kitty, tmux or herdr. It opens the workspace
  and its tabs, and lets a probe read each panel.
- **The window host** manages desktop windows: GNOME, through `wctl`. It opens
  and raises window targets, and tells which window has the focus.

revier uses the first host of each kind it finds: kitty, tmux, then herdr, and GNOME
([Hosts](configuration.md#hosts)). There is no daemon: each keypress is one
revier process.

A missing host costs only the targets that need it. Over ssh with tmux and no
desktop, every target with a runtime part works, and window-only targets do
not. In the same way, a configuration mistake costs only one target or one
project; `revier doctor` shows it ([Mistakes](configuration.md#mistakes)).

## Project

A project is a directory, a name and its targets, in one file:
`projects/<name>.toml` in the configuration directory. Settings for all
projects are in `config.toml` ([Files](configuration.md#files)). A project can
also live on another machine ([Links](links.md)).

revier finds the project you mean in this order:

1. The project you name, as an argument or with `-p <name>`.
2. The project whose directory holds the current directory. The deepest wins.
3. The project of the focused window.
4. The project revier acted on last.

A desktop key has no useful directory, so it starts at step 3.

## Target

A target is a named thing you reach with a key: the workspace, an editor, a
diff viewer, a page. A target is logical: Ctrl+Shift+D means "diff viewer" in
every project that declares one. Each host has its own part of the target,
which says how that host opens and finds it:

| Part | The diff viewer |
|---|---|
| `[target.window]`, for the window host | a Meld window, found by its window class |
| `[target.runtime]`, for the runtime host | `nvim -d` in a terminal, found by its title |

A target can have one part or both. When both hosts work, the window part
wins, unless the target sets `prefer = "runtime"`. Targets that most projects
share, such as an editor, go once in `config.toml`
([Shared targets](configuration.md#shared-targets)).

## Home, tabs and panels

Each project has one home target: the workspace you return to. In kitty it is
an OS window. In tmux it is a session. A workspace holds tabs, and a tab holds
panels. A panel is one pane: an agent, a shell or another tool.

## Agents

An agent is a panel that runs a coding agent. The **harness** is the agent
program, such as Claude Code or opencode. A **probe** knows one harness and
reads its state:

| State | In the TUI | Means |
|---|---|---|
| `attention` | needs you | The agent asked a question or a permission, and waits for you. |
| `running` | working | The agent is in a turn. |
| `idle` | idle | The agent finished its turn and waits for a prompt. |
| `unknown` | unknown | A probe knows the agent but cannot read its state. |

A program that no probe knows is not an agent, and revier does not list it.
revier reads [Claude Code](integrations/claude-code.md) with nothing to
install. opencode is always `unknown`. To read another harness, write a
[probe](extending/probes.md).

## Attached windows

A window you open while you work, such as a browser page, has no target.
`revier attach` attaches the focused window to the current project. The TUI
then lists it in the project's Targets, and Enter raises it. A session does
not save it.
