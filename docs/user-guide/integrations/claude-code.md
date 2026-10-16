# Claude Code

revier finds Claude Code in your workspace panels and shows what each one
does: working, waiting for you, or idle. It records each one's conversation,
so a restored session resumes it. revier only reads: it installs no
hook and no plugin.

## What you need

- Claude Code 2.1.212 or newer, as `claude` on your PATH. revier asks it
  `claude agents --json` for the state of every session.
- The agent in a kitty or tmux workspace. `revier new` writes one.

revier reads Claude Code's files from `$CLAUDE_CONFIG_DIR`, else `~/.claude`.

## Run it with your own flags

Change the agent panel's `command` (see
[Panels](../configuration.md#panels)):

```toml
    [[target.runtime.panels]]
    kind    = "agent"
    command = ["claude", "--model", "opus"]
```

On restore, revier adds `--resume <id>` to this command and keeps your flags.
A wrapper command must pass its arguments on, as [Panels](../configuration.md#panels)
shows, or the agent starts a new conversation.

## Read what it said

```bash
revier agent read demo            # the last message, from its conversation
revier agent read demo --screen   # the text of its panel
```

## More

- `revier assist`, and alt+a in the TUI, start Claude Code in `assist/` in
  the state directory, briefed to change your configuration; see
  [Agents](../agents.md#let-an-agent-configure-revier) and
  [Environment variables](../cli.md#environment-variables).
- To show your usage limits in the TUI, see
  [Statuses](../extending/status.md#example-claude-code-usage-limits).

## What does not work

- `revier agent read` without `--screen` on a linked project: the
  conversation is on the other machine.
- `revier assist` with an agent other than Claude Code.

**Every agent shows `unknown`.** Run `claude agents --json` in your shell.
An older Claude Code does not have it.
