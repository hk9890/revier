# Agents

This page is about the coding agents that run in your projects: what revier
can see of them, how to drive one from a script or from another agent, how to
open more agent and shell tabs, how an agent comes back on its conversation,
and how `revier assist` hands your terminal to an agent that configures
revier.

## What revier sees of an agent

An agent is a panel in a workspace that a probe recognises. A probe reads the
panel and reports the agent's state:

| State | Means |
|---|---|
| `attention` | The agent waits for you: a question, or a permission dialog. |
| `running` | The agent is working on a turn. |
| `idle` | The agent is at rest, waiting for a prompt. |
| `unknown` | The probe recognises the agent, but cannot say what it does. |

revier has these probes:

- **Claude Code** reports every state, the conversation the agent holds, and
  the last thing it said. It needs Claude Code 2.1.212 or newer, and nothing
  else installed. See [Claude Code](integrations/claude-code.md).
- **opencode** is recognised, and its state is always `unknown`: opencode
  shows nothing a panel can read. See [opencode](integrations/opencode.md).
- **An external probe** is a program you write for any other harness, and
  declare in `config.toml`. See [Agent probes](extending/probes.md).

The built-in probes are asked first, so an external probe cannot take over a
harness revier already knows.

A panel whose foreground program is a shell is never an agent, even when a
probe claims it. Text typed there would run as a command.

## Address an agent

Every `revier agent` command except `new` and `exec` takes an address:

| Address | Names |
|---|---|
| `<project>` | The project's only agent. |
| `<project>:<target>` | The only agent in that target, such as `shop:agent`. |
| `<project>:<panel>` | The agent in that panel, by the panel id the runtime gives it. |

When an address names more than one agent, the command fails and lists the
addresses that name one each. A panel id is unique only inside one kitty
process, so where two kitty windows both hold a panel `1`, the list gives the
target form instead.

## Drive an agent from a script

A script, or one agent that directs the others, drives an agent with
`revier agent`. No command of this round trip raises a window or moves the
keyboard:

```bash
revier agent prompt demo 'run the tests and say what failed'
revier agent wait demo --until stopped --timeout 600    # idle, or attention
revier agent read demo                                  # the answer
revier agent read demo --screen --lines 40              # what its panel shows
revier agent send-keys demo esc                         # interrupt, or answer a dialog
worker=$(revier agent new -p demo --no-focus)           # another agent: demo:<panel>
```

### Prompt an agent

`revier agent prompt <agent> <text>` types one line into the agent and
submits it. It returns once an idle agent has started on the prompt, so a
`wait` after it waits for that turn, not for the rest before it:

```bash
revier agent prompt demo "fix the lint errors" && revier agent wait demo --until stopped
```

An agent that is already working queues the prompt, and `prompt` returns at
once. When the agent is still idle after the prompt arrived, `prompt` warns
on stderr: check the panel before you wait on it.

`prompt` refuses:

- an agent in `attention`. Its dialog would take the Enter as an answer.
  Answer it with `send-keys`, or in the panel;
- an agent in `unknown`, for the same reason. An opencode agent is always
  `unknown`, so drive it with `send-keys`;
- text with a newline. A newline submits the text early and sends the rest
  as more prompts;
- a panel that is not an agent's.

Put `--` before text that starts with `-`: `revier agent prompt demo -- '-v'`.

### Wait for a state

`revier agent wait <agent> --until <state>` blocks until the agent reaches
the state, and prints the state it ended on. `<state>` is one of:

| `--until` | Ends when the agent is |
|---|---|
| `idle` | idle |
| `running` | running |
| `attention` | waiting for you |
| `stopped` | idle or waiting for you: no longer working |

`--timeout <seconds>` gives up after that time and exits 2, so a script can
tell a timeout from a failure. Without `--timeout`, or with 0, it waits for
good. A wait whose agent panel closes fails.

### Read what an agent said

`revier agent read <agent>` prints the last thing the agent said. It works
where the harness keeps a conversation revier can read, which Claude Code
does. Where it does not, the command fails and says so.

`--screen` prints the text of the agent's panel instead, which works for
every harness. `--lines <n>` prints the last `n` lines of the panel and its
scrollback. Both print plain text: colours and terminal control codes are
taken out.

### Type keys

`revier agent send-keys <agent> <key>..` types keys into the agent in order,
whatever the agent is doing. It is how you answer a dialog and how you
interrupt a turn. A key is one character, or one of:

```
esc  enter  tab  shift+tab  space  backspace  up  down  left  right  ctrl+c
```

A key lands on whatever the panel shows, so read the screen first. A name
that is no key refuses the whole command, so nothing is typed up to the
mistake.

### Bring an agent to the front

`revier agent focus <agent>` makes the agent's tab current and raises the
window that holds it. This one does move the keyboard.

### Time limits and exit statuses

`wait` runs as long as `--timeout` says. `prompt`, `read`, `send-keys`,
`focus` and `new` give up after about a minute, so a runtime that hangs does not hold a script
for good.

| Exit status | Means |
|---|---|
| 0 | Done. |
| 1 | The command failed. stderr says why. |
| 2 | `wait` ran out of time. |

### Agents on another machine

For a project on another machine, `prompt`, `wait`, `send-keys` and `new`
work as they do here. `read` needs `--screen`, because the conversation is
on the host. See [Projects on another machine](links.md).

## Add agent and shell tabs

`revier agent new` opens one more agent in a workspace that is open: a tab
with the project's agent panel and its shell panel split into it. `revier
shell new` opens a tab with the shell panel alone, or the runtime's own shell
where the project declares none. In kitty a tab is a tab of the OS window; in
tmux a workspace is a session, and the tab is a window of it.

```bash
revier agent new                       # the project this directory resolves to
revier agent new -p shop               # the project shop
revier agent new -p shop:home          # name the workspace when several declare an agent
revier agent new --dir ~/dev/shop-wt   # start the agent and its shell in a worktree
revier agent new --resume <id>         # start on that conversation
revier shell new -p shop
```

Which workspace the tab opens in:

- `--panel <id>`: the workspace that holds that panel. A kitty key passes the
  window it was pressed in this way.
- `-p <project>`: that project's workspace. `-p <project>:<target>` names
  the target.
- Else the project that `--dir` is in, else the project this directory
  resolves to.

The workspace must be open; the command does not open it. A `--dir` outside
the project that holds `--panel` is dropped, and the tab opens where the
project's own tab would.

`revier agent new` goes to the new agent and raises its window. With
`--no-focus` it does neither, and prints the address a script reaches the new
agent by, such as `demo:7`. The address names an agent once the harness in
the tab has started.

`--resume <id>` starts the agent on that conversation. When it cannot, the
agent starts empty and `revier agent new` warns on stderr.

For a project on another machine, the tab's agent or shell runs on the host,
and `--dir`, a path on this machine, is dropped.

### Bind the tabs to kitty keys

A kitty key can pass the window it was pressed in, and revier finds the
workspace from it. `--copy-env` tells revier which kitty process the window
id belongs to, since every kitty counts its windows from 1. In `kitty.conf`:

```
map ctrl+shift+z launch --type=background --copy-env --cwd=current sh -lc 'exec revier agent new --panel "$1" --dir .' sh @active-kitty-window-id
```

`revier shell new` refuses a window that is no project's workspace, so a key
that should open a shell everywhere falls back to kitty's own tab:

```
map ctrl+shift+t launch --type=background --copy-env --cwd=current sh -lc 'revier shell new --panel "$1" --dir . || kitten @ launch --type=tab --cwd=current' sh @active-kitty-window-id
```

More on kitty is in [kitty](integrations/kitty.md).

## Become the agent or the shell in this terminal

`revier agent exec -p <project>` and `revier shell exec -p <project>` replace
the current process with the project's agent or shell, in this terminal. The
command comes from the project file: the agent panel's `command`, or the
shell panel's, or your login shell where the project declares no shell.

```bash
revier agent exec -p shop [--tag <tag>] [--resume <id>] [--dir <path>]
revier shell exec -p shop [--tag <tag>] [--dir <path>]
```

This is what the panels of a link run on the host over ssh, so the host's
project file says which agent runs and where. `--tag` is the name the
terminal on the other machine knows the panel by. `agent exec` clones the
project's directory first when it is missing and the file has a `git_url`.
You rarely type these yourself.

## Resume an agent on its conversation

An agent that `revier session restore` brings back, or that `revier agent new
--resume <id>` opens, starts on a conversation. revier adds the harness's
resume flag to the end of the agent panel's `command`. For Claude Code this
is `--resume <id>`. A flag in your command that picks a conversation itself,
such as `--continue`, is taken out first; every other flag, such as a model
flag, stays.

So a command that wraps the agent must pass its arguments on to it:

```toml
# Right: the resume flag reaches my-agent as "$@".
command = ["sh", "-lc", "exec my-agent \"$@\"", "sh"]

# Wrong: the resume flag goes to sh, and my-agent starts empty.
command = ["sh", "-lc", "my-agent"]
```

The agent starts empty instead of resumed when:

- no conversation was recorded for it;
- no probe here can resume its harness. Today only Claude Code can;
- the panel runs another harness than the one that held the conversation;
- the directory it worked in is gone. It then starts in the project, so the
  conversation does not carry on in the wrong checkout;
- it ran in a tab that runs its own `launch`, which need not be the agent.

[Projects](projects.md) says what a session records and restores.

## Let an agent configure revier

`revier assist` hands this terminal to Claude Code, briefed to change revier's
configuration and to find out why revier does not do what you expect. Tell it
what you want. When you leave it, with `/exit`, the terminal is yours again.

```bash
revier assist             # start, or continue the last assist conversation
revier assist -p shop     # tell it which project "this project" means
revier assist --print-brief
revier assist --reference
```

What it does:

- It starts Claude Code in a directory of its own,
  `~/.local/state/revier/assist/`, and lets it read and write your
  configuration directory, `~/.config/revier/`.
- It continues the conversation you held with it last. `/clear` in Claude
  Code starts a new one.
- The brief tells it where the configuration, the project files and the logs
  are, to run `revier doctor` after each edit, and to run nothing that acts
  on your desktop, projects or agents unless you ask. It also lists what does
  not load in `config.toml` and, with `-p`, in that project.
- It starts even when the configuration does not load, and tells the agent
  what is wrong with it first.

`--print-brief` prints what the agent is told, and starts nothing.
`--reference` prints the configuration format this version reads: the pages
[Configuration](configuration.md), [Status commands](extending/status.md) and
[Agent probes](extending/probes.md). Neither starts anything.

`revier assist` exits 6 when no `claude` is on your `PATH`, so a script can
tell a machine with no agent from an agent that failed.

In the TUI, alt+a does the same, on the project the cursor is on. When you
leave the agent, the TUI is back with the files read again. See
[The TUI](tui.md).

## Quick reference

| Command | Does |
|---|---|
| `revier agent prompt <agent> [--] <text>` | Type one line into an idle agent and submit it. |
| `revier agent wait <agent> --until <state> [--timeout s]` | Block until `idle`, `running`, `attention` or `stopped`. Exits 2 on timeout. |
| `revier agent read <agent> [--screen [--lines n]]` | Print what the agent said last, or what its panel shows. |
| `revier agent send-keys <agent> [--] <key>..` | Type keys, whatever the agent is doing. |
| `revier agent focus <agent>` | Make the agent's tab current and raise its window. |
| `revier agent new [-p project[:target] \| --panel id] [--resume id] [--dir path] [--no-focus]` | Add an agent tab to an open workspace. |
| `revier shell new [-p project[:target] \| --panel id] [--dir path]` | Add a shell tab to an open workspace. |
| `revier agent exec -p project [--tag t] [--resume id] [--dir path]` | Become the project's agent in this terminal. |
| `revier shell exec -p project [--tag t] [--dir path]` | Become the project's shell in this terminal. |
| `revier assist [-p name] [--print-brief \| --reference]` | Hand this terminal to Claude Code to configure revier. Exits 6 with no `claude`. |

`<agent>` is `<project>`, `<project>:<target>` or `<project>:<panel>`.
