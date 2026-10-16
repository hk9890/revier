# Getting started

From nothing installed to an agent that waits for you, in one path. Each step
shows the command and what you see. [Concepts](concepts.md) explains the words
this page uses.

## What you need

- Linux. revier ships for `x64` and `arm64`.
- A terminal revier can drive: kitty or tmux. revier calls this the runtime
  host. It uses the first one it finds: kitty, then tmux.
- For windows, the popup and the desktop keys: GNOME with the
  [Window Control extension](https://github.com/carlo9890/gnome-window-control)
  and its `wctl`. The popup also needs kitty. Without GNOME, everything that
  lives in a terminal still works.
- For agent state: Claude Code 2.1.212 or newer. opencode is recognised, but
  its state shows as unknown.

## Install

With [mise](https://mise.jdx.dev), which reads the releases page and puts
`revier` on your PATH:

```bash
mise use -g github:hk9890/revier
```

Or download an archive from the
[releases page](https://github.com/hk9890/revier/releases). Archives are named
`revier_<version>_linux_<arch>.tar.gz`, with `x64` or `arm64` as the
architecture. The archive holds the `revier` binary alone. Put it on your PATH:

```bash
tar -xzf revier_<version>_linux_x64.tar.gz -C ~/.local/bin
```

Check what is installed:

```bash
revier version
```

### Verify a download

Each release ships a checksums file, signed by its release workflow at the
release's tag. To verify an archive, download the checksums file and its
`.sig` and `.pem` beside the archive, then run:

```bash
cosign verify-blob \
  --certificate revier_<version>_checksums.txt.pem \
  --signature  revier_<version>_checksums.txt.sig \
  --certificate-identity "https://github.com/hk9890/revier/.github/workflows/release.yml@refs/tags/v<version>" \
  --certificate-oidc-issuer "https://token.actions.githubusercontent.com" \
  revier_<version>_checksums.txt
sha256sum -c revier_<version>_checksums.txt --ignore-missing
```

The first command proves the checksums file came from the release workflow.
The second proves the archive matches it. Do this before you extract the
archive.

## First launch

Run revier with no arguments:

```bash
revier
```

This is the TUI. With no project yet, the list says where a project goes:

```
  No projects configured. Add one as
  ~/.config/revier/projects/<name>.toml
```

Press `ctrl+c` to quit. You can add a project from the TUI with `alt+n`, but
the next step does it from the shell.

## Make your first project

Go to the directory of a project and run `revier new`:

```bash
cd ~/dev/demo
revier new
```

```
created /home/you/.config/revier/projects/demo.toml
```

The project takes the directory's name. To give it another, run
`revier new <name>`. revier refuses your home directory, and a directory that
is already a project.

The file it writes declares three targets:

- `home`: the workspace. A terminal window called `session:demo`.
- `agent`: a tab inside the workspace, with Claude Code (`claude`) beside a
  shell.
- `editor`: an IntelliJ IDEA window. Change its `launch` and `match` to your
  own editor. [Projects](projects.md) shows how.

If the directory is a git checkout, the file also records its origin as
`git_url`. When the directory is missing later, `revier open` clones it from
there. If `mise` is installed, `revier new` also runs `mise trust` on the
directory, so the first shell there starts without a prompt.

## Open it

From inside the project directory:

```bash
revier open
```

```
demo: session:demo (kitty/<id>)
```

revier opens the workspace: a new kitty window with the `agent` tab, Claude
Code on one side and a shell on the other. Run `revier open` again and revier
raises the same window. It does not open a second one. This is run-or-raise.

From anywhere else, name the project: `revier open demo`. A name revier does
not know yet becomes a new project for the current directory, as `revier new`
would make it.

On tmux, the workspace is a tmux session. `revier open --attach` ends with your
terminal attached to it.

## Claim the desktop keys

revier is one process per keypress. A desktop key runs a revier command and
exits. Before the keys can do that, revier writes them into GNOME's keyboard
shortcuts.

First, give a target a key. Open `~/.config/revier/projects/demo.toml` and add
a `key` line to the `editor` target:

```toml
[[target]]
name = "editor"
key  = "ctrl-shift-o"
```

Then see what an install would change. This changes nothing:

```bash
revier keys install --dry-run
```

```
=== revier keys install ===

  dry run: nothing is changed

  - Alt+Space     revier          skipped: org.gnome.desktop.wm.keybindings activate-window-menu holds it
  > Ctrl+Shift+O  revier: editor  would be created

  keybindings: 1 of 2 would be installed
  1 key is held by something else. --force takes it.
```

`Alt+Space` opens the popup: the TUI in a kitty window of its own. It is the
trigger key, `[ui] trigger_key` in `config.toml`. Every other line is a
target's `key`, bound to `revier go <target> --picker`. The name in the second
column is what GNOME's keyboard settings show for the shortcut.

Without `--force`, install adds only the keys nothing else holds, and names
the rest as `skipped`. On a stock GNOME, `Alt+Space` opens the window menu, as
above. To take it, add `--force`:

```bash
revier keys install --dry-run --force
revier keys install --force
```

`--force` takes a key from whatever holds it. Another shortcut is switched off
and keeps its command, so the tool that wrote it can put it back. A GNOME
default loses that one key and keeps any other it holds, and the plan prints
the `gsettings reset` line that gives it back. To keep the window menu, run
`revier keys install` without `--force` instead, or set another
`trigger_key`.

`revier keys status` shows who holds each key, and changes nothing.
`revier keys uninstall` removes what revier wrote and nothing else. It restores
no other tool's shortcuts: that tool does.

## Jump to a target and back

Put the focus on the workspace and press `ctrl+shift+o`. revier finds the
project from the focused window and runs-or-raises its `editor`: the editor
opens the first time, and comes to the front every time after.

Press `ctrl+shift+o` again while the editor has the focus. You are back in the
workspace. This is toggle-back: a second press of a target's key returns you to
the project's home.

The same key works in every project that declares an `editor`, and it opens
that project's editor. When the focused window belongs to no project, revier
uses the project it acted on last. When there is none, `--picker` opens the
popup, so you can choose one.

From a shell, the same jump is:

```bash
revier go editor
revier go editor -p demo
```

## See an agent wait for you

Give the agent in the workspace a task that needs your permission, for example
a command it must ask to run. Then press `Alt+Space`, or run `revier` in any
terminal.

The project with an agent that waits for you sorts to the top. Its row counts
its agents by state: needs you, working, idle, unknown. The pane beside the
list shows the project's targets and its agents, and the terminal of the agent
that needs you, as it is now.

Press `alt+space` again on the TUI to see the agent list: every agent of every
open project, the ones that need you first. Move the cursor to the agent and
press `Enter`. revier brings its tab to the front and raises its window. The
popup hides.

The same answer, printed once:

```bash
revier list
```

```
PROJECT  STATE    AGENT      TARGETS
demo     running  attention  *home *agent  editor
```

`attention` is the state the TUI calls "needs you". A `*` marks a target that
is open.

## Where next

- **[Concepts](concepts.md)**: projects, targets, run-or-raise, agent states,
  and what degrades. Read it once.
- **[The TUI](tui.md)**: the list, the pane, the popup, and every key.
- **[Projects](projects.md)**: project files, shared targets, actions,
  sessions and shutdown.
- **[Agents](agents.md)**: drive an agent from a script with `revier agent`,
  and `revier assist`.
- **[Links](links.md)**: projects on another machine.
- **[Configuration](configuration.md)**: every file, table and field revier
  reads.
- **[Command line](cli.md)**: every command, flag and exit code.
- **[Troubleshooting](troubleshooting.md)**: `revier doctor` first, then the
  symptoms.
- **Integrations**: [kitty](integrations/kitty.md), [tmux](integrations/tmux.md),
  [Ghostty](integrations/ghostty.md), [GNOME](integrations/gnome.md),
  [Claude Code](integrations/claude-code.md), [opencode](integrations/opencode.md).
