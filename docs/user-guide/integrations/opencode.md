# opencode

This page is for you if you run opencode as your agent. revier recognises
opencode, but cannot tell what it is doing. This page says what works, what
does not, and why.

## What revier does with opencode

revier finds opencode in the panels of your workspaces and counts it as an
agent. Its state is always `unknown`. opencode shows nothing a panel can read:

- Its window title is `OpenCode`, set once at start. It does not change
  between idle and working.
- It sets no terminal variable.
- Its process does not change between idle and working. Its interface talks to
  a server on a private port, which a panel cannot see.

revier shows the agent as present, which is true, and not as idle or running,
which would be a guess.

## What you need

- `opencode` running in a kitty or tmux workspace. See [kitty](kitty.md) and
  [tmux](tmux.md).

There is nothing to install or set for it.

## How revier finds opencode

A panel is opencode when its foreground program is `opencode`. To make it the
agent of a workspace, put it in the agent panel's `command`:

```toml
[[target]]
name = "agent"
  [target.runtime]
  inside = "home"
    [[target.runtime.panels]]
    kind    = "agent"
    title   = "opencode"
    command = ["opencode"]
    [[target.runtime.panels]]
    kind = "shell"
```

## What works and what does not

| Works | Does not |
|---|---|
| The agent is counted, and listed as `unknown` | State: working, needs you, idle |
| `revier agent read --screen`: the text of its panel | `revier agent read` without `--screen`: revier reads no opencode conversation |
| `revier agent send-keys` | `revier agent prompt`: it refuses an agent whose state is `unknown`, because the Enter could answer a dialog. Drive it with `send-keys` |
| `revier agent new`, `revier agent focus`, the agent list | Resume: a restored opencode agent starts a new conversation |
| `revier session save` and `restore` open the agent again | `revier agent wait`: the state never leaves `unknown`, so the wait ends only at its timeout |

Because its state is `unknown`, an opencode agent never sorts first as one
that needs you. `revier shutdown` refuses only while an agent is working or
waits for an answer, so it closes an opencode agent even in the middle of a
turn.

## Give it a state with an external probe

An external probe is a program you write that reads one panel and answers
with its state. [Agent probes](../extending/probes.md) has the contract.

revier asks its built-in opencode probe first, and a built-in probe wins for
the program it knows. A `[[probe]]` named `opencode` is therefore never asked:
today an opencode agent has no state in revier.

## Troubleshooting

**An opencode agent shows `unknown`.** That is its state in revier. Read its
panel with `revier agent read --screen`, or move the cursor to it in the TUI
to see its terminal.

**`revier agent read` says the probe reads no conversation.** Use
`--screen`.

**My `[[probe]]` for opencode is never run.** The built-in opencode probe
claims the panel first. See
[Give it a state with an external probe](#give-it-a-state-with-an-external-probe).

For anything else, start with `revier doctor` and
[Troubleshooting](../troubleshooting.md).
