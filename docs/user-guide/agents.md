# Agents

An agent is a panel in a workspace that runs a coding agent, such as Claude
Code. This page shows how to drive an agent from a script, open more agent
tabs, end an agent, resume an agent on its conversation, and let an agent
configure revier.

## What revier sees

A probe reads each agent panel and reports one of four
[states](concepts.md#agents): `attention`, `running`, `idle` or `unknown`.
Claude Code reports every state, its conversation and its last answer; see
[Claude Code](integrations/claude-code.md). opencode is always `unknown`; see
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

The first argument is the agent's address: `<project>:<target>:<panel>`,
for example `shop:home:7`. The agent list shows it in the pane, `revier list
--agents` prints it for each agent, and `agent new --no-focus` prints it. A
panel of a tab target has the tab's target in its address, also when the tab
sits inside the home: `shop:agent:8`, never `shop:home:8`. Two
short forms name the one agent there is: `shop`, the project's only agent, and
`shop:agent`, the only agent in the target `agent`. Beside
named agents, both name the default agent, the one that runs no named agent.
An agent in a window no target holds, such as a terminal you attached
or a link's tab you moved to another window, has no address: go to it from
the agent list.

- `prompt` types one line and submits it. It refuses an agent in `attention`
  or `unknown`, because the Enter would answer a dialog. Use `send-keys` for
  those. When the agent is still idle after the text arrived, it warns and
  exits [`7`](cli.md#exit-codes), so a `prompt && wait` chain stops there.
  When the agent exits instead, it says so and exits `1`.
- `wait --until` takes `idle`, `running`, `attention`, or `stopped` (idle or
  attention: the turn stopped, the agent did not exit). A `--timeout` that
  runs out has its own [exit code](cli.md#exit-codes), so a script can tell a
  timeout from a failure. On a panel address that is not an agent's yet, such
  as the one `agent new --no-focus` prints, `wait` first waits for an agent to
  start in the panel, within the same `--timeout`, so you can run it at once.
  Without `--timeout`, a panel where no agent starts holds the wait for good.
  With `--json`, `wait` also prints the id of the conversation the agent
  holds, such as `{"status": "idle", "session": "..."}`. It is the id when
  the wait ends: Claude Code starts another conversation on `/clear`. A
  harness that does not name its conversation leaves it empty.
- `read` needs a harness whose conversation revier can read, which today is
  Claude Code. `--screen` works for every harness.
- `send-keys` types keys whatever the agent does, so read the screen first.
  `revier agent --help` lists the key names.
- `revier agent focus shop` brings the agent to the front. Unlike the
  commands above, it moves the keyboard.

For a project on another machine, see
[What does not work over a link](links.md#what-does-not-work-over-a-link).

## Open more agent and shell tabs

`revier agent new` adds a tab to an open workspace, with the project's agent
panel and its first shell panel. It goes to the new tab, unless you give
`--no-focus`. `revier shell new` adds a tab with the shell alone.

```bash
revier agent new -p shop                       # in the workspace of shop
revier agent new dtop -p shop                  # the named agent dtop
revier agent new -p shop --dir ~/dev/shop-wt   # start the agent in a worktree
revier agent new -p shop --no-focus            # stay here; print the address, e.g. shop:home:7
revier shell new -p shop
```

Without a name, the tab runs the project's default agent: the first agent
panel of its layout. With a name, the tab's agent panel runs that
[named agent](configuration.md#named-agents), and the tab keeps the shell.

Without `-p` or `--panel`, the project is the one `--dir` is in, or without
`--dir` the one the current directory is in. A directory in no project is an
error. The workspace must be open already. With `--panel`, a window that is
no project's workspace gets a plain shell tab beside it, nothing if you set
[`outside`](configuration.md#terminal) to `none`, or with `agent` a tab of
the agent. To bind these commands to a
kitty key, see [kitty](integrations/kitty.md).

## End an agent

To end an agent, close it by its address:

```bash
revier agent close shop:home:7          # prints: closed the tab
```

`revier agent close` returns once the agent is gone. An agent in a tab
beside the workspace's own, such as one `agent new` opened, closes with its
whole tab, the shell beside it included. An agent in the workspace's own tab
closes as its panel alone, and the shell there stays. The command prints
which of the two it did. It refuses while an agent in what it closes, any
agent of that tab, is `running` or in `attention`, unless you give
`--force`. It saves no session.

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

A restored agent does one of three things:

- **It resumes its conversation**, when Claude Code still has the
  conversation and the directory the agent worked in is there.
- **It starts fresh in its directory**, when Claude Code no longer has the
  conversation: an agent that never got a prompt has none, and Claude Code
  deletes old ones after `cleanupPeriodDays`, 30 days by default.
- **It starts fresh in the project**, when the directory the agent worked in
  is gone.

A save by hand, at shutdown or at a project close notes an agent on this
machine that never got a prompt. Its restore starts it fresh and does not say
that its conversation is gone, or resumes it when it got its first prompt
after the save. A `last seen` save and an agent on a linked host carry no
such note.

Which agent a restore starts, and where:

- The first agents fill the target's agent panels, in order, and each runs
  that panel's command.
- A [named agent](configuration.md#named-agents) comes back as that agent.
  When its project no longer declares it, the restore skips it and says so.
- An agent past the panels with no name, such as `dtop` you started in a
  shell panel, comes back in a tab of its own as the agent that runs that
  program: the default agent, or else a named agent. When no agent of the
  project runs it, the restore skips it and says so.
- When no probe reads an agent's `command`, as with a `zsh -ilc` wrapper, the
  named agent whose name is the program runs it. When the default agent is
  such a wrapper, only that named agent takes its place, and otherwise the
  default agent comes back.

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
