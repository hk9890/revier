# Getting started

This page takes you from install to an agent that waits for you. Three words
first. A **target** is a thing you reach with a key: the project's
terminal workspace, an editor, a web page. The **home** target is the
workspace: the terminal window you return to. The **TUI** is revier's
full-screen view in a terminal. [Concepts](concepts.md) explains the rest.

## What you need

- Linux, `x64` or `arm64`.
- kitty, tmux or herdr. revier opens your workspaces in it.
- Claude Code, in the version
  [Claude Code](integrations/claude-code.md#what-you-need) names, as `claude`
  on your `PATH`. The project that `revier new` writes runs it in its agent
  tab.
- For window targets, desktop keys and the popup: GNOME with the Window
  Control extension and its `wctl`. See [GNOME](integrations/gnome.md) to
  install it. The popup also needs kitty.

## Install

With [mise](https://mise.jdx.dev):

```bash
mise use -g github:hk9890/revier
```

Or extract `revier_<version>_linux_<arch>.tar.gz` from the
[releases page](https://github.com/hk9890/revier/releases) onto your PATH,
for example with `tar -xzf <file> -C ~/.local/bin`.

### Verify a download

Before you extract an archive, download the release's checksums file with its
`.sig` and `.pem` beside it, then run:

```bash
cosign verify-blob \
  --certificate revier_<version>_checksums.txt.pem \
  --signature  revier_<version>_checksums.txt.sig \
  --certificate-identity "https://github.com/hk9890/revier/.github/workflows/release.yml@refs/tags/v<version>" \
  --certificate-oidc-issuer "https://token.actions.githubusercontent.com" \
  revier_<version>_checksums.txt
sha256sum -c revier_<version>_checksums.txt --ignore-missing
```

The first command proves the release workflow signed the checksums. The second
proves the archive matches them.

## Set up your terminal

- **kitty**: give kitty a control socket, as step 1 of
  [kitty](integrations/kitty.md#set-it-up) shows, then restart kitty. Without
  it, revier starts a kitty of its own.
- **tmux**: nothing to set in tmux. When kitty is also installed, tell revier
  to use tmux; see [tmux](integrations/tmux.md).
- **Ghostty**: revier cannot drive it; run tmux in it, as
  [Ghostty](integrations/ghostty.md) shows.

## Give your editor a key

Skip this step without GNOME. A target in `~/.config/revier/config.toml` is
shared: every project gets it, and its key works in every project. Write your
editor there before you make a project:

```toml
[[target]]
name = "editor"
key  = "ctrl-shift-o"
  [target.window]
  launch = ["code", "{{.Path}}"]                       # {{.Path}}: the project's directory
  match  = { class = "^[Cc]ode$", title = "{{.Name}}" } # how revier finds the open window
```

This is VS Code. For another editor, change `launch` and `match`
([Targets](configuration.md#targets)); `wctl list --json` shows each window's
class. This guide writes the key `ctrl-shift-o` as Ctrl+Shift+O.

## Make your first project

Go to a project directory and run `revier new`:

```bash
cd ~/dev/demo
revier new      # created /home/you/.config/revier/projects/demo.toml
```

The project takes the name of the directory. The file declares `home`, a
terminal window called `session:demo`, and `agent`, a tab in it with Claude
Code beside a shell. Your shared `editor` is the third target.

Run `revier status` to check which hosts revier found: the `runtime` line
names the terminal host, the `window` line gnome, and `-` means none.

## Open it

Run `revier open`. revier opens the workspace with the `agent` tab. Run it
again, and revier raises the same window instead of a second one. This is
run-or-raise. From another directory, run `revier open demo`. On tmux or herdr,
`revier open --attach` puts your terminal on the workspace.

## Install the desktop keys

revier writes its keys into GNOME's keyboard shortcuts. See what an install
changes first:

```
$ revier keys install --dry-run
=== revier keys install ===

  dry run: nothing is changed

  - Alt+Space     revier: popup   skipped: org.gnome.desktop.wm.keybindings activate-window-menu holds it
  > Ctrl+Shift+O  revier: editor  would be created

  keybindings: 1 of 2 would be installed
  1 key is held by something else. --force takes it.
```

`revier: popup` is the **trigger key**. It opens the popup: the TUI in a
kitty window of its own. On a stock GNOME, the window menu holds Alt+Space.
Do one of these:

- Run `revier keys install --force`. revier takes Alt+Space, and the plan
  prints how to give it back.
- Set `trigger_key` in [`[ui]`](configuration.md#ui) to a free key, then run
  `revier keys install`.

`revier keys uninstall` removes only what revier wrote.

## Go to a target and back

Focus the workspace and press Ctrl+Shift+O. revier opens the project's editor,
or raises it. Press Ctrl+Shift+O again in the editor, and you are back in the
workspace. This is toggle-back. From a shell, run `revier go editor`.

## See an agent that needs you

In the `claude` panel of the agent tab, type `create the file hello.txt`.
Claude Code asks for permission to write it. Do not answer. Press the trigger
key, or run `revier` in a terminal.

The project with an agent that needs you is at the top, and the pane shows
the agent's terminal. For the list of all agents, press the trigger key
again in the popup, or Alt+Space in a terminal. Select the agent and press
Enter. revier raises its tab and window.

From a shell, `revier list` prints one line per project with its most urgent
agent state: `attention` means it needs you.

## Next

Read [Concepts](concepts.md) once. Then see [The TUI](tui.md),
[Projects](projects.md) and, when something does not work,
[Troubleshooting](troubleshooting.md).
