# kitty

kitty is a runtime host: the terminal that holds a project's workspace. revier
opens each workspace as a kitty OS window, opens its tabs and split windows,
and reads every kitty window to find the agents and their state. The popup is
also a kitty window, whatever your runtime host is.

## What you need

- kitty, with `kitten` on your PATH or beside the `kitty` binary.
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
3. Run `revier open demo`. The workspace opens in the kitty you ran the
   command in. When no kitty answers, revier starts one of its own.

Without step 1, revier still works, but it starts a kitty of its own, and
your other kitty windows stay out of its reach.

## Add tabs from a kitty key

These maps add a tab to the workspace you are in. `--copy-env` tells revier
which kitty the key came from.

```
map ctrl+shift+z launch --type=background --copy-env --cwd=current sh -lc 'revier agent new --panel "$1" --dir . || kitten @ launch --type=tab --cwd=current' sh @active-kitty-window-id
map ctrl+shift+t launch --type=background --copy-env --cwd=current sh -lc 'revier shell new --panel "$1" --dir . || kitten @ launch --type=tab --cwd=current' sh @active-kitty-window-id
```

The first adds an agent tab, the second a shell tab. In a window that is no
project's workspace, both open a plain shell tab beside it, or nothing if you
set `outside = "none"` in [`[terminal]`](../configuration.md#terminal). The
`kitten @ launch` after `||` opens kitty's own new tab only when revier itself
fails.

## What does not work

- A kitty window does not keep its contents after kitty exits. Use
  `revier session save` and `revier session restore`; see
  [Projects](../projects.md).
- `revier open --attach` needs tmux or herdr.
- A kitty window you opened yourself is no workspace until you run
  `revier attach` in it. revier reads its agents only when it is the one
  unnamed window of its kitty process.
- Under Wayland, without GNOME, revier cannot bring a kitty window to the
  front.

**revier opens a second kitty instead of yours.** Your kitty has no control
socket. Do step 1 of [Set it up](#set-it-up) and restart kitty.
