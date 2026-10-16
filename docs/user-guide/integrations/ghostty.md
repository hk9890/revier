# Ghostty

revier has no Ghostty host: it cannot open a workspace as a Ghostty window or
see into one. Use tmux inside Ghostty instead. revier then opens each
workspace as a tmux session, you show it in a Ghostty window, and revier sees
every agent through tmux.

## What you need

- tmux. See [tmux](tmux.md).
- For window targets and desktop keys: GNOME with `wctl`. See
  [GNOME](gnome.md).
- For the popup: kitty. Without kitty, the trigger key does nothing and shows
  no error. Run `revier` in a Ghostty window instead.

## Set it up

1. Name tmux as the runtime host, as [tmux](tmux.md#set-it-up) step 1
   shows.

2. In a Ghostty window, run `revier open demo --attach`. The window is now a
   tmux client on the project's session.
3. Keep exactly one Ghostty window attached. When you choose a project in
   the TUI or press its key, revier switches this client to the project's
   session. With two attached clients, a desktop key switches neither.
4. On GNOME, so that a desktop key knows the project of a focused Ghostty
   window, let tmux set the window title. revier puts the workspace's name in
   the tmux option `@revier-name` of each session it opens. Add to
   `~/.tmux.conf`:

   ```
   set -g set-titles on
   set -g set-titles-string '#{?@revier-name,#{@revier-name},#S}'
   ```

## A Ghostty window as a target

On GNOME, a window target can run Ghostty. Give the window a fixed title, and
match it (see [Realization fields](../configuration.md#realization-fields)):

```toml
[[target]]
name = "logs"
key  = "ctrl-shift-l"

  [target.window]
  launch = ["ghostty", "--title=logs:{{.Name}}", "-e", "tail", "-f", "{{.Path}}/app.log"]
  match  = { class = "ghostty", title = "^logs:{{.Name}}$" }
```

revier sees such a window, not what runs in it.

## What does not work

- `ghostty` in `[hosts]`: revier stops with "unknown runtime host".
- Agents that run in Ghostty outside tmux.
- Raising the Ghostty window that shows a session: revier switches the
  session in it, and the window stays where it is.
- `revier agent new --panel` from a Ghostty key. Use a tmux key, as
  [tmux](tmux.md) shows.
