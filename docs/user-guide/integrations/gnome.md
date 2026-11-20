# GNOME

GNOME is the window host. Through it, revier lists the desktop's windows,
raises, places and closes them, and reads which one has the focus. A target
can then be any desktop window: an editor, a browser page, a diff viewer.
revier also writes its desktop keys into GNOME, and the popup needs it.

## What you need

- The [Window Control extension](https://github.com/carlo9890/gnome-window-control),
  switched on, and its command `wctl` 0.23.0 or later on your PATH. GNOME
  gives no window list to an outside program; `wctl` does. The popup's
  window rules need `wctl` 0.24.0 and extension 24 or later.
- `revier` on the PATH of a login shell. Each desktop key runs it through
  `sh -lc`.

revier uses GNOME when `XDG_CURRENT_DESKTOP` names GNOME (or is not set) and
`wctl version --json` names a running extension. Else it does what
[`[hosts]`](../configuration.md#hosts) says.

## Set it up

1. Install the extension and `wctl`. Log out and in.
2. Check that `wctl version --json` shows `wctl` 0.23.0 or later and an
   `extension` version, and that `wctl list --json` prints your windows.
3. Give a target a window, as
   [Give your editor a key](../getting-started.md#give-your-editor-a-key)
   shows.
4. Run `revier setup install`, as
   [Install the desktop keys](../getting-started.md#install-the-desktop-keys)
   shows. `revier setup status` shows who holds each key. The trigger key is
   `revier: popup` in GNOME Settings.

## The popup's window

`revier setup install` writes three rules for the popup's window into a file
of its own, `~/.config/gnome-window-control/rules.d/revier.json`. It never
edits your `rules.json`. The rules set the popup's size by the width of the
monitor it opens on:

| Monitor width | Popup |
|---|---|
| 3400 px or more | 1800 px wide, centred |
| 2200 px or more, such as a 29 inch wide screen | 75% of the width, centred |
| less | the whole screen |

The rules also keep the popup over every other window and out of Alt+Tab, and
hide it when another window takes the focus or the overview opens.

- The width is in logical pixels: the monitor's width divided by its scale.
- The extension sizes the window before it draws it on GNOME 49 or later
  under Wayland. On an older GNOME, and under X11, the popup opens and then
  moves into place.
- While the rules are as revier wrote them, revier does not move the popup
  itself. Else it places the popup by the same widths, but measures the
  primary monitor, less its panels.
- To size the popup your own way, add a rule for the class `revier-popup` to
  your `rules.json`. The extension reads `rules.json` before `rules.d`, so
  your rule wins over revier's. `revier setup status` and `revier setup
  install` name each rule of yours that wins, and install leaves it. Do not
  edit `revier.json`: the next install writes it again.
- An extension older than version 24 does not read `rules.d`, and
  `revier setup install` skips the rules and says so. Upgrade the extension
  and `wctl`. `revier setup uninstall` deletes `revier.json`, with or without
  a running extension.
- A hide by the extension acts as Esc: the next press raises the popup on the
  project list, with no query. An error the popup showed stays. A screen or a
  question the popup showed, such as the help or a delete, still waits for
  you.

## Windows that open late

Many applications open their window a while after the command starts. revier
binds the one new window whose class matches the target within 60 seconds,
even before its title matches.

An editor that starts cold can take minutes, and open the windows of several
projects at once and in any order. While a TUI is open, revier keeps each such
launch for 10 minutes and binds it to the window whose title matches that
target's `match` and no other target's. Until then, a new window of that class
is bound only to a target whose full `match` finds it, not by its class alone.

An action can open a window too, such as a browser page. While a TUI is open,
the one new window that appears in the first seconds after the action, and
that no target matches, is attached to the project, as `revier attach` does.

To give a project a window no target describes, focus it and run
`revier attach -p demo`.

**The key launches a second window.** `match` does not find the window that
`launch` opened. Compare it with `wctl list --json`. Give a browser window a
`--class` of its own.

## Without GNOME

On another desktop, or with no display, a target with only a
`[target.window]` part is unavailable, and a target that also has a
`[target.runtime]` part uses that. `revier attach` and `revier popup` do not
work, and `revier setup` writes no desktop keys and no rules. Workspaces,
agent state and the TUI in a terminal still work.
