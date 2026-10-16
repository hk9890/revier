# Ghostty

This page is for you if Ghostty is your terminal. revier has no Ghostty host:
it cannot open a workspace as a Ghostty window, and it cannot see into one.
This page tells you what you can still do, and gives a setup that works.

## What revier does with Ghostty

Nothing directly. revier drives two terminals, kitty and tmux. With Ghostty,
you have two ways in, and you can use both:

- **tmux inside Ghostty.** revier opens each workspace as a tmux session, and
  you show the session in a Ghostty window. revier sees every agent, because
  it reads tmux, not Ghostty.
- **Ghostty windows as window targets.** On GNOME, revier can open a Ghostty
  window for a target and raise it on the next press, as it does for an
  editor. It sees the window, not what runs in it.

## What you need

- tmux, for the workspaces. See [tmux](tmux.md).
- For window targets and the desktop keys: GNOME with `wctl`. See
  [GNOME](gnome.md).
- For the popup: kitty installed. The popup is always a kitty window, whatever
  terminal you use. Without kitty, use the TUI in a Ghostty window: run
  `revier`.

## How revier picks a host for you

revier tries kitty first, then tmux. When kitty is installed, revier uses it
and opens your workspaces in kitty windows. Name tmux to keep the workspaces
in tmux:

```toml
# ~/.config/revier/config.toml
[hosts]
runtime = ["tmux"]
```

`ghostty` is not a host revier knows. Named in `[hosts]`, it stops revier
with "unknown runtime host".

## Set it up

1. Name tmux as the runtime host, as above.

2. Open a project in a Ghostty window, with this terminal on it:

   ```bash
   revier open demo --attach
   ```

   The Ghostty window is now a tmux client on the session `session:demo`. The
   agent and the shell are panes of its first window.

3. Run the TUI in another Ghostty window, or in a tmux window:

   ```bash
   revier
   ```

   Every agent in every session shows its state.

4. Optional, on GNOME: claim the desktop keys with
   `revier keys install --dry-run`, then `revier keys install`. See
   [GNOME](gnome.md).

## A Ghostty window as a target

On GNOME, a `[target.window]` can run Ghostty. Give the window a title that
`match` can find, and match the class Ghostty gives its windows:

```toml
[[target]]
name = "logs"
key  = "ctrl-shift-l"

  [target.window]
  launch = ["ghostty", "--title=logs:{{.Name}}", "-e", "tail", "-f", "{{.Path}}/app.log"]
  match  = { class = "ghostty", title = "^logs:{{.Name}}$" }
```

The first press opens the window. Every press after it raises the same
window, and a press while it has the focus returns you to the project's home.
`launch` must give the window a title that stays: a program that sets its own
title would make the window lose its match. Claim on appear still binds the
window when it appears, as [GNOME](gnome.md) explains.

A Ghostty window you opened yourself can join a project too. Focus it and run
`revier attach -p demo`. revier then raises and closes it with the project,
but does not see the agents in it.

## A complete setup

`~/.config/revier/config.toml`:

```toml
[hosts]
runtime = ["tmux"]

[ui]
trigger_key = "alt-space"
```

`~/.config/revier/projects/demo.toml`, as `revier new` writes it, with a key
on `home`:

```toml
path = "~/dev/demo"

[[target]]
name = "home"
key  = "ctrl-shift-u"
home = true
  [target.runtime]
  name = "session:{{.Name}}"
  match = { title = "^session:{{.Name}}$" }
  tabs = ["agent"]

[[target]]
name = "agent"
  [target.runtime]
  inside = "home"
    [[target.runtime.panels]]
    kind = "agent"
    title = "claude"
    command = ["claude"]
    [[target.runtime.panels]]
    kind = "shell"
    title = "shell"
```

Keep one Ghostty window attached to tmux. When you choose a project in the
TUI, or press a key for one, revier switches that tmux client to the
project's session. With more than one client attached, revier switches the
client of the pane you ran it from, and none when it ran outside tmux.
[tmux](tmux.md) has the rules.

A desktop key finds its project from the title of the focused window. A
Ghostty window shows the title the program inside it sets, so let tmux set it
to the workspace's name. Add to `~/.tmux.conf`:

```
set -g set-titles on
set -g set-titles-string '#{?@revier-name,#{@revier-name},#S}'
```

The Ghostty window that shows `session:demo` is then titled `session:demo`,
which the `home` rule matches. Without it, a desktop key pressed on a Ghostty
window acts on the project revier acted on last, or opens the popup.

## What works and what does not

| Works | Does not |
|---|---|
| Workspaces as tmux sessions, shown in a Ghostty window | A workspace as a Ghostty window, with Ghostty tabs and splits |
| Agent state, conversations and resume, through tmux | Seeing panels or agents that run in Ghostty itself, outside tmux |
| `revier agent` commands on agents in tmux | Raising the Ghostty window that shows a tmux session: revier switches the session in it |
| Ghostty windows as window targets, on GNOME | `revier agent new --panel` from a Ghostty key: Ghostty has no window id revier knows |
| `revier attach` on a Ghostty window | The kitty.conf maps on [kitty](kitty.md); use a tmux key, as [tmux](tmux.md) shows |
| The popup, when kitty is installed | Switching the popup's list with the trigger key, which takes kitty as the runtime |

## Troubleshooting

**Workspaces open in kitty.** kitty is installed and revier tries it first.
Set `[hosts] runtime = ["tmux"]`.

**"unknown runtime host".** `[hosts] runtime` names a host revier does not
have. Use `kitty`, `tmux` or `none`.

**A key opens the project but the Ghostty window stays behind.** revier
switches the tmux client and does not raise its window. Bring the Ghostty
window to the front yourself, or keep it where you see it.

**An agent you typed into a shell shows as unknown.** Start the agent from the
project file or with `revier agent new`. [Claude Code](claude-code.md)
explains why.

**The popup says kitty is not on PATH.** Install kitty for the popup, or run
`revier` in a Ghostty window.

For anything else, start with `revier doctor` and
[Troubleshooting](../troubleshooting.md).
