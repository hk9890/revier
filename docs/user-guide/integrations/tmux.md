# tmux

tmux is a runtime host. revier opens each workspace as a tmux session, its
tabs as tmux windows and its panels as panes, and reads every pane to find the
agents. It needs no attached client, so revier works the same on a machine
with no display or over ssh.

## What you need

- `tmux` on your PATH. Nothing in `tmux.conf`. revier uses your default tmux
  server.

## Set it up

1. Name tmux in `~/.config/revier/config.toml`
   (see [`[hosts]`](../configuration.md#hosts)):

   ```toml
   [hosts]
   runtime = ["tmux"]
   ```

   Without it, an installed kitty wins, even when you run revier inside tmux
   or on a machine with no display. This line is enough there: with no
   `window` list, revier skips GNOME when it does not answer.

2. Open a project and put this terminal on it:

   ```bash
   revier open demo --attach
   ```

   Detach with tmux's own key. The agents go on. In a pane of the same tmux
   server, the command switches that pane's client to the session and
   attaches nothing, because tmux does not nest in itself.

3. From another terminal, run `revier` or `revier list` to see the agents.

## Which client revier switches

tmux has no window to raise. When revier opens or raises a workspace, it
switches a tmux client to the session: the client of the pane you ran revier
from, or, from outside tmux, the one attached client. With several clients
and revier outside tmux, it switches none.

## Add tabs from a tmux key

```
bind-key A run-shell -b 'revier agent new --panel "#{pane_id}" --dir "#{pane_current_path}"'
bind-key S run-shell -b 'revier shell new --panel "#{pane_id}" --dir "#{pane_current_path}"'
```

## What does not work

- revier does not raise the desktop window that shows a tmux client.
- `place` does nothing: a session is no desktop window.
- The tmux server does not outlive a reboot. Use `revier session save` and
  `revier session restore`; see [Projects](../projects.md).
- The popup is a kitty window. With tmux and no kitty, the trigger key does
  nothing; run `revier` in a terminal.
