# hc

hc ([herdr-core](https://github.com/herdrdev/herdr-core)) is an experimental
runtime host. revier opens each workspace as an hc terminal and reads every
terminal to find the agents. hc runs them headless, so
no terminal has to show them.

## What you need

- hc 0.1.21 on your PATH as `herdr-core`, or as an `hc` that links to
  `herdr-core`, as hc's install makes it. revier takes no other `hc`,
  because `hc` can also be Holochain's CLI. revier uses the
  core that `hc` would talk to, and starts it when none runs. If you set
  `socket` in hc's `config.toml`, set `HC_SOCKET` to the same path for
  revier.

## Set it up

1. Name hc in `~/.config/revier/config.toml`
   (see [`[hosts]`](../configuration.md#hosts)):

   ```toml
   [hosts]
   runtime = ["hc"]
   ```

2. Open a project and put this terminal on it:

   ```bash
   revier open demo --attach
   ```

   The terminal shows the workspace's first panel. Detach with hc's own key,
   `ctrl+\`. The agents go on.

3. From another terminal, run `revier` or `revier list` to see the agents.

The core gives every program it starts the environment of the command that
started the core: the revier command, or your `hc server start`. A core
revier starts is started without the variables of the terminal, the
multiplexer, the Claude Code session or the revier command it ran in, such as
`KITTY_WINDOW_ID`, `TMUX` or `CLAUDE_CODE_SESSION_ID`. hc cannot remove a
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

## What works

- `revier open`, `revier list`, and the TUI's agent list. Claude Code reports
  its state as it does on tmux.
- `revier agent prompt`, `revier agent send-keys` and `revier agent read
  --screen`.
- `revier shutdown` and `revier agent close`.
- `revier open --attach`, onto the workspace's first panel.

## What does not work

- Tabs: a target with `inside` or `tabs`, `revier agent new` and
  `revier shell new`. Panels are declared on a tab only, so each target is
  one hc terminal that runs its `launch`.
- A second press does not return home: hc keeps no focus, so revier cannot
  tell what you look at. A press on an open workspace does nothing you see;
  attach to it.
- The attach shows the workspace's first terminal, and fails once that
  terminal is closed, or when you run it in that terminal. To see another,
  run `hc attach` with its name, `rv-<id>-<place>`, from `hc ls`.
- The TUI and `revier agent read --screen` show the screen without its
  colours, and without the lines above it: hc reads those of an agent's
  full-screen view by scrolling the agent.
- `kitten icat` draws an image only with `--use-window-size`, because hc
  does not answer the terminal's query for its size in pixels. The image is
  gone after you detach and attach again.
- A close gives a program about a second to save before hc kills it.
- hc does not bring a program back after a reboot. Run
  `revier session restore`: it opens the workspaces again, each running its
  `launch` anew.
- Terminals on a machine hc added are not listed. hc still asks each such
  machine on every listing, and one that does not answer slows the listing
  by up to 3 s.
- The popup is a kitty window. With hc and no kitty, run `revier` in a
  terminal. Enter on a project or an agent in the popup brings no window to
  the front; attach to see it.
