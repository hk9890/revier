# Claude Code

This page is for you if you run Claude Code as your agent. It tells you what
revier reads from Claude Code, what you need to install, and what works and
what does not.

## What revier does with Claude Code

revier finds Claude Code in the panels of your workspaces and shows what each
one is doing: working, waiting for you, or idle. It names the conversation
each one holds, so a saved session brings it back on that conversation. It
reads the last thing each one said. And `revier assist` hands your terminal to
Claude Code, briefed to configure revier for you.

revier only reads. It installs no hook, writes no Claude Code setting, and
needs no plugin.

## What you need

- Claude Code 2.1.212 or newer, as `claude` on your PATH. revier asks it
  `claude agents --json`, which older versions do not have.
- The agent running in a kitty or tmux workspace. See [kitty](kitty.md) and
  [tmux](tmux.md).

revier reads Claude Code's files from `$CLAUDE_CONFIG_DIR`, or from
`~/.claude` when that is not set.

## How revier finds Claude Code

A panel is Claude Code when its foreground program is `claude` or
`claude-code`, also when it runs as a script under `node`, `bun` or `deno`.
An argument does not count: `nvim claude` is an editor.

`revier new` writes a workspace whose `agent` tab runs `claude`. To run it
with flags of your own, change the panel's `command`:

```toml
    [[target.runtime.panels]]
    kind    = "agent"
    title   = "claude"
    command = ["claude", "--model", "opus"]
```

## The states it reports

revier asks `claude agents --json` for every live session and its process,
and matches a panel to a session by the process id. Claude Code's own word
decides the state:

| Claude Code says | revier shows | The TUI calls it |
|---|---|---|
| `busy` | `running` | working |
| `waiting` | `attention` | needs you |
| `idle` | `idle` | idle |
| anything else | `unknown` | unknown |

A Claude Code panel with no session in the listing is `unknown`: Claude Code
runs there, but revier cannot see what it does.

The row's activity line is the summary Claude Code writes into its window
title, without the state glyph in front.

revier runs the listing again only when a file in Claude Code's `sessions`
directory changed, or when the last answer is 10 seconds old. A state change
therefore shows within a second.

## Conversations

Each agent's row knows the conversation it holds. To print them:

```bash
revier list --json --conversations
```

`--conversations` works only with `--json`. Each agent then carries the id of
its conversation and the directory Claude Code works in. That directory is
where Claude Code is now: an agent that moved into a worktree is listed
there.

## Resume

`revier session save` records the conversation of each agent, and
`revier session restore` starts each one again with `--resume <id>`, in the
directory it worked in. `revier shutdown` saves first. [Projects](../projects.md)
covers sessions.

revier adds `--resume <id>` to the agent panel's `command` as it reads now, so
a flag you added stays. It first removes `--continue`, `-c`, `--resume`, `-r`
and `--session-id`, which would pick another conversation.

A command that wraps `claude` must pass its arguments on, or the resume is
lost:

```toml
command = ["sh", "-lc", "exec claude \"$@\"", "sh"]   # passes --resume on
command = ["sh", "-lc", "claude"]                      # drops it
```

An agent whose worktree was removed since the save starts fresh in the
project.

## Read what an agent said

```bash
revier agent read demo
revier agent read demo --screen --lines 40
```

Without `--screen`, revier prints the last thing the agent said, read from the
end of its conversation file under `~/.claude/projects/`. That file's format
is Claude Code's own and changes between releases, so revier shows it as it
can and never acts on it. `--screen` prints the text of the panel and works
for every agent. [Agents](../agents.md) has every `revier agent` command.

## revier assist

`revier assist`, or `alt+a` in the TUI, hands your terminal to Claude Code,
briefed to change revier's configuration and to find out why revier does not
do what you expect.

```bash
revier assist
revier assist -p demo
```

- Claude Code starts in a directory of its own,
  `~/.local/state/revier/assist`, and may read and write
  `~/.config/revier`.
- It continues the conversation you held with it last. `/clear` there starts
  a new one.
- `-p` tells it which project you mean. The brief also names what does not
  load, in that project and in `config.toml`.
- It starts even when the configuration does not load, so it can repair it.
- `/exit`, or `ctrl+c` twice, returns you. From the TUI, revier reads the
  files again when Claude Code exits.

Two flags print and start nothing:

```bash
revier assist --print-brief   # what Claude Code is told
revier assist --reference     # the configuration format, as this revier reads it
```

`revier assist` exits with status 6 when `claude` is not on your PATH.

## What works and what does not

| Works | Does not |
|---|---|
| State, from Claude Code's own listing | State for a session the listing does not hold |
| The conversation of each agent, and resume on restore | Resume where a wrapper command drops its arguments |
| `revier agent read` from the conversation file | Reading an agent of a linked project without `--screen`: its conversation is on the other machine |
| `revier assist` | `revier assist` with an agent other than Claude Code |

## Troubleshooting

**Every agent shows `unknown`.** Check that `claude agents --json` works in
your shell. A Claude Code older than 2.1.212 does not have it. revier's log
names the error.

**One agent shows `unknown`, the others work.** Under tmux, revier matches a
pane by the pane's own process. When you typed `claude` into a shell pane,
that process is the shell. Start the agent from the project file, or with
`revier agent new`, so Claude Code is the pane's own process. Such an agent
is also restored fresh, with no conversation.

**A restored agent starts a new conversation.** The panel's `command` is a
wrapper that drops its arguments. Pass `"$@"` on, as in [Resume](#resume).

**`revier agent read` prints nothing.** The conversation has no message yet,
or this Claude Code writes its file in a form revier cannot read. Use
`--screen`.

**`revier assist` exits with status 6.** Install Claude Code, so that
`claude` is on your PATH.

For anything else, start with `revier doctor` and
[Troubleshooting](../troubleshooting.md).
