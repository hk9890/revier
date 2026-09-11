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

Early, but usable. There is no release yet; build from source.

Working: project files, run-or-raise with toggle-back, the project resolved
from the focused window, agent state for Claude Code (opencode is recognised
but reports no state), external probes, kitty and tmux runtime hosts, GNOME and
sway window hosts, claim-on-appear for windows opened after a launch, the TUI,
every command below, and the GNOME keybindings under `contrib/`.

To follow the design, read [docs/design/](docs/design/README.md) — it specifies
the system and logs the decisions behind it.

## Usage

```
revier                        the TUI: every project, its agent state, its targets
revier list [--json]          the same, printed once
revier open [name]            run-or-raise a project's workspace
revier new [name]             write a project file for this directory
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
revier each -- <cmd>          run one command in every project's directory
revier each log [run]         past runs, or how each project ended in one
```

In the TUI, projects whose agent is waiting for you sort first. Type to filter
by name, Enter opens the project's home, Tab lists its targets, Enter on a
target runs-or-raises it, Esc goes back. alt+e opens the selected project's
file in `$EDITOR`; alt+d deletes it, after asking. A configured action key runs
the action against the selected project.

`revier each -- git pull --ff-only` runs one command in the directory of every
project, one project at a time, and skips a project whose directory is missing.
`--filter 'test -d .git'` narrows the run to the directories where a shell test
passes, and `--dry-run` prints the selection and runs nothing. Each project's
output is kept under `~/.local/state/revier/runs/<run>/`; `revier each log`
lists past runs, and `revier each log <run> <project>` prints one project's
output. A run in which any project failed exits 5.

Projects are TOML files under `~/.config/revier/projects/`; the format, with a
worked example, is [docs/design/extending.md](docs/design/extending.md).
`revier new` writes one for the current directory, and `revier open <name>`
does the same for a name revier does not know yet. A project whose directory is
missing is cloned from its `git_url` when it is opened.

## Keybindings

revier is one process per keypress: a desktop binding runs `revier go
<target>` and exits. `contrib/gnome/` holds the GNOME bindings that replace
the shell implementation's `os-*` shortcuts, on the same keys:

| Key | Runs |
|---|---|
| `Alt+Space` | `revier-popup`: the TUI in a kitty window of class `revier-popup`, or the one already open |
| `Ctrl+Shift+U` | `revier-go home` |
| `Ctrl+Shift+O` | `revier-go editor` |
| `Ctrl+Shift+I` | `revier-go web` |

`revier-go` runs the target and opens the picker when no project resolves,
because a key pressed on a window no project claims should still do something.

To install:

1. Run `mise run install`. It puts `revier`, `revier-popup` and `revier-go` in
   `~/.local/bin`, which a login shell has on its PATH.
2. Run `revier keys install --dry-run` to see what would change, then
   `revier keys install`.

Without `--force`, install adds only the keys nothing else holds, and reports
the rest. `--force` takes a key from whatever has it: another shortcut is
switched off and keeps its command, so the tool that wrote it can put it back;
a GNOME default is cleared, and the `gsettings reset` line that returns it is
printed beside the key.

`revier keys uninstall` removes what revier wrote and nothing else. It restores
no other tool's shortcuts — that tool does. To go back to the shell
implementation, run `revier keys uninstall` and `os init`, in either order:
`os init` alone leaves revier's shortcuts firing beside its own.

The key that opens the TUI is `[ui] trigger_key` in `config.toml`, `alt-space`
by default; the rest are the `key` each target declares.
`revier keys status` says who holds each of them and only reads.

Placement of the popup is a compositor rule, not revier's: on GNOME, a
`wctl place` line in `revier-popup` after the launch.

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

Contributor guides live in [docs/](docs/): [CODING.md](docs/CODING.md),
[TESTING.md](docs/TESTING.md), [RUNNING.md](docs/RUNNING.md).
