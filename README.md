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

Working: project files, run-or-raise with toggle-back, agent state for Claude
Code (opencode is recognised but reports no state), kitty and tmux runtime
hosts, a GNOME window host, the TUI, and every command below. Not built yet:
desktop keybinding integration.

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
```

In the TUI, projects whose agent is waiting for you sort first. Type to filter
by name, Enter opens a project's targets, Enter on a target runs-or-raises it,
Esc goes back. A configured action key runs the action against the selected
project.

Projects are TOML files under `~/.config/revier/projects/`; the format, with a
worked example, is [docs/design/extending.md](docs/design/extending.md).

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
