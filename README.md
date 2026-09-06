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
revier go <target> [-p name]  run-or-raise a target; pressing it again returns home
revier run <action> [-p name] run a configured action in the project
revier attach [-p name]       bind the focused window to a project
revier status                 which project this directory resolves to
revier keys status [--json]   the desktop keys revier wants, and who holds each
```

In the TUI, projects whose agent is waiting for you sort first. Type to filter
by name, Enter opens a project's targets, Enter on a target runs-or-raises it,
Esc goes back. A configured action key runs the action against the selected
project.

Projects are TOML files under `~/.config/revier/projects/`; the format, with a
worked example, is [docs/design/extending.md](docs/design/extending.md).

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
2. Run `contrib/gnome/install-keybindings.sh`. It loads
   `revier-keybindings.dconf` and appends its four entries to the
   custom-keybindings list; the entries already there are left as they are.
3. Disable the `os-*` bindings that use the same keys, in Settings > Keyboard,
   or GNOME fires both.

`revier keys status` says which of these keys revier holds and which something
else does, so step 3 can be checked rather than assumed. It only reads. The key
that opens the TUI is `[ui] trigger_key` in `config.toml`, `alt-space` by
default; the rest are the `key` each target declares.

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
mise run test      # the fast layers
```

Contributor guides live in [docs/](docs/): [CODING.md](docs/CODING.md),
[TESTING.md](docs/TESTING.md), [RUNNING.md](docs/RUNNING.md).
