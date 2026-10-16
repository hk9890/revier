# Configuration

This page is the reference for every file, table and field revier reads. Each
section says which file may hold the table, what each field does, its default,
and what revier refuses. For a walk-through, read
[projects.md](projects.md) first; come back here for the exact rules.

## The files and where they live

revier reads plain TOML files from one directory:

```
~/.config/revier/
  config.toml            global settings, shared targets, actions, statuses, probes
  projects/
    shop.toml            a project on this machine
    far.toml             a link: a project on another machine
```

The directory is the first of these that is set:

| Location | When |
|---|---|
| `$REVIER_CONFIG_HOME` | the variable is set and not empty |
| `$XDG_CONFIG_HOME/revier` | the variable is set and not empty |
| `~/.config/revier` | otherwise |

`REVIER_CONFIG_HOME` points revier at a scratch configuration, for example to
try a change without touching your own.

- **`config.toml`** holds what is not about one project: `[ui]`, `[hosts]`,
  `[[action]]`, `[[probe]]`, the shared `[[target]]` tables and the
  `[[status]]` tables. A missing `config.toml` is not an error: revier starts
  with the defaults.
- **`projects/<name>.toml`** is one project. The file name, without `.toml`,
  is the project's name. revier reads every `*.toml` file directly in
  `projects/`, in name order, and ignores subdirectories.
- **A link file** is a project file with a `[remote]` table. It lives in the
  same `projects/` directory.

Which table each file may hold:

| Table or key | `config.toml` | project file | link file |
|---|---|---|---|
| `path`, `git_url`, `[vars]` | not read | yes | yes |
| `[remote]` | not read | makes the file a link | yes |
| `[[target]]` with `[target.window]`, `[target.runtime]` | yes, shared | yes | refused |
| `[[target]]` with `[target.remote.window]`, `[target.remote.runtime]` | yes, shared | refused | yes |
| `[[status]]` at `rule-left`, `rule-right` | yes | refused | refused |
| `[[status]]` at `project-line`, `project` | yes, shared | yes | yes |
| `[ui]`, `[hosts]`, `[[action]]`, `[[probe]]` | yes | not read | not read |

revier writes these files itself in a few places: `revier new`, `revier open`
for a name it does not know, `revier link`, the project screen (alt+e) and
the config screen (alt+c). Each changes only the lines it must, keeps your
comments, and refuses a change that would not load. Otherwise, edit the files
by hand.

revier does not report a key it does not know. A misspelled key is ignored, so
when a field seems to have no effect, check its spelling against this page.

### When a change takes effect

Every command reads the files when it starts. The TUI reads the project
files, the shared targets, the actions and the statuses again each time the
popup is raised, and after `revier assist` returns. `[ui]`, `[hosts]` and
`[[probe]]` are read when revier starts: after you change them by hand, quit
the TUI and start it again. A change made on the config screen applies at
once.

## A project file

A project file says where the project lives and which targets it has:

```toml
# ~/.config/revier/projects/shop.toml
path = "~/dev/shop"

# Optional. Opening the project clones this into path when path is missing.
git_url = "git@github.com:example/shop.git"

# Optional. Values the templates of this project's targets read as {{.Vars.<key>}}.
[vars]
board = "https://github.com/example/shop/projects/1"

[[target]]
name = "home"
home = true
  [target.runtime]
  name  = "session:{{.Name}}"
  match = { title = "^session:{{.Name}}$" }
  tabs  = ["agent"]

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
    title = "shell"
```

| Key | Type | Meaning |
|---|---|---|
| `path` | string | The project's directory. Required for a project on this machine. A leading `~` is your home directory. |
| `git_url` | string | The repository `path` is a clone of. When `path` is missing, opening the project clones it. Refused: whitespace, control characters, shell metacharacters (`` ` " ' \ $ ; \| & < > ( ) { } ``), and an `http` or `https` URL with a user or token in it. |
| `[vars]` | table of strings | Your own values, for templates: `{{.Vars.board}}`. |
| `[[target]]` | array of tables | The project's targets. See [Targets](#targets). |
| `[[status]]` | array of tables | Statuses for this project's pane. See [`[[status]]`](#status). |

Do not write `name =` at the top of a project file. The file name is the
project's name, and a `name` key refuses the whole file.

A project name may not contain whitespace, a control character, `/`, or
`:`. The colon separates a project from a target in an agent address such as
`shop:agent`.

A project file needs no target of its own when `config.toml` gives it a home
target: then `path` alone is a whole project.

## Targets

A target is a named thing you reach with a key: the workspace, an editor, a
page. Each `[[target]]` table has these keys:

| Key | Type | Default | Meaning |
|---|---|---|---|
| `name` | string | required | Letters, digits, `.`, `_` and `-`, starting with a letter or a digit. Unique in the project. |
| `key` | string | none | The key that runs-or-raises the target. See [Keys](#keys). |
| `home` | bool | `false` | This target is the workspace: what opens the project and where toggle-back returns. A project needs exactly one. |
| `prefer` | `"window"` or `"runtime"` | window | Which realization wins when the target has both and both hosts are available. |
| `[target.window]` | table | none | How a window host (GNOME) realizes the target. |
| `[target.runtime]` | table | none | How a runtime host (kitty or tmux) realizes the target. |
| `[target.remote.window]`, `[target.remote.runtime]` | tables | none | The same, for a link. Only in a link file and in `config.toml`. |

A target needs at least one realization. With no `prefer`, the window
realization wins whenever a window host is available; otherwise the runtime
one is used. A target whose realization has no host on this machine reports
that it is unavailable.

### Realization fields

`[target.window]` and `[target.runtime]` take the same keys. Some apply to one
kind only:

| Key | Type | Applies to | Meaning |
|---|---|---|---|
| `launch` | list of strings | both | The command revier runs when `match` finds nothing. An argv list, not a shell string. |
| `match` | inline table | both | How revier finds an open instance. See below. Required, except on a tab. |
| `name` | string | runtime: required | The name revier gives the instance it opens: the kitty OS window's title, the tmux session's name. `match` must find it by that name. A window realization does not need it. |
| `dir` | string | both | The directory `launch` and every panel start in. Default: the project's `path`; on a link, the directory revier runs in. A leading `~` is your home directory. |
| `place` | string | both | Where a newly launched window goes: four tokens, x, y, width and height, as `wctl place` reads them, for example `"right top 75% 100%"`. Needs the GNOME window host. It applies only to a launch, never to a raise. |
| `inside` | string | runtime | Makes this target a tab of the named target's instance. See [Tabs](#tabs). |
| `tabs` | list of strings | runtime | The tab targets a new instance opens with, in order. See [Tabs](#tabs). |
| `active` | string | runtime | The entry of `tabs` that has the focus in a new instance. Default: the first. |
| `[[...panels]]` | array of tables | runtime, on a tab only | The panes of the tab. See [Panels](#panels). |

`launch` runs only when `match` finds nothing. Every target is therefore a
run-or-raise binding, not a launcher: a second press raises what the first one
opened.

`match` takes these keys. Every key you set must match; a key you leave out
does not constrain. A match with no key is refused, because it would raise
whichever instance happens to be listed first.

| Key | Meaning |
|---|---|
| `class` | A regular expression on the window's class (`WM_CLASS`). |
| `title` | A regular expression on the window's title, or on the runtime instance's name. |
| `pid` | A process id. It changes on each launch, so a project file seldom uses it. |

The patterns are not anchored: write `^` and `$` where you mean the whole
name.

```toml
# An editor: a window on a desktop.
[[target]]
name = "editor"
key  = "ctrl-o"
  [target.window]
  launch = ["code", "{{.Path}}"]
  match  = { class = "^code$", title = "{{.Name}}" }

# A page. --class gives the window something to match on; a normal browser
# window has no class of its own.
[[target]]
name = "pulls"
key  = "ctrl-g"
  [target.window]
  launch = ["google-chrome", "--app=https://github.com/example/shop/pulls",
            "--class=revier-{{.Name}}-pulls"]
  match  = { class = "^revier-{{.Name}}-pulls$" }
  place  = "right top 50% 100%"
```

### A target reached two ways

A target can carry both realizations. On a desktop the window wins; over SSH,
or with `prefer = "runtime"`, the runtime one is used:

```toml
[[target]]
name = "diff"
key  = "ctrl-shift-d"

  [target.window]
  launch = ["meld", "{{.Path}}"]
  match  = { class = "^meld$" }

  [target.runtime]
  name   = "diff:{{.Name}}"
  launch = ["nvim", "-d"]
  match  = { title = "^diff:{{.Name}}$" }
```

### Templates

A template is `{{...}}` in a string. revier fills it in from the project
before it runs anything, so a program never sees the braces. These fields can
hold templates: `launch`, `name`, `dir`, `place`, `match.class`,
`match.title`, a panel's `title` and `command`, and an action's `run`.

| Template | Value |
|---|---|
| `{{.Name}}` | The project's name: its file name without `.toml`. |
| `{{.Path}}` | The project's `path`, with `~` expanded on this machine. On a link it is the path on the host, as written. |
| `{{.GitURL}}` | The project's `git_url`. |
| `{{.Vars.<key>}}` | A value from the project's `[vars]` table. |
| `{{.Remote.Host}}` | A link's host. Only in `[target.remote.*]`. |
| `{{.Remote.Project}}` | The project's name on the link's host. Only in `[target.remote.*]`. |

The templates are Go `text/template`. A template that reads a key the project
does not have refuses the target. So does an argument that renders to an empty
string, such as `{{.GitURL}}` in a project with no `git_url`: an empty argument
would reach the program as the current directory, an empty pattern or a
missing operand. An argument written empty, `""`, stays as it is.

A shared target can read `{{.Vars.<key>}}`: each project then gives that key
its own value.

### Keys

A key is written as modifiers and a key, joined by `-` or `+`: `ctrl-o`,
`ctrl+shift+d`, `alt-f5`. The GNOME form `<Control><Shift>d` is read too.

- Modifiers: `ctrl` (also `control`, `primary`), `alt` (also `mod1`),
  `shift`, `super` (also `mod4`, `meta`, `hyper`).
- Keys: a letter, a digit, `f1` to `f24`, `enter`, `esc`, `tab`,
  `backspace`, `delete`, `insert`, `up`, `down`, `left`, `right`, `home`,
  `end`, `pgup`, `pgdown`, `menu`, `print`, a punctuation character, or any
  key name GNOME knows.

A key belongs to the target's name, not to one project:
`revier keys install` binds the key on the desktop to
`revier go <target> --picker`, which runs that target in the project you are
in. Give a target the same key in every project that has it.
`revier keys status` marks a target with two keys, or a key with two targets,
as a conflict.

A target named `picker` cannot have a key: that name belongs to the key that
opens revier, `[ui] trigger_key`.

### Tabs

Set `inside` on a runtime realization to open the target as a tab of another
target's instance, not as an instance of its own:

```toml
[[target]]
name = "tickets"
key  = "ctrl-shift-t"
  [target.runtime]
  inside = "home"
  launch = ["taskmgr-ui"]
```

A tab:

- takes `launch` or `panels`, one of the two;
- takes no `match` and no `name`: revier marks the tab it opens and finds it
  again by that mark;
- has no window realization, is not `home`, and holds no `tabs`;
- is inside a target of the same project that has a runtime realization, no
  window realization, and is not itself a tab.

A tab needs a runtime with tabs: kitty, where it is a tab of the OS window,
or tmux, where it is a window of the session. On any other runtime the key is
refused with the reason.

`tabs` on the target the tabs are inside opens a new instance with them. It
lists tab targets in the order they open; each entry must be a target whose
`inside` names this one. The target is then only the window: it has no
`launch` of its own. To open the home with the ticket viewer first and the
agent in focus:

```toml
[[target]]
name = "home"
home = true
  [target.runtime]
  name   = "session:{{.Name}}"
  match  = { title = "^session:{{.Name}}$" }
  tabs   = ["tickets", "agent"]
  active = "agent"
```

- `tabs` and `active` apply only when revier creates the instance. An
  instance that is open keeps its tabs and its focus.
- A key press on a listed tab that creates the instance leaves the focus on
  that tab.
- A tab that is not listed opens when you press its key.
- A target with no `tabs` runs its `launch`.
- A listed tab that fails to open stops the ones after it. The instance stays
  open with the tabs it has, and the press reports the error.
- On a runtime with no tabs, the key of a target with `tabs` is refused with
  the reason. There, use a `launch` alone.
- A saved session records no step for a listed tab: it comes back with its
  instance.

### Panels

`panels` are the panes of one tab. Write them on a target with `inside`, as
`[[target.runtime.panels]]` (or `[[target.remote.runtime.panels]]` in a link
file). Panels on any other realization are refused, with the move that fixes
it.

| Key | Type | Meaning |
|---|---|---|
| `kind` | `"agent"`, `"shell"` or `"tool"` | What the panel is: a coding agent, a shell, or another program. |
| `title` | string | The panel's title. |
| `command` | list of strings | What the panel runs. A panel with no `command` starts your shell. |

The workspace's agent panel and shell panel are the first panel of each kind
in the home's listed tabs, in order. `revier agent new` and
`revier shell new` copy them, and `-p <project>:agent` names the workspace as
`-p <project>:home` does. A saved session records the agents in the panels of
a listed tab with their conversations, and a restore resumes them in those
panels. A tab that runs a `launch` comes back without a resume. See
[agents.md](agents.md).

The resume flag is added to the agent panel's `command`. A `command` that
wraps the agent must pass its arguments on:
`["sh", "-lc", "exec my-agent \"$@\"", "sh"]`, not
`["sh", "-lc", "my-agent"]`.

## Shared targets

Targets most projects have in common are declared once, in `config.toml`, in
the same form. Every project gets them, and its file then holds only what is
its own:

```toml
# config.toml
[[target]]
name = "editor"
key  = "ctrl-shift-o"
  [target.window]
  launch = ["idea", "{{.Path}}"]
  match  = { class = "^jetbrains-idea", title = "^{{.Name}}( |$)" }
```

```toml
# projects/platform.toml
path = "~/dev/platform"

# Same name: merged into the shared editor. Only the title changes; the key,
# the launch and the class stay the shared ones.
[[target]]
name = "editor"
  [target.window]
  match = { title = "^platform-service( |$)" }

# A new name: a target of this project alone.
[[target]]
name = "pulls"
  [target.window]
  launch = ["google-chrome", "--app=https://github.com/example/platform/pulls"]
  match  = { class = "^chrome-github" }
```

How a project's target merges with the shared one of the same name:

- A table merges key by key, at any depth: `match = { title = "..." }` keeps
  the shared `class`.
- Any other value replaces the shared one whole: a string, a bool, and a list
  such as `launch`, `tabs` or `panels`.
- The shared targets come first, in `config.toml` order, then the project's
  own new targets, in file order.
- A project cannot remove a shared target. It can change any field of it.

A shared target carries a part for each kind of project.
`[target.window]` and `[target.runtime]` reach the projects on this machine;
`[target.remote.window]` and `[target.remote.runtime]` reach the links. A
shared target with no part of a kind is not a target of that kind of project:
one with no remote part is for local projects alone.

```toml
# config.toml: one editor for both kinds of project.
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

`[target.remote]` holds only `window` and `runtime`. Put `name`, `key`,
`home` and `prefer` on the target itself.

A shared target with no name, a name given twice, or a value of the wrong type
reaches no project. Whether a shared target is complete, with a realization
and a match, is checked in each project after the merge, because a project may
add what the shared one leaves out.

When a shared target is `home`, a new project file needs no target of its
own. A project that wants a different home overrides the shared home by its
name; a second target marked `home` is refused.

## Actions

An action is a command that produces nothing to return to: a script, a sync,
a clipboard copy. Actions live in `config.toml` only:

```toml
[[action]]
key  = "ctrl-y"
name = "copy path"
run  = ["wl-copy", "{{.Path}}"]
```

| Key | Type | Meaning |
|---|---|---|
| `name` | string | What `revier run <name>` and the help screen call it. |
| `key` | string | Required. The key that runs it in the TUI, on the selected project. |
| `run` | list of strings | The command. An argv list with the same [templates](#templates) as `launch`. It runs in the project's directory. |

An action's key works in the TUI only; no desktop binding carries it. These
keys are refused, and the action stays listed but runs nothing:

- a single character, alone or with shift, because typing filters the list:
  give it ctrl or alt;
- a key that never reaches a terminal;
- a key that reaches a terminal as another key, for example `ctrl-i` as
  `tab`: the message names the key to bind instead;
- one of revier's own keys: `up`, `down`, `ctrl+p`, `ctrl+n`, `home`,
  `end`, `pgup`, `pgdown`, `enter`, `tab`, `shift+tab`, `esc`, `ctrl+c`,
  `delete`, `alt+space`, `alt+t`, `alt+e`, `alt+delete`, `alt+d`, `alt+n`,
  `alt+r`, `alt+s`, `alt+q`, `alt+c`, `alt+a`, `alt+h`.

An action with no `key` or an empty `run` is refused too. A refused action
does not run from `revier run` either: it prints the reason.

`revier run <name> -p <project>` runs an action from a shell. On a link, the
action runs on the host, so it is one the host's `config.toml` defines.

## A link file

A link is a project on another machine. `revier link` writes the file; see
[links.md](links.md) for the steps. The file has a `[remote]` table and no
directory on this machine:

```toml
# ~/.config/revier/projects/far.toml
# far: written by `revier link`.

# The path and the repository are the host's, as `revier link` recorded them.
# A target here renders them; neither names anything on this machine.
path = "/home/user/dev/far"
git_url = "git@github.com:example/far.git"

[remote]
host = "buildbox"   # the ssh destination
project = "far"     # its name on the host; defaults to this file's name

# Optional: a target of the link's own, a window here that reaches the
# project there.
[[target]]
name = "editor"
key  = "ctrl-shift-o"
  [target.remote.window]
  launch = ["code", "--remote", "ssh-remote+buildbox", "{{.Path}}"]
  match  = { title = "far \\[SSH: buildbox\\]" }
```

| Key | Meaning |
|---|---|
| `path` | The project's path on the host. Optional. Write it in full: `~` is not expanded in a link, so `~/dev/far` reaches a launch as a tilde. |
| `git_url` | The host's repository. Optional. |
| `[remote] host` | The ssh destination, as `ssh` would take it. It may not start with `-` or contain whitespace. |
| `[remote] project` | The project's name on the host. Default: the file's name. |

A link's targets use `[target.remote.window]` and `[target.remote.runtime]`.
`[target.window]` or `[target.runtime]` in a link file refuses that target,
and `[target.remote]` in a local project file does the same.

What the link file leaves out is derived:

- The home target. With no `home` in the file, the link gets a target `home`:
  a runtime instance named `session:<name>` that opens with one tab, `agent`.
- The tab `agent`, inside the home, with an agent panel and a shell panel.
  Each is an ssh to the host that runs `revier agent exec -p <project>` or
  `revier shell exec -p <project>` there, so the host's project file says
  which agent runs and where.

A home the file or a shared target gives keeps every field it sets, and the
derived name and match fill the rest. A placement alone is enough:

```toml
[[target]]
name = "home"
home = true
  [target.remote.runtime]
  place = "left top 50% 100%"
```

- A `tabs` or `active` on a link's target is dropped: only the derived home
  has tabs.
- A home with a `launch` runs it, and gets no tab.
- A home with a window realization alone is left as it is.
- A home cannot have the name `agent`, which is the tab's.
- A target named `agent` inside the home keeps its `launch` or panels, and
  gets the two panels when it has neither.
- Panels on the home itself are refused.

A link to the machine revier runs on is left out, by the host's name or the
`HostName` your `~/.ssh/config` gives it. The project files can then be shared
between both machines. `revier doctor` names the files it skipped.

## `[ui]`

How the TUI looks, and the key that opens it. Only in `config.toml`:

```toml
[ui]
theme       = "catppuccin-mocha"
glyphs      = "nerd"
trigger_key = "alt-space"
```

| Key | Default | Values |
|---|---|---|
| `theme` | `catppuccin-mocha` | `catppuccin-mocha`, `catppuccin-macchiato`, `catppuccin-frappe`, `catppuccin-latte` |
| `glyphs` | `nerd` | `nerd` (icons; needs a Nerd Font), `unicode` (characters any font has), `ascii` |
| `trigger_key` | `alt-space` | The desktop key that opens the popup, in the [key](#keys) form. |

An unknown theme, an unknown glyph set, or a `trigger_key` that does not parse
stops revier: no command but `revier assist` runs until you fix it.

The colours are the theme's: you cannot set one colour alone. Set `NO_COLOR`
to any value that is not empty to turn colour off in the TUI; bold and reverse
video stay. The config screen (alt+c) sets all three keys.

## `[hosts]`

Which host revier uses for each kind. Only in `config.toml`. Leave it out to
use the first host that works on this machine, which is the normal case:

```toml
[hosts]
runtime = ["tmux"]
window  = ["none"]
```

| Key | Hosts | Default order |
|---|---|---|
| `runtime` | `kitty`, `tmux`, `none` | `kitty`, then `tmux` |
| `window` | `gnome`, `none` | `gnome` |

A list is an order of preference: revier takes the first host on it that
works here. `none` turns that kind off, for example to keep revier away from
your windows.

With no list, finding no host is not an error: the targets of the other kind
still work. With a list, an unknown name, or no listed host that works, stops
every command that opens or reads the desktop, with an error that names each
host and why. `revier doctor` does not check `[hosts]`.

## `[[status]]`

A status is a command whose output revier draws at one place on the TUI: the
two ends of the rule under the top line, the right end of the pane's first
line, or a section of the pane. Use it for an account's usage limits, a CI
result, a ticket count.

`config.toml` declares all four places. A project file declares the two places
in the pane, and changes a shared table at the same place. A link file declares
the two places in the pane too, but takes no table from `config.toml`: each of
its tables gives its own `run`.

The fields, the output format, the timing and a worked example are in
[extending/status.md](extending/status.md).

## `[[probe]]`

A probe tells revier the state of a coding agent it does not know: a program
that reads one panel as JSON and answers with one state. Only in
`config.toml`:

```toml
[[probe]]
name = "aider"
exec = "~/bin/aider-probe"
```

Both keys are required. A `[[probe]]` without `name` or `exec` stops revier.
The contract, with an example probe, is in
[extending/probes.md](extending/probes.md).

## What is refused, and how you find out

A mistake costs only what is wrong. revier keeps everything else working, so
you can still reach the file you have to fix.

| Mistake | What stops |
|---|---|
| `config.toml` is not valid TOML; an unknown `theme` or `glyphs`; a `trigger_key` that does not parse; a `[[probe]]` without `name` or `exec` | Every command but `revier assist`. The error names the file. |
| An `[[action]]` that breaks a rule | That action. It stays listed and says why when you run it. |
| A shared `[[target]]` with no name, a duplicate name, a wrong type, or a key under `[target.remote]` other than `window` and `runtime` | That shared target. It reaches no project. |
| A `[[status]]` table that breaks a rule | That table. |
| A project file that is not valid TOML, or that breaks a rule of the whole project | That project. It is still listed, with the reason, and nothing in it opens. |
| A target that breaks a rule | That target. It shows as invalid, and its key says why when you press it. |

A project is refused as a whole when:

- it has no `path` and no `[remote]`;
- the file sets `name`;
- its name contains `:`;
- `git_url` or `[remote] host` breaks its rule;
- no target is marked `home`.

A target is refused when:

- it has no name, a name with other characters than letters, digits, `.`,
  `_` and `-`, or the name of another target of the project;
- it is the second target marked `home`;
- its `key` does not parse, another target of the project has the same key,
  or it is named `picker` and has a key;
- it has no realization, or a `prefer` other than `window` or `runtime`;
- a realization has an empty `match` (a tab excepted), or no `launch` and no
  `tabs`;
- a runtime realization has no `name` (a tab excepted);
- `place` is not four tokens;
- a window realization has `inside`, `tabs` or `panels`;
- panels are on a realization that is not a tab;
- a tab breaks a rule of [Tabs](#tabs);
- `tabs` names a target twice, a target the project does not have, or a
  target that is not inside this one; `active` is not an entry of `tabs`, or
  is set with no `tabs`; `tabs` is beside a `launch`;
- a template does not parse, reads a key the project does not have, or
  renders an argument to nothing;
- a `match` pattern is not a valid regular expression;
- it writes the realization of the other kind of project: `[target.window]`
  in a link, `[target.remote.*]` in a local project.

A `[[status]]` table is refused when it has no `at` or an unknown one, stands
at a place of the whole screen in a project file, has a `title` at a place
other than `project`, runs nothing, has an `every` that is not a duration or is
under 5 seconds, or stands at a place an earlier table of the same file
already holds.

### `revier doctor`

`revier doctor` reads every file and prints each problem under its file, with
the edit that fixes it where it knows one:

```
$ revier doctor

/home/me/.config/revier/projects/shop.toml
  target "editor"  window realization has an empty match
                   fix: give the realization a `match` that names the instance it finds

12 projects, 1 file with problems
```

It exits 1 when a file has a problem, and 0 when all load whole. It also lists
the links it skipped because their host is this machine.

Every other command that loads a file with a problem, `revier list` excepted,
prints one warning line and writes each problem to the log:

```
revier: warning: 1 configuration file with problems; run `revier doctor`
```

The TUI lists a refused project with its reason. See
[troubleshooting.md](troubleshooting.md).

## Quick reference

| Table | File | Keys |
|---|---|---|
| top level | project, link | `path`, `git_url` |
| `[vars]` | project, link | any key, string values |
| `[remote]` | link | `host`, `project` |
| `[[target]]` | all | `name`, `key`, `home`, `prefer` |
| `[target.window]`, `[target.runtime]` | `config.toml`, project | `launch`, `match`, `name`, `dir`, `place`; runtime also `inside`, `tabs`, `active` |
| `[target.remote.window]`, `[target.remote.runtime]` | `config.toml`, link | as above |
| `match` | in a realization | `class`, `title`, `pid` |
| `[[target.runtime.panels]]` | on a tab | `kind`, `title`, `command` |
| `[[action]]` | `config.toml` | `name`, `key`, `run` |
| `[ui]` | `config.toml` | `theme`, `glyphs`, `trigger_key` |
| `[hosts]` | `config.toml` | `runtime`, `window` |
| `[[status]]` | all | `at`, `run`, `every`, `title` |
| `[[probe]]` | `config.toml` | `name`, `exec` |

| Variable | Effect |
|---|---|
| `REVIER_CONFIG_HOME` | The configuration directory. |
| `XDG_CONFIG_HOME` | Its parent, when `REVIER_CONFIG_HOME` is not set. |
| `NO_COLOR` | A value that is not empty turns colour off in the TUI. |
