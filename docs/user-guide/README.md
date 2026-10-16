# revier user guide

revier is a project-grouped control surface for running coding agents. It
shows what every agent across every project is doing, and reaches the one
that needs you in one keystroke.

Start at [Getting started](getting-started.md), then read
[Concepts](concepts.md) once. The rest you read when you need it.

## Find it by task

| I want to… | Read |
|---|---|
| Install revier, make my first project and open it | [Getting started](getting-started.md) |
| Understand projects, targets, home, toggle-back and agent states | [Concepts](concepts.md) |
| Find my way around the TUI and the popup, or look up a key | [The TUI](tui.md) |
| Add a project, share a target, run an action, run a command in every project, save a session | [Projects](projects.md) |
| Drive an agent from a script, add agent and shell tabs, resume a conversation | [Agents](agents.md) |
| Work on a project that lives on another machine | [Projects on another machine](links.md) |
| Look up a field of `config.toml` or a project file | [Configuration](configuration.md) |
| Look up a command, a flag, an exit code or an environment variable | [Command line](cli.md) |
| Work out why something does not work | [Troubleshooting](troubleshooting.md) |

## Extending revier

| I want to… | Read |
|---|---|
| Show a command's output in the TUI: usage limits, CI, mail | [Statuses](extending/status.md) |
| Give revier the state of an agent it does not know | [Agent probes](extending/probes.md) |

## Integrations

How to set revier up with the tools you already use, what works with each,
and what does not.

| Tool | Read |
|---|---|
| kitty, the terminal revier runs workspaces and the popup in | [kitty](integrations/kitty.md) |
| tmux, for workspaces with no display or over ssh | [tmux](integrations/tmux.md) |
| Ghostty, which revier has no host for: what still works | [Ghostty](integrations/ghostty.md) |
| GNOME, for window targets and the desktop keys | [GNOME](integrations/gnome.md) |
| Claude Code, the agent revier reads the most from | [Claude Code](integrations/claude-code.md) |
| opencode, which revier recognises but cannot read | [opencode](integrations/opencode.md) |
