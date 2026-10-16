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
command in the [command line reference](docs/user-guide/cli.md), and the GNOME
keybindings.

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

`revier version` prints what is installed. To install from source instead,
see [Building](#building).

To verify an archive against the release's signed checksums, see
[Getting started](docs/user-guide/getting-started.md).

## First use

```bash
cd ~/dev/demo
revier new                     # write a project file for this directory
revier open                    # open its workspace: an agent beside a shell
revier keys install --dry-run  # see which desktop keys revier would claim
revier keys install            # claim the ones nothing else holds
revier                         # every project, its agent state, its targets
```

## User guide

The [user guide](docs/user-guide/README.md) covers the rest: getting started,
the concepts, the TUI and every key, projects, agents, projects on another
machine, the configuration reference, every command, troubleshooting,
statuses and agent probes, and how to set revier up with kitty, tmux, Ghostty,
GNOME, Claude Code and opencode.

## Building

Go 1.27, pinned with [mise](https://mise.jdx.dev).

```bash
mise install
mise run build     # ./bin/revier
```

Installing revier is [Install](#install) above.

Contributor guides live in [docs/](docs/): [CODING.md](docs/CODING.md),
[TESTING.md](docs/TESTING.md), [RUNNING.md](docs/RUNNING.md).

## License

[MIT](LICENSE).
