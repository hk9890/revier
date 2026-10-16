# Projects

A project is one TOML file in `projects/` in the configuration directory
([Files](configuration.md#files)). The file name is the project's name:
`projects/shop.toml` is the project `shop`.

## Make a project

Go to the project's directory and run `revier new`:

```bash
cd ~/dev/shop
revier new    # the project is called shop; or: revier new storefront
```

A directory can be one project only. `revier new` prints the file it wrote.
`revier open shop`, with a name revier does not know, makes the project for
the current directory and opens it. In the TUI, alt+n takes a path, a name or
a git URL. In a git checkout, the file records `origin` as `git_url`, and
`revier open` clones the directory from it when it is missing.

The file gives you a workspace with an agent tab:

```toml
path = "~/dev/shop"
git_url = "git@github.com:example/shop.git"

[[target]]
name = "home"            # the workspace that toggle-back returns to
home = true
  [target.runtime]
  name = "session:{{.Name}}"              # {{.Name}}: the project's name
  match = { title = "^session:{{.Name}}$" } # ^ and $: the whole title
  tabs = ["agent"]                        # tabs that open with home

[[target]]
name = "agent"           # the agent beside a shell
  [target.runtime]
  inside = "home"        # this target is a tab of home
    [[target.runtime.panels]]
    kind = "agent"       # the panel revier reads the agent state of
    title = "claude"
    command = ["claude"]
    [[target.runtime.panels]]
    kind = "shell"
    title = "shell"
```

When `config.toml` has no shared target, the file also gets an `editor`
target that opens IntelliJ IDEA. Change its `launch` and `match` to your
editor, or put your editor in `config.toml`, as below. Reach a target with
`revier go editor` or from the TUI. After each edit, run `revier doctor`: it
names what did not load and how to fix it. Every field is in
[Configuration](configuration.md#project-file), and the `{{...}}` values are
in [Templates](configuration.md#templates).

To rename a project, close it, then use the project screen in the TUI (alt+e).
revier moves the file and changes the name in every saved session.

## Share targets and actions

A target that most projects have, such as an editor, goes once into
`config.toml`, in the same form as in a project file. Every project then has
it. Give keys there: a key binds to a target's name, so it means the same
target in every project. A project changes a shared target with a target of
the same name that holds only the fields it changes; see
[Shared targets](configuration.md#shared-targets).

An action is a command that opens nothing to return to, such as a copy to the
clipboard. Actions live in `config.toml` only:

```toml
[[action]]
key  = "ctrl-y"
name = "copy path"
run  = ["wl-copy", "{{.Path}}"]
```

In the TUI, the action's `key` runs it on the selected project. From a shell:

```bash
revier run "copy path"            # in the project of this directory
revier run "copy path" -p shop    # in the project shop
```

The command runs in the project's directory. The fields are in
[Actions](configuration.md#actions).

## Run one command in every project

Pull every repository before you start the day:

```bash
revier each -- git pull --ff-only
```

Projects run one at a time, with no input. Each project prints one line:
`ok`, `failed`, or why it was skipped. The output of each project goes to a
log, not to the terminal: `revier each log` shows it. `revier each --help`
has the filters.

## Save and restore sessions

A session is the set of open projects and which of their targets are open.
Save one before a restart, and restore it after:

```bash
revier session save --name work   # the name is optional
revier session list
revier session restore            # the newest session
revier session restore work --dry-run
```

A shutdown, and closing a whole project in the TUI, save a session for you.
A session records names, not windows. A restore reads the project files as
they are now, leaves open targets as they are, and steps over a project or
target that is gone. A Claude Code agent comes back on its conversation, in
the directory it worked in; see [Agents](agents.md#resume-an-agent). Other
agents start fresh in the project directory. An attached window is not saved.
In the TUI, alt+s opens the sessions screen.

## Shut everything down

```bash
revier shutdown                  # every open project
revier shutdown shop --agents    # only the agents of shop; an agent tab closes with its shell
revier shutdown --dry-run        # print the plan, close nothing
```

A shutdown first saves the session when it changed, so `revier session
restore` brings back what it closed. While an agent works or waits for an
answer, nothing closes and revier names the agent; `--force` closes it
anyway. In the TUI, alt+q shows the plan before anything closes.

## See what revier did

To see which projects you used most this week:

```bash
revier events --days 7 | jq -r .project | sort | uniq -c | sort -rn
```

`revier events` prints one JSON line each time you used a project: a target
opened or raised, an agent or shell tab added, an action run.
