# Agent probes

A probe tells revier what a coding agent is doing: running, idle, or waiting
for you. revier knows Claude Code and opencode itself. For any other agent,
write a probe: a small program that reads one panel as JSON and answers with
one state as JSON. No Go, no rebuild. This page is the contract.

## When to write a probe

Write one when an agent of yours shows as `unknown` in the TUI, or does not
show as an agent at all, and you want its state to sort the project list and
to wake `revier agent wait`. What the states mean to the TUI is in
[concepts.md](../concepts.md).

A probe sees only a panel of a runtime host, kitty or tmux. An agent that runs
in a window of its own, outside a terminal revier can read, has no panel to
probe.

For a project on another machine, the revier on the host reads the agent.
Declare the probe in the host's `config.toml`, and install the program there.
On the host the agent is a process with no terminal, so its panel has no
`title`: decide by `pid` or `command` there.

## Declare it

In `config.toml`:

```toml
[[probe]]
name = "aider"
exec = "~/bin/aider-probe"
```

| Key | Meaning |
|---|---|
| `name` | The agent's name, and the command the probe claims. See below. |
| `exec` | The program revier runs. It takes no arguments. |

Both keys are required. A `[[probe]]` without one of them stops revier, with
an error that names the table. revier reads `[[probe]]` when it starts:
restart the TUI after you add one.

## Which panels it reads

`name` is also the foreground command the probe claims. A probe named `aider`
reads the panels that run `aider`, and no others, so a pane of another agent
never costs it a process.

A panel runs `aider` when:

- the program of its command is named `aider`, with any directory before it:
  `/usr/bin/aider --model x` matches; or
- the program is `node`, `bun`, `deno` or `python…`, and its first argument
  that is not a flag is named `aider`: `python3 -m aider` and
  `node /opt/aider/bin/aider` both match.

Any other argument is data: `nvim aider` edits a file, and does not match.

The probes are tried in order: the compiled-in ones for Claude Code and
opencode first, then yours in `config.toml` order. The first that claims a
panel reads it. So a probe of yours named `claude` never runs: the compiled-in
one wins.

## What revier sends

revier writes one panel as JSON on the probe's stdin:

```json
{"id":"3","kind":"tool","title":"aider: refactoring the store","vars":{"revier_target":"agent"},"pid":41221,"command":["aider"],"tab":"2"}
```

| Field | Meaning |
|---|---|
| `id` | The panel's id in its runtime: a kitty window id, a tmux pane id. |
| `kind` | `shell` when the command is a shell, `tool` otherwise. A probe decides whether a panel is an agent; the runtime does not. |
| `title` | The panel's title as the program last set it. Many agents write their state here. |
| `vars` | The panel's variables, where the runtime has them. Optional. |
| `pid` | The process id of the foreground command. Optional. |
| `command` | The foreground command's argv. Optional. |
| `tab` | The tab that holds the panel: a kitty tab id, a tmux window id. Optional. |

## What the probe answers

The probe prints one state as JSON on stdout and exits 0:

```json
{"harness":"aider","status":"running","activity":"refactoring the store"}
```

| Field | Required | Meaning |
|---|---|---|
| `status` | yes | `unknown`, `idle`, `running` or `attention`. |
| `harness` | no | The agent's name. Default: the probe's `name`. |
| `activity` | no | One line about what the agent is on. The TUI shows it on the agent's row. |
| `dir` | no | The directory the agent works in, when it is not the project's own, for example a worktree. It tells two agents of one project apart. |
| `session` | no | The id of the conversation the agent holds. `revier events` records the agent by it. |

The statuses:

| `status` | Means |
|---|---|
| `running` | The agent is working. |
| `idle` | The agent has finished and waits for a new prompt. |
| `attention` | The agent waits for you: a question, a permission. A project with such an agent sorts first. |
| `unknown` | The probe cannot tell. |

## When a probe fails

- A probe that exits with a status other than 0, prints JSON that does not
  parse, or prints a `status` that is not one of the four, leaves the panel
  `unknown`. The other panels are not affected.
- A probe has half a second. A probe that has not ended by then is killed,
  with every process it started, and the panel is `unknown`.
- revier logs the failure, with what the probe wrote on stderr. See
  [troubleshooting.md](../troubleshooting.md).

revier runs the probe once per panel on every refresh. Keep it fast: a slow
probe is what makes the TUI feel slow.

## Where it runs

- `exec` is a bare name, such as `aider-probe`: revier looks it up on `PATH`.
- `exec` starts with `~/`: the path under your home directory.
- `exec` is another relative path, such as `bin/aider-probe`: revier reads it
  against your home directory, wherever revier runs.
- `exec` is an absolute path: as written.

The probe runs in revier's working directory: your home directory in the TUI,
and the directory you ran the command in for a command such as
`revier list`. Do not depend on it. To reach the agent's project, use the
panel's `pid` or `title`.

## What a probe cannot do

The compiled-in probes do more than a probe of yours can:

- `revier agent read` prints the last message of a Claude Code agent. For
  your agent it needs `--screen`, which reads the panel's text.
- A restored session resumes a Claude Code agent on its conversation. Your
  agent comes back starting fresh.
- For a project on another machine, the TUI reads the activity of a Claude
  Code agent from the title of the terminal here. Your agent's activity there
  is what your probe on the host answered, from a panel with no title.

## Example: a probe in sh

This probe reads the panel's title and decides by it. It needs `jq`. What
your agent writes in its title is for you to find out: change the patterns to
match it.

```sh
#!/bin/sh
# ~/bin/aider-probe: one panel arrives as JSON on stdin; one state goes out.
title=$(jq -r '.title')

case "$title" in
  *'?')      status=attention ;;  # the agent asks you something
  *working*) status=running ;;
  *)         status=idle ;;
esac

jq -n --arg status "$status" --arg activity "$title" \
  '{harness: "aider", status: $status, activity: $activity}'
```

Make it executable, then try it by hand before you declare it:

```
$ chmod +x ~/bin/aider-probe
$ echo '{"id":"3","kind":"tool","title":"apply this edit?","command":["aider"]}' | ~/bin/aider-probe
{
  "harness": "aider",
  "status": "attention",
  "activity": "apply this edit?"
}
```

Then add the `[[probe]]` table and restart the TUI.

## Quick reference

| | |
|---|---|
| Declare | `[[probe]]` in `config.toml`: `name`, `exec` (both required) |
| Claims | panels whose foreground command is `name` |
| Order | compiled-in probes first, then `config.toml` order; the first claim wins |
| In | one panel on stdin: `id`, `kind`, `title`, `vars`, `pid`, `command`, `tab` |
| Out | one state on stdout: `status` (required), `harness`, `activity`, `dir`, `session` |
| Statuses | `unknown`, `idle`, `running`, `attention` |
| Limit | 500 ms, then the probe and its children are killed |
| Failure | the panel is `unknown` |
