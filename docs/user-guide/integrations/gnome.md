# GNOME

GNOME is the window host. Through it, revier lists the desktop's windows,
raises, places and closes them, and reads which one has the focus. A target
can then be any desktop window: an editor, a browser page, a diff viewer.
revier also writes its desktop keys into GNOME, and the popup needs it.

## What you need

- The [Window Control extension](https://github.com/carlo9890/gnome-window-control),
  switched on, and its command `wctl` 0.16.0 or later on your PATH. GNOME
  gives no window list to an outside program; `wctl` does.
- `revier` on the PATH of a login shell. Each desktop key runs it through
  `sh -lc`.

revier uses GNOME when `XDG_CURRENT_DESKTOP` names GNOME (or is not set) and
`wctl version --json` names a running extension. Else, with no `window` list
in [`[hosts]`](../configuration.md#hosts), it runs with no window host and
says nothing.

## Set it up

1. Install the extension and `wctl`. Log out and in.
2. Check that `wctl version --json` shows `wctl` 0.16.0 or later and an
   `extension` version, and that `wctl list --json` prints your windows.
3. Give a target a window, as
   [Give your editor a key](../getting-started.md#give-your-editor-a-key)
   shows.
4. Claim the keys, as
   [Install the desktop keys](../getting-started.md#install-the-desktop-keys)
   shows. `revier keys status` shows who holds each key. The trigger key is
   `revier: popup` in GNOME Settings; an install renames one that an older
   revier wrote as `revier: picker`.

## Windows that open late

Many applications open their window a while after the command starts. revier
binds the one new window whose class matches the target within 60 seconds,
even before its title matches.

An action can open a window too, such as a browser page. While a TUI is open,
the one new window that appears in the first seconds after the action, and
that no target matches, is attached to the project, as `revier attach` does.

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
