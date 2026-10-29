# Projects on another machine

A project can live on another machine, the **host**: a build box, or a
workstation you reach over ssh. You link it here, and it is a row in your list
like any other, shown as `<name>@<host>`, for example `far@buildbox`.

The checkout, the agents and the shell run on the host. This machine shows them
in tabs of your own terminal, each tab an ssh to the host. The revier on the
host knows the project; this machine asks it over ssh.

## What you need

- On this machine: a runtime host. A link's workspace opens in it.
- The host: a Linux machine you reach with `ssh <host>` and a key. revier
  cannot type a password.
- On the host: revier on the `PATH` of your **login** shell. revier runs each
  command through `$SHELL -l`, so a `PATH` that only your interactive shell
  sets is not enough.
- On the host: the project, in its own project file. The host needs no tmux
  and no desktop.

## Add the machine

The remote screen lists the machines you added, not every host in
`~/.ssh/config`. To add one, press **alt+r**, then **alt+n** (`new` on the
screen's bar). Pick a host of your `~/.ssh/config`, and press Enter. revier
writes it to `config.toml` at once ([Machines](configuration.md#machines)).

Each row says what the machine answered when the screen opened:

| Row | Means |
|---|---|
| `checking…` | No answer yet. The other rows do not wait for it. |
| `revier 0.21.0` | The machine answers and runs that version of revier. |
| `no revier` | The machine answers, but revier is not on the `PATH` of its login shell. |
| `does not answer` | ssh failed. The pane on the right says why. |

To take a machine off the list, press **alt+d** (`remove`) on its row. If a
link still names the machine, revier asks first. The link stays and still
works.

## Link a project

In the TUI, press **alt+r**. Pick a machine, then a project (type to filter),
then give the link a name. Esc goes back one step.

From a shell:

```sh
revier link                              # the machines you added, as alt+r lists them
revier link buildbox                     # the projects the revier on buildbox has
revier link buildbox far                 # write a link to far on buildbox
revier link buildbox far --name far-box  # the same, under another name here
```

`revier link buildbox` works for any host ssh reaches, also one you did not
add.

`revier link buildbox far` writes `projects/far.toml` in the configuration
directory ([Files](configuration.md#files)):

```toml
# far: written by `revier link`.
path = "/home/user/dev/far"
git_url = "git@github.com:example/far.git"
[remote]
host = "buildbox"
```

`path` and `git_url` are the host's; nothing is cloned here.
[Link file](configuration.md#link-file) has every field, and says why you
can copy the same project files to both machines.

## Open and work with the workspace

Enter on the row, or `revier open far`, opens the workspace. You do not declare
it: revier makes one tab, `agent`, with an agent panel and a shell panel. Each
panel is an ssh to the host, so the host's project file decides which agent
runs and where. If the checkout is missing on the host, the agent panel clones
it there.

The `revier agent` commands work as they do for a local project; see
[Agents](agents.md). `revier agent new -p far` adds a tab whose agent runs on
the host. `revier agent new dtop -p far` adds a tab of the named agent dtop,
which the host's configuration declares. An agent ends when its tab closes or
its ssh connection drops. `revier session save` records each agent's
conversation and named agent, and `revier session restore` starts it again on
the host.

A host that does not answer shows the link as `unreachable`. The other rows do
not wait for it.

## Targets, statuses and actions

- **Targets** run on this machine; `{{.Path}}` is the path on the host. Which
  targets a link takes is in [Link file](configuration.md#link-file).
- **Statuses** run on this machine, with `REVIER_PROJECT_HOST` set so the
  command can ssh to the host. See [Statuses](extending/status.md#per-project).
- **Actions** run **on the host**: `revier run <action> -p far`, and an action
  key in the TUI, run the host's action of that `name`
  ([Actions](configuration.md#actions)). When the host has none, revier says
  so.

## The ssh connection

All calls to one host share one ssh connection. It stays open for ten minutes
after the last call. To close it now:

```sh
ssh -O exit -o ControlPath="$XDG_RUNTIME_DIR/revier/ssh-%C" buildbox
```

A call that reads an answer never asks for a password and gives up after five
seconds. When a call fails, revier says why in one line and what to fix, for
example `host key not known - run ssh <host> once to accept it`.

## What does not work over a link

- `revier agent read` without `--screen`: the conversation is on the host.
- `--dir` on `revier agent new` and `revier shell new`: it is dropped.
- An agent that no tab here shows, such as one you started on the host: it is
  not in your list, and you cannot reach it from here.
- Statuses from `config.toml`, shared targets without a remote part, and a
  `~` in `path` ([Link file](configuration.md#link-file)).
- `revier each`: it skips links.
