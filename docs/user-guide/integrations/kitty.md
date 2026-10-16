# kitty

This page is for you if kitty is your terminal. It tells you what revier does
in kitty, what to set in `kitty.conf`, and what works and what does not.

## What revier does with kitty

kitty is a runtime host: the terminal that holds a project's workspace and
its agents. revier opens each workspace as a kitty OS window, opens tabs and
split windows in it, and reads every kitty window inside it to find the agents
and their state. It drives kitty through kitty's remote control, with
`kitten @`. kitty is also the terminal the popup runs in, whatever your
runtime host is.

## What you need

- kitty, with `kitten` on your PATH or beside the `kitty` binary. revier is
  checked against kitty 0.48.
- `XDG_RUNTIME_DIR` set to an absolute path. A desktop login sets it. revier
  starts a kitty only with its control socket in that directory, because only
  you can enter it.
- For the popup and for raising a kitty window: GNOME and `wctl`, as
  [GNOME](gnome.md) says. Without them, workspaces still open in kitty, but
  under Wayland revier cannot bring a kitty window to the front.

## How revier picks kitty

With no `[hosts]` table in `~/.config/revier/config.toml`, revier tries kitty
first, then tmux. kitty is used when `kitten` is found and either a kitty
control socket answers or revier finds no kitty socket at all. When kitty
sockets exist but none answers, revier skips kitty and tries tmux.

To make kitty the only choice, name it:

```toml
[hosts]
runtime = ["kitty"]
```

A list is a preference order. revier tries each name in turn and stops at the
first that works. When none works, revier stops with an error that names each
one. It does not fall back to a host you did not list.

To use no runtime host at all, even where kitty is installed:

```toml
[hosts]
runtime = ["none"]
```

You can also change the runtime host in the TUI: `alt+c` opens the config
screen, and the `runtime` row steps through `auto`, `kitty`, `tmux` and
`none`. revier checks the host before it writes the choice.

## Set it up

1. Let revier reach every kitty you start. Add these two lines to
   `~/.config/kitty/kitty.conf`:

   ```
   allow_remote_control socket-only
   listen_on unix:${XDG_RUNTIME_DIR}/kitty
   ```

   Each kitty process then listens on its own socket,
   `$XDG_RUNTIME_DIR/kitty-<pid>`, which only you can reach. revier finds
   every such socket. A kitty on the older abstract socket `@kitty-<pid>`
   (`listen_on unix:@kitty`) is still found, but every user of the machine
   can connect to that socket.

2. Restart kitty, so the settings apply.

3. Open a project:

   ```bash
   revier open demo
   ```

   revier opens the workspace in the kitty you ran the command in, or in the
   first kitty that answers. When no kitty answers, revier starts one, with a
   control socket of its own and remote control switched on.

4. Optional: add the keys that open a tab in the workspace you are in. See
   [Add tabs from a kitty key](#add-tabs-from-a-kitty-key).

Without step 1, revier still works: it starts a kitty of its own for the
workspaces, and your other kitty windows stay out of its reach.

## What a workspace is in kitty

A workspace is one kitty OS window. revier gives it the name of the target's
runtime `name`, for example `session:demo`, as both its window name and its
title. That name is how revier finds the window again, and how GNOME sees it.

Inside the OS window:

- A tab target (`inside = "home"`) is a kitty tab. The `agent` tab that
  `revier new` writes holds Claude Code and a shell, split side by side.
- Each panel of a tab is a kitty window. revier reads each one: the program in
  the foreground, its title, and the user variables revier set on it.
- `revier agent new` and `revier shell new` add a tab after the last one.

revier reads a panel by the program nearest to the panel's own process that
is not a shell. A tool that an agent starts, or a `wl-copy` that stays in the
background, does not hide the agent.

A kitty window that revier did not open, and a window kitty opened from a
session file, carry kitty's default name. Such a window is no project's
workspace. To give it to a project, focus it and run `revier attach`, as
[GNOME](gnome.md) says. revier then reads the agents in it only when it can
pair the window with its kitty beyond doubt: when it is the one window of its
kitty process that revier did not name. Two such windows in one kitty process
cannot be told apart, and the window is attached alone.

A kitty OS window does not keep its contents after kitty exits. To get the
open projects back after a restart, use `revier session save` and
`revier session restore`; see [Projects](../projects.md).

## Place a workspace window

On GNOME, a runtime target in kitty can say where its new OS window goes,
with `place` on the runtime realization:

```toml
[[target]]
name = "home"
home = true

  [target.runtime]
  name  = "session:{{.Name}}"
  match = { title = "^session:{{.Name}}$" }
  tabs  = ["agent"]
  place = "right top 75% 100%"
```

`place` applies to a window revier opens, never to one it raises.
[GNOME](gnome.md) explains the tokens.

## Add tabs from a kitty key

`revier agent new` and `revier shell new` take `--panel <id>`: the kitty
window the key was pressed in. kitty passes that id as
`@active-kitty-window-id`. `--copy-env` gives revier the `KITTY_LISTEN_ON` of
the kitty the key was pressed in, because every kitty process counts its
windows from 1.

Add an agent tab, with the agent and a shell, to the workspace you are in:

```
map ctrl+shift+z launch --type=background --copy-env --cwd=current sh -lc 'exec revier agent new --panel "$1" --dir .' sh @active-kitty-window-id
```

Add a shell tab. `revier shell new` refuses a window that is not a project's
workspace, so this map falls back to kitty's own new tab there:

```
map ctrl+shift+t launch --type=background --copy-env --cwd=current sh -lc 'revier shell new --panel "$1" --dir . || kitten @ launch --type=tab --cwd=current' sh @active-kitty-window-id
```

The fallback `kitten @ launch` needs remote control on, as in
[Set it up](#set-it-up).

## The popup

`revier popup` is the TUI in a kitty window of its own, with the class
`revier-popup` and the title `revier`. The desktop's trigger key, `Alt+Space`
by default, runs it. It needs kitty on your PATH and the GNOME window host,
whatever your runtime host is. When one of them is missing, `revier popup`
names it.

- A new popup is centred, at the full height of the screen and 1800 px wide,
  or fills a screen narrower than that.
- A popup already open is raised where you left it.
- `Esc` hides it. The next press brings it back with the cursor and the
  filter where they were.
- Pressed while the popup has the focus, the trigger key switches between the
  projects and the agent list. The same works on a revier you run in a kitty
  window of your own.

The popup's kitty gets its own control socket in `XDG_RUNTIME_DIR`, and the
targets you open from the popup open through it. It takes
`allow_remote_control` from your `kitty.conf`. With remote control off, the
trigger key cannot switch the popup's list, and the targets open in another
kitty. The class lets you write a compositor rule for the popup window.

## What works and what does not

| Works in kitty | Does not |
|---|---|
| Workspaces as OS windows, tabs and split windows | Keeping a window's contents after kitty exits |
| Agent state in every workspace, across every kitty process | Seeing agents in an attached kitty window that shares its kitty process with another unnamed one |
| `revier agent read --screen`, `agent prompt`, `agent send-keys` | Raising an OS window under Wayland without the GNOME host |
| The popup, and the trigger key on a revier in your own kitty | `revier open --attach`, which takes tmux |
| `place` on a new workspace window, with GNOME | |

## Troubleshooting

**revier opens a second kitty instead of using mine.** Your kitty has no
control socket revier can reach. Add `allow_remote_control` and `listen_on`
as in [Set it up](#set-it-up), and restart kitty.

**"kitty is running but no control socket answers".** A kitty socket exists,
but `kitten @ ls` fails on it. Check that `allow_remote_control` is on in
`kitty.conf`, and that the kitty still runs.

**"XDG_RUNTIME_DIR is not set to an absolute path".** revier starts no kitty
without a private directory for its socket. Run revier from a desktop login,
which sets the variable. A workspace in a kitty that already answers still
opens.

**"kitten not on PATH and not beside kitty".** Put `kitten` on your PATH, or
install kitty so that `kitten` sits next to the `kitty` binary.

**A key opens nothing, and the error says a window is in several kitty
processes.** The id from `@active-kitty-window-id` exists in more than one
kitty. Add `--copy-env` to the `launch` in the map, so revier knows which
kitty the key came from.

**The window does not come to the front.** Under Wayland, kitty cannot raise
its own window. revier raises it through GNOME. Check that `wctl` works; see
[GNOME](gnome.md).

**A script cannot reach a kitty that revier started.** Its control socket is
`unix:$XDG_RUNTIME_DIR/kitty-<pid>`. Pass that address to `kitten @ --to`.

For anything else, start with `revier doctor` and
[Troubleshooting](../troubleshooting.md).
