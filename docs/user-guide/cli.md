# Command line

Every `revier` command and flag, what each prints, the exit codes a script can
test, and the environment variables you can set. For what the words mean —
project, target, workspace, home — read [Concepts](concepts.md) first.

## How commands take arguments

`revier` with no command opens the TUI. Every other command does one thing and
exits.

- A flag may come before or after the other arguments:
  `revier go editor -p demo` and `revier go -p demo editor` are the same.
- A flag takes one dash or two: `--json` and `-json` are the same.
- `-p <name>` and `--project <name>` name the project to act on. Without it, a
  command acts on the project of the current directory, else the project of the
  focused window, else the project revier acted on last. When none of these
  gives a project, the command exits with status `3`.
- An agent is named by an address: `<project>` for the project's only agent,
  or `<project>:<target>` or `<project>:<panel>` for one of several.
- Put `--` before a text or a key that starts with a dash:
  `revier agent prompt demo -- "-v is the flag"`.
- `revier help`, `revier --help` and `revier -h` print the usage. So does
  `help` after a command family: `revier agent help`, `revier keys help`,
  `revier shell help`, `revier session help`, `revier each help`,
  `revier events help`.

## Projects

### revier list

```sh
revier list [--json [--conversations]] [<name>..]
```

Every project, with its state, its agent and its targets, printed once. With
names, only those projects; a name revier does not know is an error.

| Flag | Does |
|---|---|
| `--json` | Print the full survey as JSON. It includes the agents on a link's host that no terminal here shows, which the table leaves out. |
| `--conversations` | With `--json`, add the conversation each agent holds. |

The table has the columns `PROJECT`, `STATE`, `AGENT` and `TARGETS`. `STATE`
is `running`, `invalid`, `unreachable` (a link whose host did not answer) or
`-`. `AGENT` is the agent closest to needing you. Each target has a mark in
front of its name:

| Mark | Means |
|---|---|
| `*` | Running. |
| (space) | Not running. |
| `!` | Its own configuration refused it. |
| `x` | No host on this machine can open it. |
| `?` | Its host could not list, so revier does not know. |

A window you attached by hand shows as `(<title>)`.

### revier new

```sh
revier new [<name>]
```

Write a project file for the current directory, under the directory's name or
`<name>`. It prints the path of the file it wrote. It needs no terminal host.

### revier open

```sh
revier open [<name>] [--attach]
```

Run-or-raise the project's home workspace. A name revier does not know becomes
a new project for the current directory first, as `revier new <name>` writes
it. A project whose directory is missing is cloned from its `git_url` first.

| Flag | Does |
|---|---|
| `--attach` | End with this terminal on the workspace. Only tmux can do this: revier becomes `tmux attach`. |

### revier link

```sh
revier link [<host> [<project>]] [--name <name>]
```

With no argument, the hosts your `~/.ssh/config` names, without this machine.
With a host, the projects the revier on that host has. With a host and a
project, write a link to that project here.

| Flag | Does |
|---|---|
| `--name` | The link's name here. The project's own name by default. |

See [Projects on another machine](links.md).

### revier run

```sh
revier run <action> [-p <name>]
```

Run a configured `[[action]]` in the project. The action has this terminal, and
its exit status is revier's. For a link, the action runs on the host. See
[Projects](projects.md).

### revier each

```sh
revier each [--filter '<sh>'] [-n] -- <cmd> [<arg>..]
revier each log [<run> [<project>]]
```

Run one command in the directory of every project, one project at a time. The
command always follows `--`. Each project's output goes to a file under the
state directory, not to the terminal; the terminal shows one line per project.
A project whose directory is missing is skipped, and so is a link and a
project whose directory an earlier project has.

| Flag | Does |
|---|---|
| `--filter '<sh>'` | A shell test run in each directory. The command runs only where it exits 0. `'test -d .git'` selects the repositories. |
| `-n`, `--dry-run` | Print the selection and run nothing. |

`revier each log` lists past runs, newest first. `revier each log <run>` shows
how each project ended in that run. `revier each log <run> <project>` prints
what the command printed there.

## Targets

### revier go

```sh
revier go <target> [-p <name>] [--picker]
```

Run-or-raise a target of the project. Pressed again on the target, it returns
to the project's home. This is the command a desktop key runs. It prints the
project and the instance it landed on, or `launching` while it starts.

| Flag | Does |
|---|---|
| `--picker` | When no project resolves, open the popup instead of exiting with status `3`. |

### revier attach

```sh
revier attach [-p <name>]
```

Bind the focused window to the project. The project then holds it: it shows on
the project's row, and a shutdown of the project closes it. It needs a window
host.

## Agents and shells

The `revier agent` commands drive one agent from a script. None of `prompt`,
`wait`, `read` or `send-keys` raises a window or moves the focus. See
[Agents](agents.md) for the round trip.

### revier agent wait

```sh
revier agent wait <agent> --until <status> [--timeout <seconds>]
```

Block until the agent reaches a status, then print the status it ended on.

| Flag | Does |
|---|---|
| `--until` | `idle`, `running`, `attention`, or `stopped`. `stopped` is idle or attention. Required. |
| `--timeout` | Give up after this many seconds and exit with status `2`. `0`, the default, waits for good. |

### revier agent prompt

```sh
revier agent prompt <agent> [--] <text>
```

Type one line into the agent and submit it. It returns once an idle agent has
started on it. It refuses a panel that is not an agent's, an agent that waits
for an answer, where the Enter would pick an option in its dialog, and an agent
whose state is `unknown`, which may show such a dialog.

### revier agent read

```sh
revier agent read <agent> [--screen [--lines <n>]]
```

Print the last thing the agent said, where its harness keeps a conversation
revier can read. Claude Code does.

| Flag | Does |
|---|---|
| `--screen` | Print the text of the agent's panel instead. Works for every harness, and for a link's agent. |
| `--lines <n>` | With `--screen`, the last `<n>` lines of the panel and its scrollback. |

### revier agent send-keys

```sh
revier agent send-keys <agent> [--] <key>..
```

Type keys into the agent, in order, whatever it is doing. A key is `esc`,
`enter`, `tab`, `shift+tab`, `space`, `backspace`, `up`, `down`, `left`,
`right`, `ctrl+c`, or one character. A key lands on whatever the panel shows,
so read the screen first.

### revier agent new

```sh
revier agent new [-p <name>[:<target>] | --panel <id>] [--resume <id>] [--dir <path>] [--no-focus]
```

Add a tab to an open workspace: the project's agent panel and its shell. It
goes to the new agent and raises its window. Without `-p` or `--panel`, the
project is the one `--dir` is in, else the one the current directory resolves
to. It prints nothing on success, unless `--no-focus`.

| Flag | Does |
|---|---|
| `-p <name>[:<target>]` | The project, and optionally the target to open the tab in. |
| `--panel <id>` | The open workspace that holds this panel. In kitty, `@active-kitty-window-id` is the window a key was pressed in. Not with `-p`. |
| `--resume <id>` | Start the agent on this conversation. |
| `--dir <path>` | Start the agent and its shell here, not in the project. Dropped for a link. |
| `--no-focus` | Leave the focus where it is, and print the new agent's address. |

### revier agent focus

```sh
revier agent focus <agent> [--ref <instance>]
```

Make the agent's tab current and raise the window that holds it.

| Flag | Does |
|---|---|
| `--ref <instance>` | The instance that holds `<project>:<panel>`, as `revier list --json` reports it. A panel id is unique only within one kitty process. |

### revier agent exec

```sh
revier agent exec -p <name> [--tag <tag>] [--resume <id>] [--dir <path>]
```

Become the project's agent in this terminal. It is what the agent panel of a
link runs on the host over ssh; you do not run it by hand.

| Flag | Does |
|---|---|
| `-p <name>` | The project. Required. |
| `--tag <tag>` | The name the terminal that shows the agent knows it by. |
| `--resume <id>` | Start the agent on this conversation. |
| `--dir <path>` | Start the agent here. |

### revier shell new

```sh
revier shell new [-p <name>[:<target>] | --panel <id>] [--dir <path>]
```

Add a shell tab to an open workspace: the target's shell panel, or the
runtime's shell where the target declares none. The target is the home target
unless `-p` names one. It refuses a window that is no project's workspace. For a
link, the shell runs on the host.

| Flag | Does |
|---|---|
| `-p <name>[:<target>]` | The project, and optionally the target. |
| `--panel <id>` | The open workspace that holds this panel. Not with `-p`. |
| `--dir <path>` | Start the shell here. Dropped for a link. |

### revier shell exec

```sh
revier shell exec -p <name> [--tag <tag>] [--dir <path>]
```

Become the project's shell in this terminal: what the shell panel of a link
runs on the host. With no shell panel declared, it starts your login shell.

## The TUI and the popup

### revier

```sh
revier
```

The TUI: every project, its agent state and its targets. See [The TUI](tui.md).

### revier popup

```sh
revier popup
```

The TUI in a kitty window of its own, or the popup already open. It takes no
argument. It needs kitty, and GNOME with the Window Control extension and its
`wctl`; it names whichever is missing.

## Sessions

### revier session save

```sh
revier session save [--name <label>]
```

Record the projects that are open now, with their targets and the conversation
each agent holds. It prints the session's id and the file it wrote.

### revier session restore

```sh
revier session restore [<id>|<name>] [--dry-run]
```

Open what a saved session recorded: the newest one without an argument.

| Flag | Does |
|---|---|
| `--dry-run` | Print what it would open, and open nothing. |

### revier session list

```sh
revier session list [--json]
```

The saved sessions, newest first.

### revier shutdown

```sh
revier shutdown [<project>] [--agents | --targets] [--force] [--no-session-save] [--dry-run]
```

Save the session when it changed since the newest save, then close every open
target and attached window, and the agents in them. With a project, only that
project. It refuses, and saves and closes nothing, while an agent is working or
waiting for an answer.

| Flag | Does |
|---|---|
| `--agents` | Close only the agents, and keep the workspaces. |
| `--targets` | Close only what holds no agent. Not with `--agents`. |
| `--force` | Close busy agents too. |
| `--no-session-save` | Do not save the session first. |
| `--dry-run` | Print what it would close, and close nothing. |

See [Projects](projects.md) for sessions and shutdown.

## Keys

### revier keys status

```sh
revier keys status [--json]
```

The desktop keys revier wants, and who holds each one now. It only reads.
Each key is `active` (revier holds it), `stale` (revier holds it and runs
something else), `inert` (revier's entry is switched off), `taken` (another
shortcut holds it), `builtin` (the desktop holds it) or `free`. A machine with
no desktop prints an empty report and exits 0.

### revier keys install

```sh
revier keys install [--force] [-n] [--json]
```

Claim the keys. Without `--force`, it adds only the keys nothing else holds,
and reports the rest.

| Flag | Does |
|---|---|
| `--force` | Take a key something else holds. Another shortcut is switched off and keeps its command; a desktop default loses that key, and the line that restores it is printed. |
| `-n`, `--dry-run` | Print what would happen and change nothing. |
| `--json` | Print the plan as JSON. |

### revier keys uninstall

```sh
revier keys uninstall [-n] [--json]
```

Remove the shortcuts revier wrote, and nothing else. It takes `-n`,
`--dry-run` and `--json` as `install` does, and refuses `--force`.

## Diagnosis

### revier status

```sh
revier status [-p <name>]
```

Which project the current directory resolves to: its name, path, runtime host
and window host, and how many windows are attached to it.

### revier doctor

```sh
revier doctor
```

Every configuration file that did not load whole, each problem with the edit
that fixes it, and the links to this machine it skipped. It takes no
argument. It exits with status `1` when a file has a problem.

### revier assist

```sh
revier assist [-p <name>] [--print-brief | --reference]
```

Hand this terminal to Claude Code, briefed to change revier's configuration
and to find out what is wrong with it, on the conversation it held last. It
starts even when the configuration does not load.

| Flag | Does |
|---|---|
| `-p <name>` | Tell it which project you mean. |
| `--print-brief` | Print the brief, and start nothing. |
| `--reference` | Print the configuration format, and start nothing. Not with `--print-brief`. |

See [Claude Code](integrations/claude-code.md).

### revier events

```sh
revier events [--days <n>] [--local]
```

What revier did in each project, one JSON line per event, oldest first, with
what the revier on each linked host recorded. A host that does not answer is
named on stderr, and the events of the others are printed.

| Flag | Does |
|---|---|
| `--days <n>` | How far back to read, in days of 24 hours. Default 7. |
| `--local` | Only what this machine recorded. Ask no linked host. |

See [Projects](projects.md) for the event fields.

## Version

### revier version

```sh
revier version
```

The installed version, commit and build date. `revier --version` is the same.

## Output modes

Commands print plain text tables for a person. Five take `--json` for a
script: `revier list`, `revier session list`, `revier keys status`,
`revier keys install` and `revier keys uninstall`. Each prints one indented
JSON document. `revier events` always prints JSON, one object per line.

```sh
revier list --json demo
```

```json
[
  {
    "project": {
      "name": "demo",
      "path": "/home/user/dev/demo",
      "targets": []
    },
    "running": true,
    "path_exists": true,
    "home": { "host": "kitty", "id": "12", "title": "session:demo" },
    "targets": [
      { "name": "home", "key": "ctrl-shift-u", "host": "kitty",
        "ref": { "host": "kitty", "id": "12", "title": "session:demo" }, "available": true }
    ],
    "agents": [
      { "panel": "3", "ref": { "host": "kitty", "id": "12" },
        "state": { "harness": "claude", "status": "attention" } }
    ]
  }
]
```

The example is shortened: `project.targets` holds every target as the project
file declares it. An agent's `status` is `unknown`, `idle`, `running` or
`attention`.

```sh
revier events --days 7 | jq -r .project | sort | uniq -c | sort -rn
```

A warning goes to stderr as `revier: warning: ...`, and an error as
`revier: ...`, so stdout stays clean for a script. When a configuration file
has a problem, most commands print one warning line that says to run
`revier doctor`.

## Exit codes

| Code | Means |
|---|---|
| `0` | Success. |
| `1` | Failure: a usage error, an unknown project, a host that failed, or anything else. `revier doctor` also exits `1` when a file has a problem, after its report. |
| `2` | `revier agent wait` timed out. |
| `3` | No project resolved: none was named, the current directory is in none, the focused window belongs to none, and none is remembered. A normal outcome for a key pressed on a window no project claims. `revier go --picker` opens the popup instead. |
| `4` | `revier keys install` or `uninstall` ran, and some key is not as asked: it is held by something else (without `--force`), or a step failed. Each key's line already says why. |
| `5` | `revier each` ran, and the command failed in at least one project. The summary names each one. |
| `6` | `revier assist` found no coding agent: `claude` is not on `PATH`. |
| the action's | `revier run`: the action's own exit status, passed on as revier's. revier prints nothing more; the action has said why. |
| `128` + signal | The TUI ended by a signal: `129` when its terminal closed, `143` on a termination. ctrl+c does not end it. |

Three commands replace themselves with another program, and end with that
program's exit status: `revier open --attach` (tmux), `revier agent exec` (the
agent) and `revier shell exec` (the shell).

## Environment variables

You can set these:

| Variable | Does |
|---|---|
| `REVIER_CONFIG_HOME` | The configuration directory: `config.toml` and `projects/`. Wins over `XDG_CONFIG_HOME`. |
| `XDG_CONFIG_HOME` | Without `REVIER_CONFIG_HOME`, the configuration directory is `$XDG_CONFIG_HOME/revier`. Without either, `~/.config/revier`. |
| `REVIER_STATE_HOME` | The state directory: logs, events, sessions, `revier each` runs, and what revier remembers between commands. Wins over `XDG_STATE_HOME`. |
| `XDG_STATE_HOME` | Without `REVIER_STATE_HOME`, the state directory is `$XDG_STATE_HOME/revier`. Without either, `~/.local/state/revier`. |
| `XDG_RUNTIME_DIR` | Where the shared ssh connections to linked hosts keep their sockets, and the popup's kitty its control socket. Without it, every ssh call connects on its own. Your login session normally sets it. |
| `REVIER_SSH_CONFIG` | The ssh configuration file revier reads hosts from, for the link dialog and `revier link`, in place of `~/.ssh/config`. ssh itself still reads its own. |
| `NO_COLOR` | Any value that is not empty turns colour off in the TUI. Bold and reverse video stay. |
| `SHELL` | The shell `revier shell exec` starts when the project declares no shell command. `sh` when unset. |
| `CLAUDE_CONFIG_DIR` | Where revier reads Claude Code's files, as Claude Code does. `~/.claude` when unset. |

revier also reads variables that other tools set: `KITTY_LISTEN_ON` (the kitty
a command runs in), `TMUX` and `TMUX_PANE` (the tmux pane a command runs in),
and `XDG_CURRENT_DESKTOP` (the GNOME window host and `revier keys` refuse a
desktop that is not GNOME).

revier sets some variables itself; do not set them by hand. `REVIER_POPUP`
marks the TUI as the popup. `REVIER_TAG` and `REVIER_WORKSPACE` mark the agent
and shell of a link on its host. `REVIER_PROJECT`, `REVIER_PROJECT_PATH` and
`REVIER_PROJECT_HOST` tell a status command its project; see
[Statuses](extending/status.md).

## Quick reference

| Command | Does |
|---|---|
| `revier` | The TUI |
| `revier list [--json [--conversations]] [<name>..]` | Every project, printed once |
| `revier new [<name>]` | Write a project file for this directory |
| `revier open [<name>] [--attach]` | Run-or-raise a project's workspace |
| `revier link [<host> [<project>]] [--name <name>]` | Hosts, a host's projects, or a new link |
| `revier run <action> [-p <name>]` | Run an action in the project |
| `revier each [--filter '<sh>'] [-n] -- <cmd>` | Run a command in every project |
| `revier each log [<run> [<project>]]` | Past runs of `revier each` |
| `revier go <target> [-p <name>] [--picker]` | Run-or-raise a target |
| `revier attach [-p <name>]` | Bind the focused window to a project |
| `revier agent wait <agent> --until <status> [--timeout <s>]` | Wait for an agent |
| `revier agent prompt <agent> <text>` | Type and submit one line |
| `revier agent read <agent> [--screen [--lines <n>]]` | What the agent said, or shows |
| `revier agent send-keys <agent> <key>..` | Type keys |
| `revier agent new [-p <name>[:<target>] \| --panel <id>] [--resume <id>] [--dir <path>] [--no-focus]` | Add an agent tab |
| `revier agent focus <agent> [--ref <instance>]` | Go to an agent |
| `revier agent exec -p <name> [--tag <tag>] [--resume <id>] [--dir <path>]` | Become the agent (a link's panel) |
| `revier shell new [-p <name>[:<target>] \| --panel <id>] [--dir <path>]` | Add a shell tab |
| `revier shell exec -p <name> [--tag <tag>] [--dir <path>]` | Become the shell (a link's panel) |
| `revier popup` | The TUI in a kitty window |
| `revier session save [--name <label>]` | Record the open projects |
| `revier session restore [<id>\|<name>] [--dry-run]` | Open a saved session |
| `revier session list [--json]` | The saved sessions |
| `revier shutdown [<project>] [--agents \| --targets] [--force] [--no-session-save] [--dry-run]` | Close what is open |
| `revier keys status [--json]` | Who holds each desktop key |
| `revier keys install [--force] [-n] [--json]` | Claim the keys |
| `revier keys uninstall [-n] [--json]` | Release the keys |
| `revier status [-p <name>]` | Which project this directory resolves to |
| `revier doctor` | What does not load, and the fix |
| `revier assist [-p <name>] [--print-brief \| --reference]` | Claude Code, briefed on revier |
| `revier events [--days <n>] [--local]` | What revier did, as JSON lines |
| `revier version` | The installed version |
