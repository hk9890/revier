# Agent probes

A probe tells revier the state of a coding agent that revier does not know.
revier knows Claude Code, opencode, and in herdr every agent herdr
detects. For another agent, write a small
program: it reads one panel as JSON on stdin and prints one state as JSON.
On this machine, a probe reads the runtime's panels. For a link,
declare and install the probe on the host. There it reads the process that
`revier agent exec` started for the link's agent panel. That panel has no
`title`, `tab` or `dir`, so use `pid` or `command`.

## Declare a probe

Only in `config.toml`; in a project file revier ignores it. Restart the TUI
after you add one ([Files](../configuration.md#files)), and run
`revier doctor`: a `[[probe]]` without `name` or `exec` is not run, and
`revier doctor` names it.

```toml
[[probe]]
name = "aider"
exec = "~/bin/aider-probe"
```

| Key | Default | Meaning |
|---|---|---|
| `name` | required | The agent's name, and the program the probe claims. |
| `exec` | required | The program revier runs, with no arguments. A bare name is looked up on `PATH`; `~/` is your home; another relative path is read against your home. |

A probe named `aider` reads the panels whose program is `aider` (with any
directory), or `node`, `bun`, `deno` or `python…` with `aider` as the first
argument that is not a flag. The Claude Code probe goes first, then yours in
file order, then the others.

## Input and output

```json
{"id":"3","kind":"tool","title":"aider: refactoring the store","vars":{"revier_target":"agent"},"pid":41221,"command":["aider"],"tab":"2"}
```

`id` is the panel (kitty window id, tmux pane id) and `tab` its tab. `kind` is
`shell` when the command is a shell, else `tool`. `title` is the panel's
title; many agents write their state there. `vars`, `pid`, `command`, `tab`
and `dir` are optional. `pid` and `command` are the panel's foreground process.
`dir` is the panel's working directory.

Print one JSON object and exit 0 within 500 ms:

| Field | Required | Meaning |
|---|---|---|
| `status` | yes | `running` (working), `idle` (waits for a prompt), `attention` (waits for you), `unknown`. |
| `harness` | no | The agent's name. Default: the probe's `name`. |
| `activity` | no | One line about the work, shown on the agent's row. |
| `dir` | no | The agent's directory when it is not the project's, for example a worktree. |
| `session` | no | The conversation id. `revier events` records the agent by it. |
| `name` | no | What the harness calls the agent; it need not be unique. The agent's row and the agent list's pane show it. |

A non-zero exit, bad JSON, an unknown `status` or a run over 500 ms makes the
panel `unknown`; the log has the reason and the probe's stderr. revier runs
the probe for each panel on each refresh, so keep it fast. What a probed agent
cannot do, such as resume, is in [Agents](../agents.md).

## Example

```sh
#!/bin/sh
# ~/bin/aider-probe
title=$(jq -r '.title // ""')

case "$title" in
  *'?')      status=attention ;;
  *working*) status=running ;;
  *)         status=idle ;;
esac

jq -cn --arg status "$status" --arg activity "$title" \
  '{harness: "aider", status: $status, activity: $activity}'
```

Test it by hand:

```
$ chmod +x ~/bin/aider-probe
$ echo '{"id":"3","kind":"tool","title":"apply this edit?"}' | ~/bin/aider-probe
{"harness":"aider","status":"attention","activity":"apply this edit?"}
```
