# Ghostty

revier has no Ghostty host: it cannot open a workspace as a Ghostty window or
see into one. Run your workspaces in hc instead, with no view, and put a
Ghostty window on one with `revier open --attach`. revier sees every agent
through hc.

## What you need

- hc. See [hc](hc.md).
- For window targets and desktop keys: GNOME with `wctl`. See
  [GNOME](gnome.md).
- For the popup: kitty. Without kitty, the trigger key does nothing and shows
  no error. Run `revier` in a Ghostty window instead.

## Set it up

1. Name hc as the runtime host, and no view, in
   `~/.config/revier/config.toml`
   (see [`[hosts]`](../configuration.md#hosts)):

   ```toml
   [hosts]
   runtime = ["hc"]
   view    = ["none"]
   ```

2. In a Ghostty window, run `revier open demo --attach`. The window now shows
   the workspace's first terminal. Detach with hc's own key, `ctrl+\`.

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

- `ghostty` in `[hosts]`: revier stops with `unknown host "ghostty"`.
- Agents that run in Ghostty outside hc.
- Raising or switching the Ghostty window: with no view, a press focuses
  nothing. Attach the window to another terminal with `hc attach`.
- Tabs: the window shows one terminal at a time.
- `revier agent new --panel` from a Ghostty key.
