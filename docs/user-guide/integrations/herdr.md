# herdr

herdr is a runtime host. revier opens each workspace as a herdr workspace, its
tabs as herdr tabs and its panels as panes, and reads every pane to find the
agents. One herdr server holds every workspace.

## What you need

- herdr 0.9.5 or newer as `herdr` on your PATH. revier starts the server when
  none runs.

## Set it up

1. Name herdr in `~/.config/revier/config.toml`
   (see [`[hosts]`](../configuration.md#hosts)). Without it, an installed kitty
   or tmux wins:

   ```toml
   [hosts]
   runtime = ["herdr"]
   ```

2. Let `revier session restore` resume the agents, not herdr. In herdr's
   `config.toml`:

   ```toml
   [session]
   resume_agents_on_restore = false
   ```

3. Open a project and put this terminal on it:

   ```bash
   revier open demo --attach
   ```

## What does not work

- A focus moves every terminal attached to herdr: herdr cannot move one alone.
  Attach one terminal.
- revier does not raise the desktop window that shows herdr, and a second
  press does not return home on GNOME. `place` does nothing.
- While you scroll a pane up in herdr, the TUI and `revier agent read --screen`
  show those lines, not the newest.
- After the herdr server restarts, herdr brings its workspaces back as shells,
  without revier's marks: a tab target then opens a second tab. Close those
  workspaces in herdr, then run `revier session restore`.
- The popup is a kitty window. With herdr and no kitty, run `revier` in a
  terminal.
