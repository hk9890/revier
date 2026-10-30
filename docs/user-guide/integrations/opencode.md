# opencode

revier finds opencode in your workspace panels and counts it as an agent, but
outside herdr its state is `unknown`. opencode shows nothing a panel can read:
its title, its variables and its process stay the same when it works and when
it is idle. revier shows it as present rather than guess.

## What you need

- `opencode` in a workspace. Nothing to install or set.

To make it a workspace's agent, put it in the agent panel's `command` (see
[Panels](../configuration.md#panels)):

```toml
    [[target.runtime.panels]]
    kind    = "agent"
    title   = "opencode"
    command = ["opencode"]
```

## What works

- The agent list, `revier agent new`, `revier agent focus`.
- `revier agent read --screen` and `revier agent send-keys`.
- Session save and restore open the agent again, with a new conversation.

## What does not work

- `revier agent read` without `--screen`
  ([Agents](../agents.md#drive-an-agent-from-a-script)).

Outside herdr:

- State: working, needs you, idle. An opencode agent never sorts first as one
  that needs you.
- `revier agent prompt`: it refuses an agent in state `unknown`
  ([Agents](../agents.md#drive-an-agent-from-a-script)). Use `send-keys`.
- `revier agent wait`: it ends only at its timeout.
- `revier shutdown` does not wait for an opencode turn to end.
- `revier agent close` does not refuse an opencode agent in a turn: it shows
  `unknown` there, and only `running` and `attention` count as busy.
