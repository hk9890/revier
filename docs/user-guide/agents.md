# Agents

An agent is a panel in a workspace that runs a coding agent, such as Claude
Code. This page shows how to drive an agent from a script, open more agent
tabs, resume an agent on its conversation, and let an agent configure revier.

## What revier sees

A probe reads each agent panel and reports one of four
[states](concepts.md#agents): `attention`, `running`, `idle` or `unknown`.
Claude Code reports every state, its conversation and its last answer; see
[Claude Code](integrations/claude-code.md). In [herdr](integrations/herdr.md),
any other agent herdr detects reports the state herdr reads, unless a probe
you declared reads it. Elsewhere opencode is always `unknown`; see
[opencode](integrations/opencode.md). For any other agent, write a probe:
[Agent probes](extending/probes.md).

## Drive an agent from a script

A script, or an agent that directs other agents, uses `revier agent`. These
commands do not raise a window or move the keyboard:

```bash
revier agent prompt shop 'run the tests and say what failed'
revier agent wait shop --until stopped --timeout 600
revier agent read shop                         # the agent's last answer
revier agent read shop --screen --lines 40     # what its panel shows
revier agent send-keys shop esc                # interrupt, or answer a dialog
```

The first argument is the agent's address: `shop` names the project's only
agent, and `shop:agent` the agent in the target `agent`. A tab that
`agent new` added has no target name: its address is `<project>:<number>`,
and `agent new --no-focus` prints it. When an address names more than one
agent, the error lists an address for each.

- `prompt` types one line and submits it. It refuses an agent in `attention`
  or `unknown`, because the Enter would answer a dialog. Use `send-keys` for
  those.
- `wait --until` takes `idle`, `running`, `attention`, or `stopped` (idle or
  attention: the turn stopped, the agent did not exit). A `--timeout` that
  runs out has its own [exit code](cli.md#exit-codes), so a script can tell a
  timeout from a failure.
- `read` needs a harness whose conversation revier can read, which today is
  Claude Code. `--screen` works for every harness.
- `send-keys` types keys whatever the agent does, so read the screen first.
  `revier agent --help` lists the key names.
- `revier agent focus shop` brings the agent to the front. Of the commands
  above, it is the only one that moves the keyboard.

For a project on another machine, see
[What does not work over a link](links.md#what-does-not-work-over-a-link).

## Open more agent and shell tabs

`revier agent new` adds a tab to an open workspace, with a copy of the
panels of the project's agent tab. It goes to the new tab, unless you give
`--no-focus`. `revier shell new` adds a tab with the shell alone.

```bash
revier agent new -p shop                # in the workspace of shop
revier agent new --dir ~/dev/shop-wt    # start the agent in a worktree
revier agent new -p shop --no-focus     # stay here; print the address, e.g. shop:7
revier shell new -p shop
```

Without `-p`, the project is the one `--dir` is in, else the one revier
finds as for every command ([Concepts](concepts.md#project)). The workspace
must be open already. To bind these commands to a kitty key, see
[kitty](integrations/kitty.md).

## Resume an agent

`revier session restore` and `revier agent new --resume <id>` start an agent on
a conversation. revier adds the harness's resume flag, for Claude Code
`--resume <id>`, to the end of the agent panel's `command`. Today only Claude
Code can resume.

When your `command` wraps the agent in a shell, pass the arguments on, or the
agent starts with no conversation ([Panels](configuration.md#panels)):

```toml
# Right: the resume flag reaches my-agent as "$@".
command = ["sh", "-lc", "exec my-agent \"$@\"", "sh"]

# Wrong: the resume flag goes to sh.
command = ["sh", "-lc", "my-agent"]
```

When the directory the agent worked in is gone, the agent starts fresh in the
project. When Claude Code no longer has the conversation, the agent starts
fresh in its directory: an agent that never got a prompt has none, and Claude
Code deletes old ones after `cleanupPeriodDays`, 30 days by default.

## Let an agent configure revier

`revier assist` hands this terminal to Claude Code, briefed to change revier's
configuration and to find out why revier does not do what you expect. Say what
you want. Leave with `/exit`.

```bash
revier assist             # start, or continue the last assist conversation
revier assist -p shop     # say which project "this project" is
```

The agent can read and write the configuration directory and runs `revier doctor` after
each edit. It starts even when your configuration does not load, and gets the
errors first. `/clear` in Claude Code starts a new conversation.

- `revier assist --print-brief` prints what the agent is told.
- `revier assist --reference` prints the configuration reference:
  [Configuration](configuration.md), [Status commands](extending/status.md) and
  [Agent probes](extending/probes.md).

`revier assist` needs `claude` on your `PATH`.

In the TUI, alt+a raises `revier-config-assistant`, a project revier builds
in: the same agent, in a workspace of its own beside the TUI. Its row has a
cog where other rows have a folder. It shows in the agent list, and a saved
session restores it. It is not told which project the cursor is on: name it.
What the TUI reads again after its edits is in [Files](configuration.md#files).
With no runtime host, alt+a hands the terminal over, as `revier assist` does.
