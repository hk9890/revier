# Configuration

The reference for every file, table and field revier reads. For a
walk-through, read [projects.md](projects.md).

## Files

revier reads `config.toml` and `projects/*.toml` from one directory:

| Directory | When |
|---|---|
| `$REVIER_CONFIG_HOME` | the variable is set and not empty |
| `$XDG_CONFIG_HOME/revier` | the variable is set and not empty |
| `~/.config/revier` | otherwise |

- `config.toml` holds global settings and shared tables. It is optional.
- `projects/<name>.toml` is one project. The file name is the project's name.
  revier ignores subdirectories of `projects/`.
- A link file is a project file with a `[remote]` table: a project on another
  machine.

Which file can hold which table:

| Table or key | `config.toml` | project file | link file |
|---|---|---|---|
| `path`, `git_url`, `[vars]` | no | yes | yes |
| `[remote]` | no | no (it makes the file a link) | yes |
| `[[target]]` with `[target.window]`, `[target.runtime]` | yes, shared | yes | no |
| `[[target]]` with `[target.remote.window]`, `[target.remote.runtime]` | yes, shared | no | yes |
| `[[status]]` at `rule-left`, `rule-right` | yes | no | no |
| `[[status]]` at `project-line`, `project` | yes, shared with local projects only | yes | yes |
| `[ui]`, `[hosts]`, `[[action]]`, `[[probe]]`, `[[machine]]` | yes | no | no |

- revier ignores an unknown key, and a table in a file that does not take it.
  `revier doctor` reports only a `[[status]]` at a rule place and a target
  part in the wrong file. If a field has no effect, check its spelling.
- `revier new`, `revier link`, the project screen (alt+e), the config
  screen (alt+c) and the remote screen (alt+r) also write these files. They
  keep your comments.
- Every TUI reads the files again when it gets the focus; the popup, when
  you raise it. After you change `[ui]`, `[hosts]` or `[[probe]]` by hand,
  restart the TUI.

## Project file

```toml
# ~/.config/revier/projects/shop.toml
path    = "~/dev/shop"
git_url = "git@github.com:example/shop.git"

[vars]
board = "https://github.com/example/shop/projects/1"
```

| Key | Type | Default | Meaning |
|---|---|---|---|
| `path` | string | required | The project's directory. A leading `~` is your home directory. |
| `git_url` | string | none | The repository `path` is a clone of. When `path` is missing, opening the project clones it. No whitespace, no shell metacharacters, no user or token in an `http(s)` URL. |
| `[vars]` | table of strings | none | Your own values for [templates](#templates): `{{.Vars.board}}`. |
| `[[target]]` | array of tables | none | The project's [targets](#targets). |
| `[[status]]` | array of tables | none | Statuses in this project's pane. See [extending/status.md](extending/status.md). |

- Do not write `name =` at the top level. The file name is the project's name.
- A project name has no whitespace, no control character, no `/` and no `:`.
  `revier new` and a rename refuse such a name; a file you name yourself must
  follow the same rule.
- A project needs exactly one target with `home = true`. With a
  [shared](#shared-targets) home, `path` alone is a complete project. A home
  with an agent tab is in [Tabs](#tabs).

## Targets

A target is a named thing you reach with a key: the workspace, an editor, a
page.

| Key | Type | Default | Meaning |
|---|---|---|---|
| `name` | string | required | Letters, digits, `.`, `_`, `-`; starts with a letter or digit. Unique in the project. |
| `key` | string | none | The key that runs-or-raises the target. See [Keys](#keys). |
| `home` | bool | `false` | The workspace: what opens the project, and where toggle-back returns. |
| `prefer` | `"window"` or `"runtime"` | `"window"` | Which realization wins when the target has both and both hosts work. |
| `[target.window]` | table | none | How the window host (GNOME) opens and finds it. |
| `[target.runtime]` | table | none | How the runtime host opens and finds it. |
| `[target.remote.window]`, `[target.remote.runtime]` | tables | none | The same, for a link. Only in a link file and in `config.toml`. |

A target needs at least one realization. A realization with no host on this
machine reports that it is unavailable.

### Realization fields

`[target.window]` and `[target.runtime]` take these keys:

| Key | Type | Applies to | Meaning |
|---|---|---|---|
| `launch` | list of strings | both | The command revier runs when `match` finds nothing. An argv list, not a shell string. Required, except on a runtime part with `tabs`, and on a tab with `panels`. |
| `match` | inline table | both | How revier finds an open instance. Required, except on a tab. |
| `name` | string | runtime; required, except on a tab | The name of the instance revier opens: the kitty OS window title, the tmux session name, the herdr workspace label. `match` must find this name. |
| `dir` | string | both | The directory `launch` and the panels start in. Default: the project's `path`. |
| `place` | string | both | Where a new window goes: x, y, width, height as `wctl place` reads them, for example `"right top 75% 100%"`. Needs GNOME. Applies on launch only. A rule of the Window Control extension for the same window must name the same position, or the window jumps from the rule's position to this one. |
| `inside` | string | runtime | Makes the target a tab of the named target. See [Tabs](#tabs). |
| `tabs` | list of strings | runtime | The tabs a new instance opens with, in order. |
| `active` | string | runtime | The entry of `tabs` that has the focus in a new instance. Default: the first. |
| `[[...panels]]` | array of tables | runtime, tab only | The panes of the tab. See [Panels](#panels). |

`match` keys. Set at least one. Every key you set must match.

| Key | Meaning |
|---|---|
| `class` | Regular expression on the window class (`WM_CLASS`). |
| `title` | Regular expression on the window title, or on the runtime instance name. |
| `pid` | A process id. |

The patterns are not anchored: write `^` and `$` to match the whole name.

```toml
[[target]]
name = "pulls"
key  = "ctrl-g"
  [target.window]
  launch = ["google-chrome", "--app=https://github.com/example/shop/pulls",
            "--class=revier-{{.Name}}-pulls"]
  match  = { class = "^revier-{{.Name}}-pulls$" }
  place  = "right top 50% 100%"
```

A target can have both realizations. revier uses the window one when a window
host works, else the runtime one. `prefer = "runtime"` reverses this when both
work.

### Templates

revier fills in `{{...}}` before it runs a command. Templates work in
`launch`, `name`, `dir`, `place`, `match.class`, `match.title`, a panel's
`title` and `command`, and an action's `run`.

| Template | Value |
|---|---|
| `{{.Name}}` | The project's name. |
| `{{.Path}}` | The project's `path`, with `~` expanded. On a link, the path on the host. |
| `{{.GitURL}}` | The project's `git_url`. |
| `{{.Vars.<key>}}` | A value from `[vars]`. |
| `{{.Remote.Host}}` | A link's host. Only in `[target.remote.*]`. |
| `{{.Remote.Project}}` | The project's name on the host. Only in `[target.remote.*]`. |

A template that reads a missing key, or an argument that renders empty,
refuses the target. An argument you write as `""` stays empty.

### Keys

Write modifiers and a key joined by `-` or `+`: `ctrl-o`, `ctrl+shift+d`,
`alt-f5`. The GNOME form `<Control><Shift>d` also works.

| Part | Values |
|---|---|
| Modifiers | `ctrl` (`control`, `primary`), `alt` (`mod1`), `shift`, `super` (`mod4`, `meta`, `hyper`) |
| Keys | a letter, a digit, `f1`-`f24`, `enter`, `esc`, `tab`, `backspace`, `delete`, `insert`, `up`, `down`, `left`, `right`, `home`, `end`, `pgup`, `pgdown`, `menu`, `print`, a punctuation character, any GNOME key name |

`revier keys install` binds the key to the target's name in the project you
are in, so give a target the same key in every project. Two targets of a
project cannot share a key. A target named `popup` or `picker` cannot have a key.

### Tabs

`inside` opens the target as a tab of another target's instance: a kitty tab,
or a tmux window. `tabs` on the outer target opens a new instance with those
tabs.

```toml
[[target]]
name = "home"
home = true
  [target.runtime]
  name   = "session:{{.Name}}"
  match  = { title = "^session:{{.Name}}$" }
  tabs   = ["tickets", "agent"]
  active = "agent"

[[target]]
name = "tickets"
key  = "ctrl-shift-t"
  [target.runtime]
  inside = "home"
  launch = ["taskmgr-ui"]

[[target]]
name = "agent"
  [target.runtime]
  inside = "home"
    [[target.runtime.panels]]
    kind    = "agent"
    title   = "claude"
    command = ["claude"]
    [[target.runtime.panels]]
    kind  = "shell"
```

| Rule | Tab (has `inside`) | Target a tab is inside |
|---|---|---|
| Needs | `launch` or `panels`, not both | a runtime realization with `name` and `match`, in the same project |
| Cannot have | `tabs`, a window realization, `home = true` | a window realization, `inside`; with `tabs`, a `launch` |
| `tabs`, `active` | not allowed | each `tabs` entry is a tab inside this target; `active` is one of them |

- A tab ignores `match` and `name`: revier finds it by the target's name.
- `tabs` and `active` apply only when revier creates the instance.
- When a tab of `tabs` fails to open, the tabs after it do not open. The
  instance stays open with the tabs it has.
- A tab that is not in `tabs` opens when you press its key.
- Tabs need a runtime host.

### Panels

Panels are the panes of one tab. Write them only on a target with `inside`,
as `[[target.runtime.panels]]` (`[[target.remote.runtime.panels]]` in a link
file).

| Key | Type | Meaning |
|---|---|---|
| `kind` | `"agent"`, `"shell"` or `"tool"` | A coding agent, a shell, or another program. Default: `"tool"`. revier refuses the target for another value. |
| `title` | string | The panel's title. |
| `command` | list of strings | What the panel runs. Default: your shell. |
| `agent` | string | A [named agent](#named-agents): the panel runs its `command`, and its name is the default `title`. Only with `kind = "agent"` and no `command`. revier refuses the target for a name the project has no agent of. |

The first agent and shell panels in the home's `tabs` are the workspace's
agent and shell: `revier agent new` and `revier shell new` copy them. A
restore adds the resume flag to the agent's `command`, so a wrapper must pass
its arguments on: `["sh", "-lc", "exec my-agent \"$@\"", "sh"]`. A restore
resumes the agent panels of a tab in `tabs`. A tab that runs a `launch` comes
back, but without a resume.

## Shared targets

A `[[target]]` in `config.toml` goes to every project. A project target with
the same name merges into it:

```toml
# config.toml
[[target]]
name = "editor"
key  = "ctrl-shift-o"
  [target.window]
  launch = ["idea", "{{.Path}}"]
  match  = { class = "^jetbrains-idea", title = "^{{.Name}}( |$)" }
  [target.remote.window]
  launch = ["code", "--remote", "ssh-remote+{{.Remote.Host}}", "{{.Path}}"]
  match  = { class = "^Code$", title = "\\[SSH: {{.Remote.Host}}\\]" }
```

```toml
# projects/platform.toml: only the title changes.
# It still needs a home target, its own or a shared one.
path = "~/dev/platform"

[[target]]
name = "editor"
  [target.window]
  match = { title = "^platform-service( |$)" }
```

| Merge rule | Effect |
|---|---|
| Table | Merges key by key, at any depth. The example keeps the shared `class`. |
| Any other value (string, bool, list such as `launch`, `tabs`, `panels`) | Replaces the shared value. |
| Order | Shared targets first, in `config.toml` order, then the project's new targets. |
| Removal | Not possible: every project gets every shared target that has a part for its kind. A project can change every field, its `key` included. |

- `[target.window]` and `[target.runtime]` reach local projects;
  `[target.remote.window]` and `[target.remote.runtime]` reach links. A shared
  target with no part for a kind of project does not reach it.
- A shared `home` gives every project its home. Override it by name; a second
  target with `home = true` is refused.

## Named agents

An `[[agent]]` names a program an agent panel runs. Declare it in
`config.toml`, and every local project has it:

```toml
# config.toml
[[agent]]
name    = "claude"
command = ["claude"]

[[agent]]
name    = "dtop"
command = ["dtop", "{{.Path}}"]
key     = "ctrl+shift+d"
```

`revier agent new dtop` opens a tab that runs it
([Agents](agents.md#open-more-agent-and-shell-tabs)). A panel names one with
`agent` in place of `command`:

```toml
    [[target.runtime.panels]]
    kind  = "agent"
    agent = "claude"
```

| Key | Type | Meaning |
|---|---|---|
| `name` | string | The agent's name. Required, and unique. Use letters, digits, `-`, `_` and `.`, and do not start it with `-`: the name is one word of the `revier agent new <name>` command that its terminal key runs. revier refuses the agent for another name. |
| `command` | list of strings | What the agent runs. Required. [Templates](#templates) work as in a panel's `command`. |
| `key` | string | The terminal key that opens this agent, in the [key](#keys) form. Only in `config.toml`. See [`[terminal]`](#terminal). |

A project file can declare `[[agent]]` too:

- The same name as one in `config.toml` changes that agent key by key: write
  only the `command` to give this project another one.
- A new name is an agent of this project alone.
- A project cannot remove an agent `config.toml` declares.
- A `key` in a project file is refused: the terminal's keys are one file per
  machine. The agent keeps its command.

A link has no named agents of its own: its host runs them, and the host's
configuration names them.

## Actions

An action is a command with nothing to raise: a script, a sync, a clipboard
copy. Only in `config.toml`.

```toml
[[action]]
key  = "ctrl-y"
name = "copy path"
run  = ["wl-copy", "{{.Path}}"]
```

| Key | Type | Default | Meaning |
|---|---|---|---|
| `name` | string | none | The name `revier run <name>` and the help screen use. |
| `key` | string | required | The TUI key that runs it on the selected project. |
| `run` | list of strings | required | The command, an argv list with [templates](#templates). Runs in the project's directory. |

The key works only in the TUI. Use ctrl or alt: a plain character filters the
list. A key the TUI uses itself (alt+h lists them) is refused.

`revier run <name> -p <project>` runs an action from a shell. For a link, it
runs the action of that name from the host's `config.toml`.

## Link file

A link is a project on another machine. `revier link` writes the file; see
[links.md](links.md).

```toml
# ~/.config/revier/projects/far.toml
path    = "/home/user/dev/far"
git_url = "git@github.com:example/far.git"

[remote]
host    = "buildbox"
project = "far"
```

| Key | Default | Meaning |
|---|---|---|
| `path` | none | The project's path on the host. Write it in full: `~` is not expanded. |
| `git_url` | none | The host's repository. |
| `[remote] host` | required | The ssh destination. No leading `-`, no whitespace. |
| `[remote] project` | the file's name | The project's name on the host. |

revier adds what the file leaves out:

| Derived | What it is |
|---|---|
| Target `home` | A runtime instance `session:<name>` with one tab, `agent`. A home you write keeps its fields; revier fills `name` and `match`. A home with a `launch` gets no tab. |
| Tab `agent` | Inside the home: an agent panel and a shell panel, each an ssh to the host that runs `revier agent exec` or `revier shell exec` there. |

- revier drops `tabs` and `active` on a link's targets. The home cannot be
  named `agent`, and cannot have panels.
- revier skips a link whose host is this machine (by name, or by `HostName`
  in `~/.ssh/config`), so both machines can share the project files.
- A link works also when its host is not one of your
  [machines](#machines). `revier doctor` names such a link.

## Machines

The machines the remote screen (alt+r) and `revier link` list. Only in
`config.toml`. The remote screen's `new` (alt+n) writes a table, and its
`remove` (alt+d) deletes one.

```toml
[[machine]]
host = "buildbox"
```

| Key | Default | Meaning |
|---|---|---|
| `host` | required | The ssh destination, as for `[remote] host`: a `Host` of `~/.ssh/config`, or `user@machine`. No leading `-`, no whitespace. |

- revier does not list a machine that is this machine (by name, or by
  `HostName` in `~/.ssh/config`), so both machines can share `config.toml`.

## `[ui]`

Only in `config.toml`.

| Key | Default | Values |
|---|---|---|
| `theme` | `catppuccin-mocha` | `catppuccin-mocha`, `catppuccin-macchiato`, `catppuccin-frappe`, `catppuccin-latte` |
| `glyphs` | `nerd` | `nerd` (needs a Nerd Font), `unicode`, `ascii` |
| `trigger_key` | `alt-space` | The desktop key that opens the popup, in the [key](#keys) form. |

You cannot set a single colour. To turn colour off, set `NO_COLOR`
([Environment variables](cli.md#environment-variables)).

## `[terminal]`

The keys your terminal holds for revier, and what they do in a terminal window
that is no project's workspace. Only in `config.toml`. `revier term install`
writes the keys into the terminal's configuration; run it again after you
change one.

```toml
[terminal]
shell_key = "ctrl+shift+t"
agent_key = "ctrl+shift+z"
outside   = "tab"
```

| Key | Default | Meaning |
|---|---|---|
| `shell_key` | `ctrl+shift+t` | Opens a shell tab in the workspace of the focused window, as `revier shell new` does. |
| `agent_key` | `ctrl+shift+z` | Opens a tab with the workspace's agent, as `revier agent new` does. |
| `outside` | `tab` | What a key does in a window of no workspace: `tab` opens a plain tab beside it, `none` does nothing, `agent` runs the agent the key opens. |

- Write a key in the [key](#keys) form. An empty string turns that key off.
- A named agent's key is its `key` in `[[agent]]`.
- With `outside = "agent"`, in a window of no workspace, `agent_key` opens a
  tab that runs the first `[[agent]]` of `config.toml`, and a named agent's
  key that agent, each in the window's directory. The shell key still opens
  a plain tab. With no `[[agent]]` in `config.toml`, `agent_key` opens a
  plain tab. An agent whose `command` uses a [template](#templates) opens no
  tab there, because no project fills it in; the error names the agent.
- revier refuses a key that another terminal key already holds, or that it
  cannot read. The other keys still work.
- `revier term install` leaves out a key on the same chord as `trigger_key`
  or a target's `key`: the desktop takes that press first.

## `[hosts]`

Only in `config.toml`. Leave it out: revier then uses the first host that
works.

```toml
[hosts]
runtime = ["tmux"]
window  = ["none"]
```

| Key | Values | Default |
|---|---|---|
| `runtime` | `kitty`, `tmux`, `herdr`, `none` | `["kitty", "tmux", "herdr"]` |
| `window` | `gnome`, `none` | `["gnome"]` |

A key you leave out uses its default, and revier skips a default host that
does not work, without a message. A list you write is strict: revier tries the
names in order and stops at the first that works, or at `none`, which turns
that kind off. When it reaches an unknown name first, or no name works, every
command that opens, raises or lists stops with an error. One exception:
`revier agent new --panel` and `revier shell new --panel` with a tmux pane id
use tmux wherever `runtime` names it before `none`, so a tmux key works where
kitty is the runtime of everything else. `revier doctor`
reports every unknown name, wherever it is in the list; it does not check
whether a host works.

## Statuses and probes

`[[status]]` shows the output of your command on the TUI: see
[extending/status.md](extending/status.md). `[[probe]]` reads the state of an
agent revier does not know: see [extending/probes.md](extending/probes.md).

## Mistakes

A mistake stops only the table, target or project it is in. An unknown
`theme` or `glyphs` uses its default, a `trigger_key` revier cannot read
stops only `revier keys`, and a `[[probe]]` without `name` or `exec` is not
run. One exception: when `config.toml` is not valid TOML, or a value in
it has the wrong type, such as `theme = 3`, no command that reads the
projects runs. `revier doctor` prints the error, and `revier assist` starts
to fix it.

Short of that exception, every command runs, and warns on stderr that some
files have problems, naming `revier doctor`; `revier list` marks the projects
instead. The config screen (alt+c) lists each problem of `config.toml` over its
rows. An edit revier makes, such as on the project screen, the
config screen or a rename, is refused only for a problem it adds. A file that
was broken before can be repaired one target at a time.

Run `revier doctor` to see every problem, with the fix where it knows one. See
[troubleshooting.md](troubleshooting.md).
