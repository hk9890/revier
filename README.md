# revier

A project-grouped control surface for running coding agents.

You run several projects at once. Each holds a coding agent, a shell, and the
things that belong with them — an editor, a diff viewer, a page. revier answers
the two questions that come up all day, in one keystroke each:

- Which of my agents is working, which is idle, and which one is waiting for me?
- Where is the editor for this project, and how do I get back to the terminal?

A *Revier* is a hunting ground or a patrolled district. Your projects are the
territory.

## Status

Early, but usable. Linux only.

Working: project files, run-or-raise with toggle-back, the project resolved
from the focused window, agent state for Claude Code 2.1.212 or newer (opencode is recognised
but reports no state), external probes, kitty and tmux runtime hosts, the GNOME window host,
claim-on-appear for windows opened after a launch, the TUI, the popup, every
command below, and the GNOME keybindings.

To follow the design, read [docs/design/](docs/design/README.md) — it specifies
the system and logs the decisions behind it.

## Install

With [mise](https://mise.jdx.dev), which reads the releases page and puts
`revier` on your PATH:

```bash
mise use -g github:hk9890/revier
```

Or download an archive from the
[releases page](https://github.com/hk9890/revier/releases) and put `revier`
on your PATH. Archives are
named `revier_<version>_linux_<arch>.tar.gz`, with `x64` or `arm64` as the
architecture:

```bash
tar -xzf revier_<version>_linux_x64.tar.gz -C ~/.local/bin
```

`revier version` prints what is installed. Each release ships a signed
checksums file; [docs/RELEASING.md](docs/RELEASING.md) says how to verify it.
To install from source instead, see [Building](#building).

## Usage

```
revier                        the TUI: every project, its agent state, its targets
revier list [--json] [name..] the same, printed once, or for the named projects
revier open [name] [--attach] run-or-raise a project's workspace; --attach ends
                              with this terminal on it (tmux)
revier new [name]             write a project file for this directory
revier link [host [project]]  the ssh hosts; a host's projects; or a link to one,
                              written here under --name or the project's own
revier go <target> [-p name]  run-or-raise a target; pressing it again returns home
revier run <action> [-p name] run a configured action in the project
revier attach [-p name]       bind the focused window to a project
revier status                 which project this directory resolves to
revier keys status [--json]   the desktop keys revier wants, and who holds each
revier keys install [--force] claim the desktop keys
revier keys uninstall         release the keys revier holds
revier agent wait <project>[:<target>] --until <status> [--timeout s]
                              block until the agent is idle, running, attention,
                              or stopped; exit 2 on timeout
revier agent prompt <project>[:<target>] <text>
                              type one line into the agent and submit it
revier agent new [-p <project> | --panel <id>] [--resume <id>] [--dir <path>]
                              add an agent tab to an open workspace: the
                              project's agent panel and its shell
revier agent focus <project>[:<target>]
                              switch to the agent's tab and raise its window
revier shell new [-p <project> | --panel <id>] [--dir <path>]
                              add a shell tab to an open workspace
revier session save [--name label]
                              record the projects that are open now
revier session restore [id|name] [--dry-run]
                              open what a saved session recorded; the newest
                              without an argument
revier session list [--json]  the saved sessions, newest first
revier shutdown [project] [--agents | --targets] [--force] [--no-session-save] [--dry-run]
                              save the session when it changed, then close what
                              is open; refuses while an agent is busy
revier each -- <cmd>          run one command in every project's directory
revier each log [run]         past runs, or how each project ended in one
```

In the TUI, projects whose agent is waiting for you sort first. Each project
row counts its agents by state: needs you, working, idle, unknown. The list, and
the pane's targets and agents beside it, are three sections, each with its own
filter field over its rows. Tab moves the cursor to the next section and
shift+tab to the one before. Typing filters the section the cursor is in, and
Enter acts on its row: a project opens its home, a target runs-or-raises, and
an agent's tab comes to the front, on its host for a project on another
machine. Esc clears the section's filter, then brings the cursor back to the
projects. alt+e, or a click on the project's name at the top of the pane,
opens the project screen: its name, path and git URL, and every target it has,
the ones from `config.toml` included. A change there is written to the
project's own file at once, and a new name renames the file. alt+d deletes the
project, after asking. A configured action key runs the
action against the selected project.

The top line holds what is not about one project, each with its key: **new**
(alt+n) adds a project on this machine, **remote** (alt+r)
links a project on another machine from the hosts `~/.ssh/config` names and the
projects the revier on the chosen host has, **sessions** (alt+s) saves and
restores the set of open projects, **shutdown** (alt+q) closes every project
or one after a confirm, **config** (alt+c) sets the
theme, the glyphs, the trigger key and the runtime host, and adds, changes and
deletes shared targets and actions, each written to `config.toml` and applied
as it changes, comments kept, and **help** (alt+h)
lists every key: the surface's own, the target keys, the configured actions
and the desktop keys. The rule over the list says how many projects the
filter leaves.

**new** takes one of three things:

- A full path, starting with `/` or `~`, which Tab completes. That folder
  becomes the project.
- A name. Enter lists the folders your projects already live in, the one
  holding the most first, and the project goes in the one you choose.
- A git clone URL. As a name, with the repository's name; the repository is
  cloned there and the project opens. If the folder is already there, it is
  added as it is, and the URL is ignored when it is not the folder's origin.

A folder that is not there is created, or cloned into, after asking.

With the mouse, whatever the pointer is over lights up. A click on a project
selects it and a double click opens it; one click on a target, an agent or a
top-line button runs it, and a click on a filter field moves the cursor
there. A drag selects a box of the screen, holds the screen still
while it lasts, and copies the box's text to the clipboard on release; Esc
drops it.

`revier each -- git pull --ff-only` runs one command in the directory of every
project, one project at a time, and skips a project whose directory is missing.
`--filter 'test -d .git'` narrows the run to the directories where a shell test
passes, and `--dry-run` prints the selection and runs nothing. Each project's
output is kept under `~/.local/state/revier/runs/<run>/`; `revier each log`
lists past runs, and `revier each log <run> <project>` prints one project's
output. A run in which any project failed exits 5.

`revier session save` writes down which projects are open and which of their
targets, so `revier session restore` can open them again after a restart. It
records names, not windows: the argv comes from the project file as it reads
at restore, a target already up is left alone, and anything that has since
been deleted is named and stepped over. Attached instances are not recorded —
they have no name to be reopened by. Sessions live in
`~/.local/state/revier/sessions/`, one readable TOML file each. In the TUI,
the sessions screen (alt+s) shows the same list; its pane says what a restore
of the selected session would open now, Enter restores it, and alt+s on the
screen saves the open projects under an optional name.

`revier shutdown` closes every open target and attached window, and the agents
in them. `revier shutdown <project>` closes one project; `--agents` closes only
its agents and keeps the workspaces, `--targets` closes only what holds no
agent. Each shutdown first saves the session when it changed since the newest
save, unless `--no-session-save`. While an agent is working or waiting for an
answer, it saves and closes nothing and names the agent; `--force` shuts down
anyway. A window whose application asks about unsaved work stays open and is
named. In the TUI, alt+q asks every project or one, then what of it, and shows
the plan before anything closes.

An agent comes back on the conversation it held if its probe can name one.
Claude Code can, with nothing to install: the save asks `claude agents --json`
which conversation each agent's process holds. An agent the save cannot name -
`claude` typed into a tmux shell pane rather than started by revier - is named
when you save, and comes back starting fresh.

Every agent comes back, not only the one the layout declares: an agent you
opened beside the workspace returns as the tab `revier agent new` opens, and
each agent starts in the directory it worked in. An agent whose worktree was
removed since the save starts fresh in the project instead.
The resume flag is added to the agent panel's `command`, so a command that
wraps the agent must pass its arguments on:
`["sh", "-lc", "exec my-agent \"$@\"", "sh"]`, not `["sh", "-lc", "my-agent"]`.

`revier agent new` opens such an agent by hand: a tab with the project's agent
panel and its shell panel split into it, in `--dir`, in the workspace that is
open. In tmux a workspace is a session, and the tab is a window of it. A kitty
key can pass the window it was pressed in, and
the workspace is found from it. `--copy-env` tells revier which kitty process
the window id belongs to, since every kitty counts its windows from 1:

```
map ctrl+shift+z launch --type=background --copy-env --cwd=current sh -lc 'exec revier agent new --panel "$1" --dir .' sh @active-kitty-window-id
```

`revier shell new` opens a shell the same way: the target's shell panel alone,
or the runtime's shell where the target declares none. It refuses a window
that is no project's workspace, so a key that should open a shell everywhere
falls back to kitty's own tab:

```
map ctrl+shift+t launch --type=background --copy-env --cwd=current sh -lc 'revier shell new --panel "$1" --dir . || kitten @ launch --type=tab --cwd=current' sh @active-kitty-window-id
```

Projects are TOML files under `~/.config/revier/projects/`; the format, with a
worked example, is [docs/design/extending.md](docs/design/extending.md).
Targets most projects share, such as an editor, are declared once as
`[[target]]` in `~/.config/revier/config.toml`; a project file then overrides
only the fields it changes, and adds targets of its own. `revier new` writes
one for the current directory, and `revier open <name>` does the same for a
name revier does not know yet. A project whose directory is
missing is cloned from its `git_url` when it is opened.

A project can live on another machine. `revier link <host> <project>`, or
alt+r in the TUI, writes a link file for it in the same directory. The file
names the host and, when it differs from the file's own name, the project's
name there, and records what the host said about the project: the path and
the repository, both the host's:

```toml
path = "/home/hans/dev/far"
git_url = "git@github.com:hk9890/far.git"

[remote]
host = "buildbox"
project = "far"
```

The workspace is derived: an agent panel and a shell panel, each an ssh that
runs `revier agent exec -p far` or `revier shell exec -p far` on the host, so
the host's project file says which agent runs and where. A link may add
`[[target]]` entries of its own, such as an editor over ssh, which render that
path and that repository. Neither names anything on this machine: the link has
no directory here, and the checkout stays the host's to clone.

revier must be installed on the host, a Linux machine, with the project's own
file under its `~/.config/revier/projects/`; it needs no tmux there. The list
shows the link as `far@buildbox` with what each agent there is doing, read
from the revier there. Enter opens the workspace. Its tabs are tabs of your
terminal: `revier agent new`, `revier shell new`, `revier agent prompt`,
`revier agent wait`, a shutdown and the two keys above work as they do in a
local workspace, and `--dir`, a path on this machine, is dropped. An agent
ends with its tab or its connection; a saved session resumes it on the host.
An agent started on the host from another machine is counted, and cannot be
reached from here. `revier run <action> -p far` runs on the host, so an action
is one the host's `config.toml` defines. A host that does not answer shows as
unreachable. ssh runs in batch mode, so the host has to accept a key;
`ControlMaster` in `~/.ssh/config` keeps the refresh and a new tab fast.

## Keybindings

revier is one process per keypress: a desktop binding runs `revier go
<target> --picker` and exits. These GNOME bindings replace the shell
implementation's `os-*` shortcuts, on the same keys:

| Key | Runs |
|---|---|
| `Alt+Space` | `revier popup`: the TUI in a kitty window of class `revier-popup`, or the one already open |
| `Ctrl+Shift+U` | `revier go home --picker` |
| `Ctrl+Shift+O` | `revier go editor --picker` |
| `Ctrl+Shift+I` | `revier go web --picker` |

`--picker` opens the popup when no project resolves, because a key pressed on a
window no project claims should still do something. Without it, `revier go`
exits with status 3 there, for scripts.

The popup needs kitty, and GNOME with the
[Window Control extension](https://github.com/carlo9890/gnome-window-control)
and its `wctl`; `revier popup` names whichever is missing. A new popup is
centred at full screen height and 1800 px wide, or fills the screen when the
screen is narrower than that. A popup already open is raised where you left it.

To claim the keys, run `revier keys install --dry-run` to see what would
change, then `revier keys install`.

Without `--force`, install adds only the keys nothing else holds, and reports
the rest. `--force` takes a key from whatever has it: another shortcut is
switched off and keeps its command, so the tool that wrote it can put it back;
a GNOME default loses that one key and keeps any other it holds, and the
`gsettings reset` line that returns it is printed beside the key.

`revier keys uninstall` removes what revier wrote and nothing else. It restores
no other tool's shortcuts — that tool does. To go back to the shell
implementation, run `revier keys uninstall` and `os init`, in either order:
`os init` alone leaves revier's shortcuts firing beside its own.

The key that opens the TUI is `[ui] trigger_key` in `config.toml`, `alt-space`
by default; the rest are the `key` each target declares.
`revier keys status` says who holds each of them and only reads.

## How it works

Every operation is the same one: **run-or-raise a named target, and remember
where you came from.** A target is logical — `ctrl+shift+d` means "diff viewer"
in every project that declares one. What it resolves to depends on the host that
can provide it: a Meld window on a desktop, `nvim -d` in a pane over SSH. The
key and the promise do not change.

## Building

Go 1.27, pinned with [mise](https://mise.jdx.dev).

```bash
mise install
mise run build     # ./bin/revier
```

Installing revier is [Install](#install) above.

Contributor guides live in [docs/](docs/): [CODING.md](docs/CODING.md),
[TESTING.md](docs/TESTING.md), [RUNNING.md](docs/RUNNING.md).
