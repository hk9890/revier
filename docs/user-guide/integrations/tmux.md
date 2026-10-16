# tmux

This page is for you if you work in tmux: on a desktop, on a machine with no
display, or over ssh. It tells you what a workspace becomes in tmux, how to
make revier use tmux, and what works and what does not.

## What revier does with tmux

tmux is a runtime host: the terminal that holds a project's workspace and its
agents. revier opens each workspace as a tmux session, opens tabs as tmux
windows of that session, and reads every pane to find the agents and their
state. Every query works with no terminal attached to tmux, so revier runs the
same with no display at all.

## What you need

- `tmux` on your PATH. revier is checked against tmux 3.4 and 3.7.
- Nothing in `tmux.conf`. revier starts the tmux server when none runs.

revier uses your default tmux server, the one plain `tmux` talks to. It has no
setting for another socket.

## How revier picks tmux

With no `[hosts]` table in `~/.config/revier/config.toml`, revier tries kitty
first, then tmux. revier does not look at `$TMUX`: running revier inside tmux
does not make it pick tmux. On a machine where kitty is installed, kitty wins
unless you name tmux:

```toml
[hosts]
runtime = ["tmux"]
```

Name tmux on every machine where you want tmux, also on one with no display:
there, an installed kitty still counts as usable, and revier would try to
start it.

A list is a preference order. `runtime = ["tmux", "kitty"]` tries tmux first.
When no name in the list works, revier stops with an error that names each
one. To use no runtime host at all, write `runtime = ["none"]`.

You can also change the runtime host in the TUI: `alt+c` opens the config
screen, and the `runtime` row steps through `auto`, `kitty`, `tmux` and
`none`.

## Set it up

1. Name tmux in `~/.config/revier/config.toml`:

   ```toml
   [hosts]
   runtime = ["tmux"]
   ```

2. Open a project and put this terminal on it:

   ```bash
   revier open demo --attach
   ```

   revier creates the session when it is not open, then this terminal
   becomes a tmux client attached to it. Detach as usual, with tmux's own
   key.

3. From another terminal, or after you detach, see what the agents do:

   ```bash
   revier list
   revier
   ```

## What a workspace is in tmux

A workspace is one tmux session.

- revier names the session after the target's runtime `name`, for example
  `session:demo`. tmux changes `:` and `.` in a session name to `_`, so revier
  keeps the real name in the session option `@revier-name`, and finds the
  session by that.
- A tab target (`inside = "home"`) is a tmux window of the session. The
  `agent` tab that `revier new` writes holds Claude Code and a shell, as two
  panes side by side.
- Each panel is a pane. revier reads its current command, its title and the
  variables revier left in the pane option `@revier`.
- `revier agent new` and `revier shell new` add a window after the last one.

A session outlives the terminal you close: detach, and the agents go on.
The tmux server does not outlive a reboot. To get the open projects back after
one, use `revier session save` and `revier session restore`; see
[Projects](../projects.md).

## Which terminal revier switches

tmux has no window of its own to raise. When revier opens or raises a
workspace, it switches a tmux client to the session:

- Run from a pane of the same tmux server, for example from a tmux key or the
  TUI in a pane, revier switches the client that shows that pane.
- Run from outside tmux, revier switches the one client attached to the
  server. With several clients, it switches none, since tmux cannot say which
  one you mean.

revier does not raise the desktop window that shows the tmux client. A
desktop key that raises a workspace in tmux switches the session, and the
terminal window stays where it is.

## Use revier with no display, or over ssh

On a machine with no display, everything that lives in a terminal works:
workspaces, agent state, the TUI, `revier list`, and every `revier agent`
command. Targets that are only a desktop window, such as an editor with only a
`[target.window]` part, report that no host can open them. A target with
both parts uses its runtime part there.

To work on such a machine:

```bash
ssh buildbox
revier open demo --attach
```

To drive the agent from a script, with no terminal attached at all:

```bash
revier agent prompt demo 'run the tests and say what failed'
revier agent wait demo --until stopped --timeout 600
revier agent read demo
```

[Agents](../agents.md) has every `revier agent` command.

To reach a project on another machine from your desktop, link it instead;
see [Links](../links.md). A link does not use tmux on the other machine: its
panels run revier there over ssh, and that revier needs no tmux.

## Add tabs from a tmux key

`revier agent new` and `revier shell new` take `--panel <id>`. Under tmux the
id is the pane id, such as `%3`, which is unique on the server. A tmux key can
pass the pane it was pressed in:

```
bind-key A run-shell -b 'revier agent new --panel "#{pane_id}" --dir "#{pane_current_path}"'
bind-key S run-shell -b 'revier shell new --panel "#{pane_id}" --dir "#{pane_current_path}"'
```

## What works and what does not

| Works in tmux | Does not |
|---|---|
| Workspaces as sessions, tabs as windows, panels as panes | Raising the desktop window that shows the session |
| Agent state with no client attached | Choosing a client when several are attached and revier runs outside tmux |
| `revier open --attach` | A private tmux socket |
| `revier agent read --screen`, `agent prompt`, `agent send-keys` | `place` on a workspace: a session is no desktop window |
| Sessions that outlive the terminal | Switching the popup's list with the trigger key, which takes kitty as the runtime |

## Troubleshooting

**revier opens kitty, not tmux.** revier tries kitty first. Set
`[hosts] runtime = ["tmux"]`.

**"tmux not on PATH".** Install tmux, or put it on the PATH revier runs with.

**"--attach: the kitty runtime cannot put a terminal on a workspace".**
`--attach` takes tmux. Set `[hosts] runtime = ["tmux"]`.

**"--attach: the workspace has not come up yet".** The session did not open
in time. Run the command again.

**The key raises nothing.** Several clients are attached and revier ran
outside tmux, so it switched none. Detach the clients you do not use, or press
the key from a pane of the server.

**An agent typed into a shell shows as unknown.** For tmux, a pane's process
is the program the pane started. When you type `claude` into a shell pane,
that process is the shell, and Claude Code's listing cannot be matched to it.
Start the agent from the project file or with `revier agent new`, so it is the
pane's own process. [Claude Code](claude-code.md) says more.

For anything else, start with `revier doctor` and
[Troubleshooting](../troubleshooting.md).
