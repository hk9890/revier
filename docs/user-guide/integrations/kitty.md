# kitty

kitty is a runtime host: the terminal that holds a project's workspace. revier
opens each workspace as a kitty OS window, opens its tabs and split windows,
and reads every kitty window to find the agents and their state. The popup is
also a kitty window, whatever your runtime host is.

revier opens every workspace in a kitty it starts for the workspaces, never in
a kitty you started and never in the popup's. A kitty option applies to every
window of its kitty, so the popup's options, such as its hidden title bar,
stay out of your workspaces.

revier's kitty runs in the background with no window of its own, and keeps
running after you close the last workspace, so the next one opens at once.
Where systemd runs your session, revier starts it as the user service
`revier-kitty`, with your desktop session's environment, wherever you run
`revier open`: a shell's own settings, such as an activated virtualenv, do not
reach your workspaces. Without systemd, it starts with the environment of the
command that opened the first workspace. A window you open in it yourself is
no workspace.

To make revier's kitty read a changed `kitty.conf`, or to use an updated kitty,
stop it; the next workspace you open starts it again:

```bash
systemctl --user stop revier-kitty
```

This closes every workspace in it.

## What you need

- kitty 0.42 or later, with `kitten` on your PATH or beside the `kitty` binary.
- `XDG_RUNTIME_DIR` set. A desktop login sets it.
- To raise a kitty window under Wayland, and for the popup: GNOME with `wctl`.
  See [GNOME](gnome.md).

## Set it up

1. Let revier reach every kitty you start. Add to
   `~/.config/kitty/kitty.conf`:

   ```
   allow_remote_control socket-only
   listen_on unix:${XDG_RUNTIME_DIR}/kitty
   ```

2. Restart kitty.
3. Run `revier open demo`. The workspace opens in revier's kitty, which
   revier starts when it is not running.

Without step 1, revier still opens workspaces, but your own kitty windows stay
out of its reach: `revier attach` reads no agents in them, and the trigger key
does not switch a TUI you run in them.

## Add tabs from a kitty key

kitty keys add a tab to the workspace you are in: ctrl+shift+z an agent tab,
ctrl+shift+t a shell tab, and the `key` of each named agent a tab of that
agent. [`[terminal]`](../configuration.md#terminal) sets them. To write them
into kitty's configuration:

```bash
revier term install --dry-run   # see what would change
revier term install
```

This writes `revier.conf` in kitty's configuration directory and adds
`include revier.conf` at the top of `kitty.conf`. kitty reads the change by
itself. If `kitty.conf` did not exist, restart kitty. Run `revier term
install` again after you change a terminal key in `config.toml`. `revier term
status` shows each key, and `revier term uninstall` removes both again. If
nothing else is left in `kitty.conf`, uninstall removes the file too, or
leaves it empty when `kitty.conf` itself is a link.

- revier writes through links, so a `kitty.conf` in a dotfiles repository
  stays a link.
- A map of your own below the include wins: for example a
  `map --when-focus-on var:in_tmux ctrl+shift+t ...` for windows that run
  tmux. `revier term status` names each such map by its line.
- A terminal key on the same chord as `trigger_key` or a target's `key` is
  left out: the desktop takes that press first.
- A shell tab starts in the directory of the window you pressed the key in.
  An agent tab starts in the project, so from a worktree the agent can still
  enter any worktree.
- In a window that is no project's workspace, `outside` in
  [`[terminal]`](../configuration.md#terminal) sets what a key does. When
  revier itself fails, for example on an agent whose command uses a
  template, kitty's own new tab opens.

**You wrote these maps by hand before.** Delete your own `map ctrl+shift+z`
and `map ctrl+shift+t` lines that run `revier agent new` or `revier shell new`
from `kitty.conf`: they come after the include and win over revier's. `revier
term status` names them as `shadowed by kitty.conf:<line>`.

## What does not work

- A kitty window does not keep its contents after kitty exits. Use
  `revier session save` and `revier session restore`; see
  [Projects](../projects.md).
- `revier open --attach` needs tmux or herdr.
- A kitty window you opened yourself is no workspace until you run
  `revier attach` in it. revier reads its agents only when it is the one
  unnamed window of its kitty process.

**`revier attach` reads no agents in your kitty.** Your kitty
has no control socket. Do step 1 of [Set it up](#set-it-up) and restart
kitty.
