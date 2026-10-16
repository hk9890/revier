# Projects on another machine

A project can live on another machine: a build box, a workstation you reach
over ssh. You link it here, and it is a row in your list like any other. This
page says what a link is, what the other machine needs, how to make one, and
what works over it.

## What a link is

A link is a project file on this machine that points at a project on another
machine, the **host**. The link has no directory here. The checkout, the agents
and the shell all run on the host; this machine shows them in tabs of your own
terminal, each tab an ssh to the host.

The revier on the host is what knows the project. This machine asks it, over
ssh, what the project is and what its agents are doing, and shows that answer.

In the list a link shows as `<name>@<host>`, for example `far@buildbox`, with
what each agent there is doing.

## What the host needs

- A Linux machine you can reach with `ssh <host>`, with a key: revier cannot
  type a password.
- revier installed there, on the `PATH` of your login shell. revier runs every
  command there through `$SHELL -l`, so a `PATH` that only your interactive
  shell sets is not enough.
- The project known there: its own project file under the host's
  `~/.config/revier/projects/`.

The host needs no tmux and no window host. It only runs the agent and the
shell that your tabs here connect to.

The link dialog and `revier link` with no argument offer only the hosts your
`~/.ssh/config` names. A `Host` line with a pattern
(`*`, `?`, `!`) is not offered. `revier link <host> <project>` takes any
destination ssh accepts, `user@machine` included.

## Link a project

From a shell, `revier link` walks the same three steps as the dialog:

```sh
revier link                      # the hosts your ssh configuration names
revier link buildbox             # the projects the revier on buildbox has
revier link buildbox far         # write a link to far on buildbox
revier link buildbox far --name far-box   # the same, under another name here
```

`revier link <host>` prints a table of the host's projects: its path there,
whether it is open, its agent, and the link here that already points at it, if
any, under `LINKED AS`.

`revier link <host> <project>` asks the host first, so a project the host does
not have is refused before a file is written. The link takes the project's own
name unless you give `--name`. A name that a project here already has is
refused.

In the TUI, press **alt+r** (the **remote** button):

1. Pick a host. The list holds the hosts `~/.ssh/config` names, without this
   machine.
2. Enter asks the revier on that host for its projects. Type to filter them.
   The pane shows what the host says about the project under the cursor.
3. Enter on a project asks for the name of the link. The name offered is
   `rs-<host>-<project>`, so it does not take the name of a project here; the
   first character you type replaces it. Enter writes the link.

Esc goes back one step. A project that is already linked is refused, with the
name of the link that points at it.

Some links are refused:

- A link to this machine. Its projects are already in your list.
- A link to a project that is itself a link on the host. Link it on the
  machine that holds the checkout.

## The link file

The link is written to `~/.config/revier/projects/<name>.toml`, beside your
other project files:

```toml
# far: written by `revier link`.
path = "/home/user/dev/far"
git_url = "git@github.com:example/far.git"
[remote]
host = "buildbox"
project = "far"
```

| Field | What it is |
|---|---|
| `path` | The project's directory **on the host**, as the host said when the link was written. |
| `git_url` | The project's repository, as the host said. |
| `[remote] host` | The ssh destination. |
| `[remote] project` | The project's name on the host. Written only when it differs from the link's own name; it defaults to the file's name. |

Neither `path` nor `git_url` names anything on this machine. Nothing is cloned
here, and nothing is created at `path` here. They are kept so that a target of
the link can use them, for example an editor that opens the path on the host.

A `~` in a link's `path` is **not** expanded. A local project's `~` is expanded
against your home directory here, which says nothing about the home directory
on the host. Write the path out in full.

On the host itself, a link that points at that same machine is left out. So
one set of project files can serve both machines: copy or sync them, and each
machine ignores the links to itself. `revier doctor` names the files it
skipped.

## Open the workspace

Enter on the link, or `revier open far`, opens its workspace. You do not
declare the workspace; revier derives it:

- a home target, which is a workspace in your runtime here, and
- in it one tab, named `agent`, with an agent panel and a shell panel.

Each panel runs an ssh to the host. The agent panel runs
`revier agent exec -p far` there, and the shell panel runs
`revier shell exec -p far`. So the host's project file decides which agent
runs and in which directory. When the shell panel of the host's project
declares no command, the shell is your login shell there.

If the checkout is missing on the host, the agent panel clones it there from
the host project's `git_url`.

The row of a link says what the host said. A host that does not answer shows
the link as `unreachable`, and the other projects are not held up. Until the
host has answered once, the link's pane says `surveying`.

## Work with the link's agents

The tabs of a link are tabs of your terminal, so most commands work as they do
in a local workspace:

- `revier agent new -p far` and `revier shell new -p far` add a tab. Its agent
  or shell runs on the host. `--dir` is a path on this machine, so it is
  dropped.
- `revier agent prompt`, `revier agent wait` and `revier agent send-keys`
  work. Each read of the agent is an ssh to the host, so a wait polls every two
  seconds.
- `revier agent read far` does not work: the conversation is on the host. Use
  `revier agent read far --screen`, which reads the panel here.
- `revier agent focus far` and Enter on the agent go to its tab here.
- `revier shutdown far` and del close the tabs here.

An agent ends when its tab closes or its ssh connection drops. `revier session
save` records the conversation each agent of a link holds, as the host names
it, and `revier session restore` starts the agent on the host again on that
conversation.

See [Agents](agents.md) for these commands in full.

## Agents started somewhere else

The host can have agents in the project that no tab here shows: one you
started on the host itself, or one a third machine started through its own
link. Your list and the TUI do not show those agents and do not count them,
because you cannot go to them, type into them or close them from here. A
command that names one fails with `no panel here shows the agent`.
`revier list --json` still reports them.

The same holds on the host. An agent that this machine started there through
`revier agent exec` is not on any row of the host's own TUI, and a shutdown on
the host does not close it. It ends with its tab here.

## Add the link's own targets

A link can declare targets of its own, for example an editor that reaches the
project on the host. Add them to the link file. A link writes its realization
under `[target.remote.window]` or `[target.remote.runtime]`; a
`[target.window]` or `[target.runtime]` in a link file is refused.

```toml
path = "/home/user/dev/far"
git_url = "git@github.com:example/far.git"

[remote]
host = "buildbox"

[[target]]
name = "editor"
key  = "ctrl-shift-o"
  [target.remote.window]
  launch = ["code", "--remote", "ssh-remote+buildbox", "{{.Path}}"]
  match  = { title = "far \\[SSH: buildbox\\]" }
```

The target runs here. `{{.Path}}` and `{{.GitURL}}` are the host's path and
repository, and `{{.Remote.Host}}` is the host. The path is used as written:
a `~` reaches the launch as a literal `~`.

A shared target in `config.toml` reaches a link only when it has a remote
part. The same target can carry both: `[target.window]` for local projects and
`[target.remote.window]` for links.

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

A shared target with no remote part is a local target, and no link has it.

A link may declare its home target, for example to place the workspace. The
home keeps every field it sets, and revier fills in the rest. A list of `tabs`
it sets is dropped: the one tab of a link is the derived `agent` tab. For the
same reason a link's home cannot be named `agent`, and only the home has tabs.

[Configuration](configuration.md) has every field of a target.

## Statuses on a link

A link runs only the `[[status]]` tables of its own file. It takes no status
from `config.toml`, because a shared command is written against a checkout on
this machine. For the same reason a link's table must give `run` itself; one
without `run` is refused, and `revier doctor` says so.

The command runs on this machine. revier sets `REVIER_PROJECT` to the link's
name, `REVIER_PROJECT_PATH` to the path on the host, and `REVIER_PROJECT_HOST`
to the host, so the command can reach the host itself:

```toml
[[status]]
at  = "project-line"
run = ["sh", "-c", "ssh \"$REVIER_PROJECT_HOST\" git -C \"$REVIER_PROJECT_PATH\" branch --show-current"]
```

The statuses on the rule under the top line run once for the whole screen, so links
do not change them. [Statuses](extending/status.md) has the format.

## Actions on a link

`revier run <action> -p far` runs the action **on the host**: revier runs
`revier run <action> -p far` there over `ssh -t`, so an action that asks a
question or draws works as in a shell on the host. The action is the one the
host's `config.toml` defines under that name.

An action key in the TUI is bound by this machine's `config.toml`. On a link's
row it runs the action of the same name on the host, so the host must define it
too.

The host records the action in its own event file. `revier events` here shows
it with the host's name. See [Projects](projects.md) for actions and events.

## The ssh connection

Every call revier makes to a host is an ssh:

- A call that reads an answer, such as a survey or the link dialog, runs in
  batch mode. It never asks for a password and gives up after five seconds
  without an answer.
- A panel of a link is not in batch mode, so it can answer a prompt.
- Every call sends keepalives, so a connection to a network that went away
  ends within about a minute.

All calls to one host share one connection. The connection stays up for ten
minutes after the last call, so the TUI can survey the host many times without
a new handshake each time. Its control socket is under
`$XDG_RUNTIME_DIR/revier/`. To end the connection early:

```sh
ssh -O exit -o ControlPath="$XDG_RUNTIME_DIR/revier/ssh-%C" buildbox
```

Without `XDG_RUNTIME_DIR`, nothing is shared and every call makes its own
connection.

When a call fails, revier says why in one line:

| It says | What to do |
|---|---|
| `host key not known - run ssh <host> once to accept it` | Connect once by hand and accept the key. |
| `ssh refused the key - revier cannot type a password, so set up key auth` | Set up key authentication to the host. |
| `hostname does not resolve` | Check the name, or the `HostName` in `~/.ssh/config`. |
| `no answer on port 22`, `nothing is listening on port 22`, `the machine is not reachable` | The host is down, or the network or VPN is. |
| `no shell there - it is a git host, not a machine` | The destination is a git server. Link a real machine. |
| `no revier on the PATH ssh gives - put mise or asdf shims in ~/.zshenv` | revier is not on the `PATH` of your login shell on the host. |
| `the revier there has no <command> - update revier on <host>` | The host runs an older revier. Update it. |

## What does not work over a link

- `revier agent read` without `--screen`: the conversation is on the host.
- `--dir` on `revier agent new` and `revier shell new`: it is dropped.
- Agents that no tab here shows: they are not listed and cannot be reached.
- Statuses from `config.toml`: a link runs only its own.
- Shared targets without a remote part.
- Actions defined only on this machine: an action runs on the host.
- `revier each` skips links, since the directory is not here.
- A password login: the host must accept a key.
- A `~` in the link's `path`, or in a target that renders it.

## Quick reference

| To | Do |
|---|---|
| List the hosts you can link to | `revier link` |
| List a host's projects | `revier link <host>` |
| Link a project | `revier link <host> <project> [--name <name>]`, or alt+r in the TUI |
| Open the workspace | Enter on the row, or `revier open <name>` |
| Add an agent or shell tab | `revier agent new -p <name>`, `revier shell new -p <name>` |
| Read the agent | `revier agent read <name> --screen` |
| Run an action on the host | `revier run <action> -p <name>` |
| End the shared ssh connection | `ssh -O exit -o ControlPath="$XDG_RUNTIME_DIR/revier/ssh-%C" <host>` |
| See links this machine skipped | `revier doctor` |
