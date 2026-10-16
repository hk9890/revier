# Troubleshooting

When revier does not do what you expect, start with `revier doctor`, then find
the symptom below. The last sections say where revier writes its log and
its events, and how to read them.

## Start here

### revier doctor

```
revier doctor
```

`revier doctor` reads `config.toml` and every project file, and prints each
file that did not load whole. Under each file it prints one line per problem:
what the problem is about (the project, a target, an action, a status), the
reason, and for the common problems a `fix:` line with the edit that answers
it.

```

/home/you/.config/revier/projects/demo.toml
  project no target is marked home; nothing to open or return to
          fix: mark the workspace target `home = true`

12 projects, 1 file with problems
```

The last line counts the projects and the files with problems. When project
files link to this machine itself, it then lists them under `skipped N links
to this machine`. That is not a problem: the files are shared between
machines, and on the other machine the link is right.

`revier doctor` exits 0 when every file loads whole, and 1 when one does not,
so a script can check the configuration.

A mistake in a file costs only what is wrong. A bad target shows `invalid` and
its key says why when pressed. A project file that does not load is still
listed, with the reason in the pane. Every other project, the TUI and the
desktop keys keep working. Most commands warn once on stderr when something
did not load:

```
revier: warning: 1 configuration file with problems; run `revier doctor`
```

### revier assist

```
revier assist
```

`revier assist` hands the terminal to Claude Code, briefed to change the
configuration and to find what is wrong with it. It knows what does not load,
and it runs `revier doctor` after each edit. With `-p <name>` it knows which
project you mean. It continues the conversation you held with it last. In the
TUI, alt+a does the same.

`revier assist` starts even when the configuration does not load at all, so it
is the command to use when nothing else runs. It exits 6 when `claude` is not
on your PATH. Refer to [agents.md](agents.md).

## Every command fails with config.toml in the message

A few mistakes in `config.toml` stop every command, `revier doctor` included.
[What is refused, and how you find out](configuration.md#what-is-refused-and-how-you-find-out)
lists them.

The message names the file and the mistake. Fix the file, or run
`revier assist`, which still starts. Every other mistake costs only the table
it is in.

## A key does nothing

First see who holds the key:

```
revier keys status
```

It lists every desktop key revier wants, the target it opens, and its status.
It only reads.

| Status | Means | What to do |
|---|---|---|
| `active` | revier holds the key | The key works. Read on. |
| `free` | nothing holds the key | `revier keys install` |
| `stale` | revier holds the key but runs an older command | `revier keys install` |
| `inert` | revier's shortcut is there but switched off; the desktop settings do not show it | `revier keys install` |
| `taken` | another shortcut holds the key | `revier keys install --force`, or give the target another key |
| `builtin` | the desktop itself holds the key | `revier keys install --force` |

Run `revier keys install --dry-run` first to see what would change. Without
`--force`, install claims only the keys that nothing else holds, reports the
rest, and exits 4. `--force` takes a key from whatever has it: another
shortcut is switched off and keeps its command, and a desktop default loses
that one key. The `gsettings reset` line that gives the default back is
printed beside the key. Refer to [integrations/gnome.md](integrations/gnome.md).

When the key is `active` and still does nothing:

- **The focused window belongs to no project.** A key runs
  `revier go <target> --picker`, which opens the popup when no project
  resolves. `revier status`, run in the project's directory, says which
  project a directory resolves to.
- **The project has no such target.** The footer of the TUI, or the
  message of `revier go`, says so.
- **The target is `invalid`.** `revier doctor` says why.
- **In the TUI, a ctrl+shift key.** In a terminal that does not report key
  disambiguation, the key arrives as ctrl and its letter, and the TUI runs the
  target only where that ctrl key means nothing else there. Refer to
  [tui.md](tui.md).

## A target says no host here

The pane shows `no host here` for a target that no host on this machine can
realize: a window target with no window host, or a tab target with no runtime
host. On a machine with no desktop, a window target is expected to show this.

- A window target needs the GNOME window host, which needs GNOME, the Window
  Control extension and its `wctl` on your PATH. Refer to
  [integrations/gnome.md](integrations/gnome.md).
- A tab target needs a runtime host: kitty or tmux. Refer to
  [integrations/kitty.md](integrations/kitty.md) and
  [integrations/tmux.md](integrations/tmux.md).
- The config screen (alt+c) shows under **Hosts** which runtime is in use and
  which window host was found at start.

When `[hosts]` in `config.toml` names hosts, revier tries them in that order.
If none of them answers, revier stops at start and names each one with its
reason. With no list, revier searches the hosts it knows, and a host it does
not find is left out.

A target that shows `unknown` is different: its host is there, but could not
list its windows or panes on the last refresh. A press on it is refused until
the host answers again, and the footer says why. The log has an `instances`
line with the reason.

## An agent shows no state

An agent that shows `unknown`, or no agent at all, depends on its harness:

- **Claude Code** reports its state from version 2.1.212. Update Claude Code
  if it is older. Refer to
  [integrations/claude-code.md](integrations/claude-code.md).
- **opencode** is recognised, but reports no state. Refer to
  [integrations/opencode.md](integrations/opencode.md).
- **Any other harness** needs an agent probe. Refer to
  [extending/probes.md](extending/probes.md).

When a probe fails, the agent shows `unknown`, and the log has a `probe`
line with the reason.

An agent on another machine is shown from the terminal on this machine that
holds it. An agent started on the host itself, or from a third machine, is
not shown in the TUI, because it cannot be reached from here.

## A project is missing from the list

- **The file is not where revier reads.** revier reads every `*.toml` file
  in `~/.config/revier/projects/`. Refer to
  [Where revier keeps its files](#where-revier-keeps-its-files).
- **It is a link to this machine.** A link whose host is this machine is
  skipped, so one set of project files serves both machines. `revier doctor`
  lists the skipped files.
- **The TUI has not read the file yet.** A revier in a terminal of your own
  reads the files when it starts. The popup reads them each time it is raised.
  Start revier again, or hide and raise the popup.

A project file that does not load is not missing. It is listed with the
status `invalid`, and the pane shows the reason. `revier doctor` prints it
with the fix.

## A link will not connect

A project on another machine that does not answer shows as `unreachable`.
The pane shows the reason in full. revier says what ssh said in one line,
with what to do:

| The reason says | What to do |
|---|---|
| `host key not known` | Run `ssh <host>` once and accept the key |
| `ssh refused the key` | Set up key authentication. revier runs ssh in batch mode and cannot type a password. |
| `hostname does not resolve` | Check the name in `~/.ssh/config` |
| `no answer on port 22`, `nothing is listening on port 22`, `the machine is not reachable` | The machine or its sshd is down, or the network is |
| `no shell there - it is a git host` | The name points at a git host, not a machine |
| `no revier on the PATH ssh gives` | revier is installed there, but not on the PATH of a command run over ssh. Put the mise or asdf shims in `~/.zshenv` on the host. |
| `the revier there has no <command>` | Update revier on the host |
| `no answer before the wait ran out` | The host is slow or the connection hangs. Read on. |

Every call to a host shares one ssh connection, which stays open ten minutes
after the last call. When the network changed under it, for example after a
VPN or a suspend, that connection can hang. End it:

```
ssh -O exit -o ControlPath="$XDG_RUNTIME_DIR/revier/ssh-%C" <host>
```

The next call opens a new one. Refer to [links.md](links.md).

## A status shows nothing or is dimmed

A **dimmed** status is the last good answer: the run after it failed. The log
has a `status` line with the reason. A run that takes more than five seconds
is ended and counts as failed.

A status that shows **nothing**:

- **The table was refused.** `revier doctor` names it, for example two tables
  at one place, or an `every` shorter than five seconds.
- **The first run has not answered yet.**
- **There is no room.** A place leaves out segments that do not fit, and
  shows nothing when not even the first one fits. Make the terminal wider.
- **The popup is hidden.** Nothing runs while it is hidden. The statuses that
  fell due run when it is raised.

[Statuses](extending/status.md) has how to configure one.

## The popup does not open

A desktop key shows no error. Run the command in a terminal to see why:

```
revier popup
```

| The message says | What to do |
|---|---|
| `kitty is not on PATH` | Install kitty. The popup is the TUI in a kitty window. |
| `wctl is not on PATH` | Install the GNOME Window Control extension and its `wctl`. |
| `it needs GNOME with the Window Control extension enabled, and [hosts] window must allow gnome` | Enable the extension. If `[hosts] window` in `config.toml` lists hosts, add `gnome` to it. |

Refer to [integrations/kitty.md](integrations/kitty.md) and
[integrations/gnome.md](integrations/gnome.md).

## Where revier keeps its files

| What | Where |
|---|---|
| Configuration | `$REVIER_CONFIG_HOME`, else `$XDG_CONFIG_HOME/revier`, else `~/.config/revier` |
| Project files | `projects/` in the configuration directory |
| State | `$REVIER_STATE_HOME`, else `$XDG_STATE_HOME/revier`, else `~/.local/state/revier` |
| Daily log | `logs/revier-YYYY-MM-DD.log` in the state directory |
| Events | `events.jsonl` in the state directory |

Refer to [configuration.md](configuration.md) for the files you edit.

## Read the log

Every revier process appends to the same daily file: each key press, the TUI,
the popup, a restore. One line is one JSON record. Files older than 14 days
are removed.

| Field | Holds |
|---|---|
| `time`, `level`, `msg` | When, how bad, and the operation, for example `go`, `probe` or `status` |
| `pid`, `cmd` | The process that wrote the line, and its command (`tui` for the TUI) |
| `duration_ms` | How long a timed operation took |
| `err` | Why it failed |
| `project`, `target`, `ref` | What it acted on |

The levels:

- `INFO`: an operation that succeeded.
- `ERROR`: an operation that failed.
- `WARN`: an error revier carried on past, for example a probe that reported
  unknown, or a state file it could not read.

A failure that repeats on every refresh, such as a probe or a host that does
not answer, is written once per different reason, then once more as
`<msg>: recovered` when it stops. Silence after a `WARN` means it still fails.

To split the lines by process, use `pid`. An operation on a project on
another machine runs in the revier on that host, and is in the log there.
`revier list` writes only `WARN` and `ERROR` lines.

```bash
L=${REVIER_STATE_HOME:-${XDG_STATE_HOME:-$HOME/.local/state}/revier}/logs/revier-$(date +%F).log
jq -c 'select(.level != "INFO")' "$L"                                   # everything that went wrong
jq -c 'select(.msg == "go") | {time, project, target, launched, duration_ms}' "$L"
jq -s 'map(select(.duration_ms)) | sort_by(-.duration_ms) | .[:10]' "$L"    # the slowest operations
```

Each command ends with a `command` line that has its `args` and its `exit`
status. A TUI with no `command` line is still running, or was killed.

## revier events

```
revier events --days 3
```

`revier events` prints what revier did in each project, one JSON line per
event, oldest first: a target run or raised, an agent or shell tab opened, an
action run, and the conversation each agent held. It also asks the revier on
each linked host, and prints its events with their `host`. `--local` asks no
host. A host that does not answer is named on stderr. `revier events --help`
lists every field.

The events are kept in `events.jsonl` in the state directory, which revier
never prunes. A failed operation writes no event: to explain a failure, read
the log. Refer to [projects.md](projects.md) for more on events.
