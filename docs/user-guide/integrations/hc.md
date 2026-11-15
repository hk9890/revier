# hc

hc ([herdr-core](https://github.com/herdrdev/herdr-core)) is an experimental
runtime host. hc runs your workspaces' programs headless, and kitty shows
them. Your agents keep running when you close kitty, or when it crashes; the
next press opens the window again over them. Until you name hc, kitty runs
and shows your workspaces, as before.

## What you need

- hc 0.1.21 on your PATH as `herdr-core`, or as an `hc` that links to
  `herdr-core`, as hc's install makes it. revier takes no other `hc`,
  because `hc` can also be Holochain's CLI. If you set `socket` in hc's
  `config.toml`, set `HC_SOCKET` to the same path for revier.
- kitty, to see the workspaces. Without kitty, or with no display, as over
  ssh, revier runs hc with no view.

## Switch to hc

1. Close the workspaces kitty runs with `revier shutdown`. Once you switch,
   revier does not see them, and a window left open has the title of the
   hc workspace's window, so a press cannot tell the two apart.

2. Name hc in `~/.config/revier/config.toml`
   (see [`[hosts]`](../configuration.md#hosts)):

   ```toml
   [hosts]
   runtime = ["hc"]
   ```

3. Run `revier doctor` to check the setup.

Your project files stay as they are. A press opens the workspace in a kitty
window, with its tabs, panels and keys as on kitty, and a second press goes
home.

To switch back, run `revier shutdown` first, then remove the line: the kitty
runtime does not read hc's terminals.

## What changes

- Each kitty window shows one hc terminal. Closing a window, a tab or the
  whole kitty ends no program. The next press, or `revier session restore`,
  opens the window again over the running terminals.
- To end a program, close it in revier: `revier shutdown` or `revier agent
  close`. Its window then closes.
- When a panel's command ends on its own, as an agent that exits or fails,
  its window stays open with what the command printed and its exit code, and
  closes when you press Enter. Until then the window is still the
  workspace's, and `revier agent close` or `revier shutdown` closes it. A
  shell's window closes when the shell exits.
- `revier agent new --no-focus` adds the tab to the window that shows the
  workspace, and opens no window where none shows it.
- In a kitty window that shows no workspace, the shell key opens a plain kitty
  tab, as `outside = "tab"` says. revier does not list it, and it ends with
  kitty.

## Without a view

Set `view = ["none"]` under `[hosts]`, or run revier over ssh. revier opens,
lists, reads and types into the workspaces, and focuses nothing. To see one,
put a terminal on it:

```bash
revier open demo --attach
```

Detach with hc's key, `ctrl+\`. To see another terminal, run `hc attach` with
its name from `hc ls`.

## The core's environment

The core gives every program it starts the environment of the command that
started the core. Where systemd runs your session, revier starts the core as
a user service with your desktop session's environment, so a shell's own
settings, such as an activated virtualenv, do not reach your agents. To give
your agents a variable, set it before the core starts:

```bash
systemctl --user set-environment NAME=value
```

`hc server stop` stops the core and every program in it. A core you start
yourself with `hc server start` takes your shell's environment.

Without systemd, the core takes the environment of the revier command, with
the variables of the terminal, tmux and Claude Code session it ran in set
empty. A core that `hc server restart` restarted keeps its old environment;
run `hc server restart --fresh` from a terminal outside tmux to replace it.

## Limits

- An image an agent draws, such as with `kitten icat`, is gone after the
  window opens again, and `kitten icat` draws one only with
  `--use-window-size`.
- A program that writes a lot of output at once lags in its window.
- The TUI and `revier agent read --screen` show the screen without its
  colours and without the lines above it.
- A close gives a program about a second to save before hc kills it.
- hc does not bring a program back after a reboot. Run
  `revier session restore` to open the workspaces again.
- Terminals on a machine hc added are not listed, and one that does not
  answer slows each listing by up to 3 s.
- A panel's `title` is not shown: the terminal's title is the one its
  program sets.
- A terminal you attach to by hand in a kitty of your own is not the
  workspace's window: a press opens the workspace's own window beside it.
