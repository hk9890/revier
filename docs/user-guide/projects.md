# Projects

This page is about the work you do with projects: write one, share targets
between projects, add actions, run one command in every project, save and
restore the set of open projects, shut everything down, and read back what
revier did. Every field of a project file is in
[Configuration](configuration.md); this page shows only what each task needs.

## Write a project

A project is one TOML file under `~/.config/revier/projects/`. The file name is
the project's name: `projects/shop.toml` is the project `shop`.

To make a project of the directory you are in, run:

```bash
cd ~/dev/shop
revier new            # the project is called shop, after the directory
revier new storefront # the same directory, called storefront
```

`revier new` prints the file it wrote. Without a name, the project takes the
directory's name, with each space and colon changed to `_`.

To make the project and open it in one step, give `revier open` a name revier
does not know yet:

```bash
cd ~/dev/shop
revier open shop
```

revier refuses to write a project when:

- the name is already a project;
- the directory is already the path of another project;
- the directory is your home directory, because that project would own every
  directory under it.

A name cannot hold whitespace, a `/` or a `:`. The colon separates a project
from its target in an agent address, such as `shop:agent`.

When the directory is a git checkout, `revier new` records its `origin` as
`git_url`, so the project can be cloned on the next machine. A URL with a
credential in it is not recorded. When `mise` is installed, revier also runs
`mise trust` on the directory, so the first shell there has the tools the
checkout pins.

In the TUI, alt+n does the same, and also takes a path or a git URL. See
[The TUI](tui.md).

## What a project file holds

With no shared targets in `config.toml`, `revier new` writes a workspace with
an agent tab, and an editor:

```toml
# shop: written by `revier new`.
path = "~/dev/shop"
git_url = "git@github.com:example/shop.git"

[[target]]
name = "home"
home = true
  [target.runtime]
  name = "session:{{.Name}}"
  match = { title = "^session:{{.Name}}$" }
  tabs = ["agent"]

[[target]]
name = "agent"
  [target.runtime]
  inside = "home"
    [[target.runtime.panels]]
    kind = "agent"
    title = "claude"
    command = ["claude"]
    [[target.runtime.panels]]
    kind = "shell"
    title = "shell"

[[target]]
name = "editor"
  [target.window]
  launch = ["snap", "run", "intellij-idea", "{{.Path}}"]
  match = { class = "^jetbrains-idea", title = "^{{.Name}}( |$)" }
```

- `path` is the project's directory. `git_url` is where it is cloned from when
  the directory is missing.
- `home` is the workspace: the terminal window that toggle-back returns to.
  `tabs` lists the tabs it opens with.
- `agent` is a tab of `home`: an agent panel beside a shell panel.
- `editor` is a window on the desktop.

No target gets a `key`. A key means the same target in every project, so you
choose it once, usually in `config.toml`. A target without a key is still
reached by its name: `revier go editor`, or the TUI.

When `config.toml` declares shared targets, `revier new` leaves out the
editor. When a shared target is the home, the file holds only `path` and
`git_url`.

Every field, and what each realization means, is in
[Configuration](configuration.md). After an edit, run `revier doctor`: it
names every file and target that did not load, with the edit that fixes it. A
mistake in one target disables only that target.

## Share a target across projects

A target most projects have, such as an editor, goes once into
`~/.config/revier/config.toml`, in the same form as in a project file. Every
local project then has it. A project file changes a shared target by
declaring a target of the same name with only the fields it changes: a table
merges key by key, and any other value replaces the shared one whole. A
target with a new name is the project's own.
[Shared targets](configuration.md#shared-targets) has the merge rule with a
worked example.

A shared target reaches a project on another machine only through its remote
part. See [Projects on another machine](links.md).

In the TUI, the config screen (alt+c) adds, changes and deletes shared
targets, and the project screen (alt+e) shows every target a project has,
the shared ones included.

## Add an action and run it

An action is a command that opens nothing to return to: a script, a sync, a
copy to the clipboard. Actions live in `config.toml` only:

```toml
[[action]]
key  = "ctrl-y"
name = "copy path"
run  = ["wl-copy", "{{.Path}}"]
```

`run` is an argument list, not a shell string, so there is nothing to quote.
`{{.Path}}` and `{{.Name}}` are the project's. The command runs in the
project's directory.

To run an action from a shell or a script:

```bash
revier run "copy path"             # in the project this directory resolves to
revier run "copy path" -p shop     # in the project shop
```

The action's output goes to your terminal, and its exit status is the exit
status of `revier run`. An unknown action name fails.

In the TUI, the action's `key` runs it on the selected project. An action key
is a TUI key: it needs ctrl or alt, and it cannot be one of the keys the TUI
uses itself, such as alt+space. A window that the action opens is claimed for
the project, as a target's window is.

For a project on another machine, `revier run` runs the action on that host,
so the action is one the host's `config.toml` defines.

## Clone a missing project

When a project's directory is missing and its file records a `git_url`,
opening the project clones it first:

```bash
revier open shop
# revier: /home/you/dev/shop is not on this machine; cloning git@github.com:example/shop.git
```

`revier open`, Enter on the project in the TUI, and the agent panel of a link
on its host clone a missing directory. Nothing else does: a survey or a
keypress on a target never clones. The clone runs in the foreground with
git's output on the terminal, and with no time limit. After the clone, revier
runs `mise trust` on the directory when `mise` is installed.

A missing directory with no `git_url` is an error that says so. Add the
`git_url` line to the project file.

## Rename a project

To rename a project, open the project screen in the TUI (alt+e, or a click on
the project's name atop the pane), change the name, and press Enter. revier:

1. moves the project file to the new name;
2. moves what it remembers about the project, such as hand-attached windows;
3. changes the name in every saved session, so an old session still opens
   the project.

The rename is refused:

- while the project is open, because its windows are found by titles that
  hold its name;
- when a target writes the name out in a `match` beside `{{.Name}}`. Change
  it to `{{.Name}}` first, or the target looks for the old name after the
  rename;
- when the project file is a symbolic link. Rename the file and the link by
  hand.

## Run one command in every project

`revier each` runs one command in the directory of every project, one project
at a time:

```bash
revier each -- git pull --ff-only
```

The command always follows `--`. Each project prints one line: `ok`,
`failed`, or why it was skipped. A summary follows, with the directory that
holds the output.

```
shop      ok         1.2s
platform  failed     exit 1, 0.4s, /home/you/.local/state/revier/runs/2026-10-16T09-12-03-41237/platform.log
far       remote     /home/user/dev/far
old       missing    /home/you/dev/old

1 ok, 1 failed, 2 skipped
failed:   platform
missing:  old
output:   /home/you/.local/state/revier/runs/2026-10-16T09-12-03-41237
```

A project is skipped when:

- its directory is missing (`missing`);
- it is on another machine (`remote`);
- an earlier project has the same directory (`duplicate`).

To run only where a shell test passes, give `--filter`. The test runs with
`sh -c` in each directory, and the command runs only where it exits 0:

```bash
revier each --filter 'test -d .git' -- git fetch
```

`--dry-run`, or `-n`, prints the selection and runs nothing.

The command's input is empty, so a command that asks a question ends instead
of waiting. Its output goes to a file per project, not to the terminal.
Projects run one after the other, never side by side: a pull asks one ssh
agent and one credential helper, and ninety at once would prompt ninety
times.

`revier each` exits 5 when the command failed in at least one project, and 1
when revier itself could not run.

### Read a past run

Every run is kept under `~/.local/state/revier/runs/<run>/`, one `.log` file
per project and a `summary.json`:

```bash
revier each log                 # past runs, newest first
revier each log <run>           # how each project ended in that run
revier each log <run> platform  # what the command printed in platform
```

## Save and restore sessions

A session is the set of projects that are open, and which of their targets.
Save one before a restart, and restore it after:

```bash
revier session save                  # record what is open now
revier session save --name work      # the same, with a label
revier session list                  # the saved sessions, newest first
revier session restore               # open the newest session again
revier session restore work          # the newest session labelled work
revier session restore 2026-10-16T09-12-03 --dry-run
```

`--dry-run` prints what a restore would do and opens nothing.
`revier session list --json` prints the sessions as JSON.

Sessions live in `~/.local/state/revier/sessions/`, one readable TOML file
each. The file name is the session id: the time it was saved.

### What a session records

A session records names, not windows. At restore, revier reads each project
file as it is then and opens the targets from it, so an edit you made since
the save wins. A restore:

- leaves a target that is already up as it is;
- names a project, target or host that is gone since the save, and steps
  over it;
- ends on the project that had the focus when you saved.

A window you bound by hand with `revier attach` is not recorded: it has no
name to be reopened by. `revier session save` says how many it left out.

### What comes back of an agent

Every agent comes back, not only the one the layout declares. An agent you
opened beside the workspace returns as the tab `revier agent new` opens. Each
agent starts in the directory it worked in. An agent whose worktree was
removed since the save starts fresh in the project.

An agent comes back on the conversation it held when its probe can name the
conversation. Claude Code can, with nothing to install: the save asks
`claude agents --json` which conversation each agent holds. An agent the save
cannot name, such as `claude` typed into a tmux shell, is reported when you
save, and comes back with no conversation. How the resume reaches the agent's
command is in [Agents](agents.md#resume-an-agent-on-its-conversation).

In the TUI, the sessions screen (alt+s) shows the same list. Its pane says
what a restore of the selected session would open now. Enter restores it, and
alt+s on the screen saves the open projects under an optional name.

## Shut everything down

`revier shutdown` closes every open target and hand-attached window, and the
agents in them:

```bash
revier shutdown                   # everything
revier shutdown shop              # one project
revier shutdown shop --agents     # only its agents; the workspaces stay
revier shutdown --targets         # only what holds no agent
revier shutdown --dry-run         # print the plan, close nothing
```

Before it closes anything, a shutdown saves the session when it changed since
the newest save, so `revier session restore` brings back what it closed.
`--no-session-save` skips the save.

While an agent is working or waiting for an answer, `revier shutdown` saves
nothing, closes nothing, and names the busy agent. `--force` shuts down
anyway.

A window whose application asks about unsaved work stays open, and the
shutdown names it. When you run `revier shutdown` from a terminal of a
workspace, that terminal closes last.

In the TUI, alt+q asks for every project or one, then for what of it, and
shows the plan before anything closes.

## See what revier did

`revier events` prints what revier did in each project, one JSON line per
event, oldest first:

```bash
revier events            # the last 7 days
revier events --days 3   # the last 3 days
revier events --local    # only this machine; ask no linked host
```

An event is one of these:

| `event` | What happened |
|---|---|
| `go` | A target was run or raised, or a press went back home. |
| `go agent` | An agent was brought to the front. |
| `agent new` | An agent tab was opened. |
| `shell new` | A shell tab was opened. |
| `action` | An action ran. |
| `agent session` | The surface saw an agent hold a conversation: first, then once on each later day it still holds it. |

Each line has `time`, `event` and `project`, and where they apply `target`,
`launched`, `action`, `agent`, `session` and `dir`. `revier events --help`
says what each field holds. A session restore writes no `go`.

`revier events` counts nothing. To find which projects you use most, count
the lines yourself:

```bash
revier events --days 7 | jq -r .project | sort | uniq -c | sort -rn
```

The events are kept in `~/.local/state/revier/events.jsonl`. revier never
prunes this file.

Without `--local`, `revier events` also asks the revier on every host a link
names. A line from a host carries that `host`, and an action on a link is
among them, since it ran there. Where a project here links to the project of
such a line, the line has it as `link`. A host that does not answer is named
on stderr, and the events of the others are printed.

## Quick reference

| Command | Does |
|---|---|
| `revier new [name]` | Write a project file for this directory. |
| `revier open <name>` | Open a project; an unknown name becomes a new project for this directory; a missing directory is cloned. |
| `revier run <action> [-p name]` | Run an action in the project. Exits with the action's status. |
| `revier each [--filter '<sh>'] [-n] -- <cmd>` | Run a command in every project's directory. Exits 5 when it failed in one. |
| `revier each log [<run> [<project>]]` | Past runs, one run, or one project's output. |
| `revier session save [--name label]` | Record the open projects. |
| `revier session restore [id\|name] [--dry-run]` | Open a saved session; the newest without an argument. |
| `revier session list [--json]` | The saved sessions, newest first. |
| `revier shutdown [project] [--agents \| --targets] [--force] [--no-session-save] [--dry-run]` | Save the session when it changed, then close what is open. |
| `revier events [--days n] [--local]` | What revier did, one JSON line per event. |

| Path | Holds |
|---|---|
| `~/.config/revier/projects/<name>.toml` | One project. |
| `~/.config/revier/config.toml` | Shared targets and actions. |
| `~/.local/state/revier/runs/` | The output of every `revier each`. |
| `~/.local/state/revier/sessions/` | The saved sessions. |
| `~/.local/state/revier/events.jsonl` | The events. |

The two roots follow `XDG_CONFIG_HOME` and `XDG_STATE_HOME` when they are set.
