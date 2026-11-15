# GNOME

GNOME is the window host. Through it, revier lists the desktop's windows,
raises, places and closes them, and reads which one has the focus. A target
can then be any desktop window: an editor, a browser page, a diff viewer.
revier also writes its desktop keys into GNOME, and the popup needs it.

## What you need

- The [Window Control extension](https://github.com/carlo9890/gnome-window-control),
  switched on, and its command `wctl` 0.23.0 or later on your PATH. GNOME
  gives no window list to an outside program; `wctl` does.
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
   `revier: popup` in GNOME Settings; an install renames one that an older
   revier wrote as `revier: picker`.

## The popup's window

`revier setup install` writes three rules for the popup's window into
`~/.config/gnome-window-control/rules.json`, above your own rules. They set
its size by the width of the monitor it opens on:

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
- A rule of yours for `revier-popup` that keeps the popup on top is
  revier's: install replaces it. Any other rule of yours for it holds the
  install back, and `revier setup install --force` replaces it. Install and
  uninstall print each rule they removed that revier does not write, as the
  file held it, for you to put back.
- An extension older than version 23 cannot read the rules, and
  `revier setup install` skips them and says so. Upgrade the extension.
  `revier setup uninstall` still removes them, with or without a running
  extension.
- A hide by the extension acts as Esc: the next press raises the popup on the
  project list, with no query. An error the popup showed stays. A screen or a
  question the popup showed, such as the help or a delete, still waits for
  you.

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
`[target.runtime]` part uses that. `revier attach` and `revier popup` do not
work, and `revier setup` writes no desktop keys and no rules. Workspaces,
agent state and the TUI in a terminal still work.

**The key launches a second window.** `match` does not find the window that
`launch` opened. Compare it with `wctl list --json`. Give a browser window a
`--class` of its own.
