# Concepts

Read this page once. Each word below has one meaning in the whole guide.

## One operation

Every operation in revier is the same one: **run-or-raise a named target**,
by revier or by a command you name. A desktop key, `revier go`, `revier open`
and Enter in the TUI all do it.

A press of the key of a window or runtime target does one of three things:

- **Run.** Nothing matches the target. revier launches it.
- **Raise.** The target is open but has no focus. revier raises it.
- **Toggle back.** The target has the focus. revier goes to the project's
  home, not to the window you were in before.

A press on the home target itself only raises it.

A command target is the one exception: revier does not run or raise it
itself. A press runs your command, and the command shows the thing and raises
its window. A second press goes home while the focus is still where the
command left it. See [Which kind to declare](#which-kind-to-declare).

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
`revier agent new` and `revier shell new` without `--panel` stop at step 2,
and read `--dir` in place of the current directory: from a directory in no
project they fail, so an agent never starts in a project you did not mean.

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

A third part, `[target.command]`, takes the place of both: revier runs your
command, and the command does what a host does for the other parts.

## Which kind to declare

Start from what one press of the key should do.

| You want the key to... | Declare | Second press |
|---|---|---|
| Reach a terminal workspace or a tab in it | a target with a `runtime` part | goes home |
| Reach a program with its own window, such as an editor or a diff viewer | a target with a `window` part | goes home |
| Show something in a window you share with other projects, such as a browser tab group | a target with a `command` part | goes home, if the focus is still where the command left it |
| Run a script once, with nothing to reach, such as a sync or a copy | an `[[action]]` | runs it again |
| Start one more agent or shell beside the open workspace | nothing; use the agent or shell tab key | opens one more |

**Use a window or runtime part** when the program makes its own window or tab
for this project. revier finds it, raises it, or launches it and waits for it.
These targets are saved and restored with a session.

**Use a command part** when no new window appears. revier runs your command
and then stops: it does not wait for a window or attach one.
[Command targets](configuration.md#command-targets) says what your command
must do.

```toml
[[target]]
name = "web"
key  = "ctrl-shift-i"
  [target.command]
  run = ["repo-web", "{{.Name}}", "{{.GitURL}}"]
```

**Use an action** when the result is not a place to go. An action key works
only in the TUI. Use `revier run <name>` to run one from a shell.

If a command target fails, `revier go` prints the command's last error line on
stderr. In the TUI, the footer names the failure, and the logs screen (alt+l)
shows that line. A desktop key shows nothing, so have your command send its
own notification.

The fields of each kind are in [Targets](configuration.md#targets) and
[Actions](configuration.md#actions).

## Home, tabs and panels

Each project has one home target: the workspace you return to. In kitty it is
an OS window. In tmux it is a session. In herdr it is a workspace. A
workspace holds tabs, and a tab holds panels. A panel is one pane: an agent,
a shell or another tool.

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
install. Outside herdr, opencode is always `unknown`. To read another harness, write a
[probe](extending/probes.md).

## Attached windows

A window you open while you work, such as a browser page, has no target.
`revier attach` attaches the focused window to the current project. The TUI
then lists it in the project's Targets, and Enter raises it. A session does
not save it.
