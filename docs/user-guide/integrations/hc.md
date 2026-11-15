# hc

hc ([herdr-core](https://github.com/herdrdev/herdr-core)) is an experimental
runtime host. hc runs your workspaces' programs headless, and kitty shows
them. Your agents keep running when you close kitty, or when it crashes; the
next press opens the window again over them.

Without the switch, kitty runs and shows your workspaces, as before. Nothing
changes until you name hc.

## What you need

- hc 0.1.21 on your PATH as `herdr-core`, or as an `hc` that links to
  `herdr-core`, as hc's install makes it. revier takes no other `hc`,
  because `hc` can also be Holochain's CLI. revier uses the
  core that `hc` would talk to, and starts it when none runs. If you set
  `socket` in hc's `config.toml`, set `HC_SOCKET` to the same path for
  revier.
- kitty, as for the kitty runtime, to see the workspaces. Without kitty, or
  with no display, as over ssh, revier runs hc with no view.

## Switch to hc

1. Close the workspaces kitty runs with `revier shutdown`. A workspace kitty
   runs is not an hc workspace: once you switch, revier does not see it, and
   its window has the title the hc workspace's window gets.

2. Name hc in `~/.config/revier/config.toml`
   (see [`[hosts]`](../configuration.md#hosts)):

   ```toml
   [hosts]
   runtime = ["hc"]
   ```

3. Run `revier doctor`. Its last line says `runtime hc is shown by the kitty
   view`.

Your project files stay as they are. A press opens the workspace in a kitty
window of its title, with its tabs in their order, the active tab current,
and an agent and a shell side by side where the tab declares them. A target
with `inside`, `revier agent new`, `revier shell new`, the kitty keys and
`revier session restore` work as they do on kitty. A second press goes home.

To switch back, remove the line and run `revier shutdown` first: the kitty
runtime does not read hc's terminals.

## What changes

- Each kitty window runs hc's attach of one terminal. Closing a window, a tab
  or the whole kitty ends no program. The next press on the workspace opens
  its window again over the running terminals, where the workspace's `place`
  puts it, and `revier session restore` does too.
- To end a program, close it in revier: `revier shutdown`, `revier agent
  close`, or exit the program. Its window then closes.
- `revier agent new --no-focus` from a script adds the tab to the window
  that shows the workspace, and opens no window where none shows it.
- In a kitty window that shows no workspace, the shell key opens a plain kitty
  tab in that window's directory, as `outside = "tab"` says. That tab is kitty's alone, so revier does
  not list it, and it ends with kitty.

## Without a view

Set `view = ["none"]` under `[hosts]`, or run revier over ssh. revier opens,
lists, reads and types into the workspaces, and focuses nothing. To see one,
put a terminal on it:

```bash
revier open demo --attach
```

The terminal shows the workspace's first terminal. Detach with hc's own key,
`ctrl+\`. To see another, run `hc attach` with its name from `hc ls`.

## The core's environment

The core gives every program it starts the environment of the command that
started the core, for as long as the core runs. Where systemd runs your
session, revier starts the core as a user service named `revier-hc-` and a
short hash of the core's socket, one per hc session, with your
desktop session's environment, wherever you run revier: a shell's own
settings, such as an activated virtualenv or a credential, do not reach your
agents. To give your agents a variable, such as `CLAUDE_CONFIG_DIR`, set it in
the session with `systemctl --user set-environment NAME=value` before the core
starts. `hc server stop` stops the core, and ends every program in it. A core
you start yourself with `hc server start` takes your shell's environment.

A user service stops when you log out of your last session, and so the core
and every agent in it stop too. To keep them running after you log out, as
over ssh, run once:

```bash
loginctl enable-linger
```

Without systemd, a core revier starts takes the environment of the revier
command, without the variables of the terminal, the multiplexer, the Claude
Code session or the revier command it ran in, such as `KITTY_WINDOW_ID`,
`TMUX` (a tmux you run revier in) or `CLAUDE_CODE_SESSION_ID`. hc cannot remove a
variable, so revier sets them empty in each program: the Claude Code
session's always, and each of the others only when the core holds it, since
an empty one reads as set. A core that `hc server restart` restarted keeps
its old environment where revier cannot see it, so a terminal's variable in
it reaches each program; run `hc server restart --fresh` from a terminal
outside any multiplexer to replace it. A Claude Code that kept another
session's variables would save no transcript, and revier would show its state
as unknown. A core revier starts also drops the `GIT_EDITOR=true` that Claude
Code sets; a core you start yourself from a Claude Code session keeps it, and
`git commit` without `-m` then aborts in each shell. To give the core another
environment, start it yourself with `hc server start`.

## Limits

- An image an agent draws, such as with `kitten icat`, is gone after the
  window opens again. `kitten icat` draws one only with
  `--use-window-size`, because hc does not answer the terminal's query for
  its size in pixels.
- A program that writes a lot of output at once lags in its window: every
  byte goes through hc's attach.
- The TUI and `revier agent read --screen` show the screen without its
  colours, and without the lines above it: hc reads those of an agent's
  full-screen view by scrolling the agent. kitty's own scrollback in a window
  holds only what that window showed since it opened.
- A close gives a program about a second to save before hc kills it.
- hc does not bring a program back after a reboot. Run
  `revier session restore`: it opens the workspaces again, each running its
  `launch` anew.
- Terminals on a machine hc added are not listed. hc still asks each such
  machine on every listing, and one that does not answer slows the listing
  by up to 3 s.
- A panel's `title` is not shown: the terminal's title is the one its
  program sets.
- A terminal you attach to by hand in a kitty of your own is not the
  workspace's window: a press opens the workspace's own window beside it.
