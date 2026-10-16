# GNOME

GNOME is the window host. Through it, revier lists the desktop's windows,
raises, places and closes them, and reads which one has the focus. A target
can then be any desktop window: an editor, a browser page, a diff viewer.
revier also writes its desktop keys into GNOME, and the popup needs it.

## What you need

- The [Window Control extension](https://github.com/carlo9890/gnome-window-control),
  switched on, and its command `wctl` on your PATH. GNOME gives no window list
  to an outside program; `wctl` does.
- `revier` on the PATH of a login shell. Each desktop key runs it through
  `sh -lc`.

revier uses GNOME when `XDG_CURRENT_DESKTOP` names GNOME (or is not set) and
`wctl list --json` answers. Else it runs with no window host and says
nothing; see [`[hosts]`](../configuration.md#hosts).

## Set it up

1. Install the extension and `wctl`. Log out and in.
2. Check that `wctl list --json` prints your windows.
3. Give a target a window (see
   [Realization fields](../configuration.md#realization-fields)):

   ```toml
   [[target]]
   name = "editor"
   key  = "ctrl-shift-o"

     [target.window]
     launch = ["code", "{{.Path}}"]
     match  = { class = "^[Cc]ode$", title = "{{.Name}}" }
   ```

   `class` is the `wm_class` that `wctl list --json` shows.
4. Claim the keys. Look first, then install:

   ```bash
   revier keys install --dry-run
   revier keys install
   ```

   revier skips a key that something else holds, such as Alt+Space on stock
   GNOME; see [Getting started](../getting-started.md#install-the-desktop-keys).
   `revier keys status` shows who holds each key. The trigger key is
   `revier: popup` in GNOME Settings; an install renames one that an older
   revier wrote as `revier: picker`.

## Windows that open late

Many applications open their window a while after the command starts. revier
binds the one new window that matches the target within 60 seconds, even
before its title matches.

To give a project a window no target describes, focus it and run
`revier attach -p demo`.

## Without GNOME

On another desktop, or with no display, a target with only a
`[target.window]` part is unavailable, and a target that also has a
`[target.runtime]` part uses that. `revier attach`, `revier popup` and
`revier keys` do not work. Workspaces, agent state and the TUI in a terminal
still work.

**The key launches a second window.** `match` does not find the window that
`launch` opened. Compare it with `wctl list --json`. Give a browser window a
`--class` of its own.
