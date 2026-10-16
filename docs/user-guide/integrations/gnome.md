# GNOME

This page is for you if your desktop is GNOME. It tells you how revier reaches
desktop windows, how it claims the desktop keys, and what you lose without
GNOME.

## What revier does with GNOME

GNOME is the window host: revier lists the desktop's windows, raises one,
places a new one, closes one, and reads which one has the focus. With it, a
target can be any desktop window: an editor, a browser page, a diff viewer.
GNOME is also where revier writes its desktop keys, and the popup needs it.

GNOME gives no window list to an outside program, so revier reaches the
windows through the
[Window Control extension](https://github.com/carlo9890/gnome-window-control)
and its command `wctl`.

## What you need

- GNOME Shell with the Window Control extension installed and switched on.
- `wctl` on your PATH. With `wctl` 0.13.0 or newer, the popup hides and comes
  back without the shell's animation. An older `wctl` works with the
  animation.
- For the desktop keys: `gsettings` and `dconf` on your PATH. GNOME ships
  both.

## How revier picks GNOME

GNOME is the only window host. With no `[hosts]` table in
`~/.config/revier/config.toml`, revier uses it when all of these hold:

- `wctl` is on your PATH;
- `XDG_CURRENT_DESKTOP` names GNOME, or is not set;
- `wctl list --json` answers.

When one fails, revier runs with no window host, and says nothing. To make a
missing GNOME an error instead, name it:

```toml
[hosts]
window = ["gnome"]
```

To keep revier away from your windows, even on GNOME:

```toml
[hosts]
window = ["none"]
```

The config screen (`alt+c`) shows the window host in use. It is chosen when
revier starts, and the screen cannot change it.

## Set it up

1. Install the Window Control extension and `wctl`, as its project page says.
   Log out and in, so GNOME loads the extension.
2. Check that `wctl` answers:

   ```bash
   wctl list --json
   ```

3. Give a target a desktop window. See
   [Window targets](#window-targets).
4. Claim the desktop keys. See [Claim the desktop keys](#claim-the-desktop-keys).

## Window targets

A window target has a `[target.window]` part: the command that opens the
window, and the rule that finds it.

```toml
[[target]]
name = "editor"
key  = "ctrl-shift-o"

  [target.window]
  launch = ["code", "{{.Path}}"]
  match  = { class = "^code$", title = "{{.Name}}" }
```

`match` takes `class` and `title`, each a regular expression. Every field you
set must match, and a field you leave out matches anything. `class` is the
window's WM class, as `wctl list --json` shows it in `wm_class`.

When you press the key, revier looks for a window that matches. When it finds
one, it raises it. When it finds none, it runs `launch`. Pressed again while
that window has the focus, the key returns you to the project's home.

`launch` must open a window that `match` can find. A browser window has no
class of its own, so give it one:

```toml
[[target]]
name = "pulls"
key  = "ctrl-g"

  [target.window]
  launch = ["google-chrome", "--app=https://github.com/example/demo/pulls",
            "--class=revier-{{.Name}}-pulls"]
  match  = { class = "^revier-{{.Name}}-pulls$" }
```

A target with both a `[target.window]` and a `[target.runtime]` part uses the
window on GNOME, and the runtime part where there is no window host. Set
`prefer = "runtime"` on the target to use the runtime part on GNOME too.

## Place a new window

`place` on a realization says where a new window goes: four words, for x, y,
width and height, in pixels or in words relative to the screen's usable area.
`wctl` reads them.

```toml
  [target.window]
  launch = ["meld", "{{.Path}}"]
  match  = { class = "^meld$" }
  place  = "right top 75% 100%"
```

revier places a window it opened, and never one it raised: where you moved a
window is yours. A window that is maximized, full screen or tiled keeps its
geometry, and the key still works. `place` also works on a kitty workspace;
see [kitty](kitty.md).

## Claim on appear

Many applications open their window a while after the command starts, and
some hand the window to a process that is already running. revier does not
need the launch to report a window. It watches for the window to appear:

- After a target's launch, the one new window that matches the target's
  rule, or its `class`, within 60 seconds is bound to the target. The next
  press raises that window, even when its title does not match yet.
- After an action's launch, the one new window within 5 seconds that no
  project's rule matches is attached to the project.

When two new windows could be the one, revier binds neither. A wrong binding
would send a key to a window of another project.

## Attach a window by hand

To give a project a window that no target describes, focus the window and
run:

```bash
revier attach -p demo
```

Without `-p`, revier takes the project of the current directory, then the
project of the focused window, then the project it acted on last. A desktop
key has no useful directory, so give `-p` there. An attached window appears
in the TUI under its project, and `del` there closes it. A saved session does
not record it: it has no name to be opened by again.

When the window is a kitty window, revier also attaches the terminal in it,
when it can pair the two beyond doubt, and then reads the agents in it.
## Claim the desktop keys

revier is one process per keypress: a desktop key runs a revier command and
exits. revier writes its keys as GNOME custom shortcuts:

| Key | Runs |
|---|---|
| The trigger key, `Alt+Space` by default | `revier popup` |
| Each target's `key` | `revier go <target> --picker` |

Each shortcut runs its command through `sh -lc`, so `revier` must be on the
PATH of a login shell.

`--picker` opens the popup when the focused window belongs to no project, so
a key pressed there still does something. Without it, `revier go` exits with
status 3 there, for scripts.

The trigger key is `trigger_key` in `config.toml`:

```toml
[ui]
trigger_key = "alt-space"
```

You can also set it on the config screen (`alt+c`).

### See who holds each key

```bash
revier keys status
revier keys status --json
```

This only reads. Each key has one status:

| Status | Means |
|---|---|
| `active` | revier holds the key, and it runs what revier would install |
| `stale` | revier holds the key, but it runs something else: an older install |
| `inert` | revier's shortcut is in GNOME's store but switched off. GNOME's settings do not show it. |
| `taken` | another custom shortcut holds the key |
| `builtin` | a GNOME default holds the key |
| `free` | nothing holds the key |

A key two targets ask for, or a target on two keys, is marked `(conflict)`,
and the lines under the table say which. Shortcuts revier holds and no longer
wants are listed as orphaned.

### Install

See what an install would change. This changes nothing:

```bash
revier keys install --dry-run
```

Then install:

```bash
revier keys install
```

Without `--force`, install adds only the keys nothing else holds, and names
the rest as skipped. It exits with status 4 when it did not install every key.

`--force` takes a key from whatever holds it:

- Another custom shortcut is switched off. It keeps its command, so the tool
  that wrote it can switch it on again.
- A GNOME default loses that one key, and keeps any other key it has. revier
  prints the `gsettings reset` line that gives the key back, beside the key.

On a stock GNOME, `Alt+Space` opens the window menu, so expect that key to be
skipped without `--force`.

### Uninstall

```bash
revier keys uninstall --dry-run
revier keys uninstall
```

Uninstall removes the shortcuts revier wrote, and nothing else. It does not
give back what `--force` took from another tool: that tool does. A GNOME
default comes back with the `gsettings reset` line install printed.

## The popup

The popup is the TUI in a kitty window that revier places on the screen. It
needs GNOME, `wctl` and kitty. [kitty](kitty.md) describes it.

## Without GNOME

On another desktop, on a machine with no display, or with
`window = ["none"]`, revier has no window host:

- A target with only a `[target.window]` part is unavailable. Its key says
  that no host can open it.
- A target with a `[target.runtime]` part uses that part.
- `revier attach` stops with "attach needs a window host".
- `revier popup` names what is missing.
- `revier keys` reports that there is no desktop to read, and changes
  nothing.
- Under Wayland, revier cannot raise a kitty window. It focuses it inside
  kitty.

Workspaces, agent state, the TUI in a terminal and every `revier agent`
command still work.

## What works and what does not

| Works on GNOME | Does not |
|---|---|
| Window targets: raise, launch, close, place | Placing a window revier raised rather than opened |
| Claim on appear, for a target and for an action | Binding a window when two new ones could be it |
| `revier attach` | Recording an attached window in a session |
| The desktop keys, and the popup | Giving back what `--force` took from another tool |
| Raising a kitty workspace under Wayland | Raising the terminal that shows a tmux session |

## Troubleshooting

**The popup or a window target says there is no window host.** Check each
condition in [How revier picks GNOME](#how-revier-picks-gnome). Run
`wctl list --json`: when it fails, the extension is off or not loaded. Log
out and in after you install it.

**"no configured window host is usable".** You named `gnome` in `[hosts]`
and it failed its check. The error says which check.

**A key does nothing.** Run `revier keys status`. `taken` and `builtin` mean
something else holds it; `inert` means revier's shortcut is switched off;
`stale` means an older install. Run `revier keys install`, with `--force`
for a key something else holds. Check that `sh -lc 'revier version'` works,
since the shortcut runs revier that way.

**The key launches a second window.** `match` does not find the window
`launch` opened. Compare `class` and `title` with what `wctl list --json`
reports for the window, and give a browser window a `--class`.

**The window opens in the wrong place.** `place` applies only to a window
revier opened. A maximized, full-screen or tiled window keeps its geometry.

For anything else, start with `revier doctor` and
[Troubleshooting](../troubleshooting.md).
