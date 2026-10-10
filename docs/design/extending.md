# Extending

Three levels, in the order most people need them. Interface signatures are in
[interfaces.md](interfaces.md).

## Level 1 — configuration

No code. Covers new projects, the targets bound to them, and how each target is
realized.

A project, in `~/.config/revier/projects/revier.toml`. The file name is the
project's name (D80):

```toml
path = "~/dev/github/revier"
# Optional. Opening the project clones it here when path is missing.
git_url = "git@github.com:hk9890/revier.git"

# The workspace. Home is where toggle-back returns. It is the window alone:
# what runs in it is the tabs it lists.
[[target]]
name = "home"
key  = "ctrl-shift-h"
home = true

  [target.runtime]
  name  = "session:{{.Name}}"
  match = { title = "^session:{{.Name}}$" }
  tabs  = ["agent"]

# The tab of the workspace: an agent beside a shell.
[[target]]
name = "agent"

  [target.runtime]
  inside = "home"

    [[target.runtime.panels]]
    kind    = "agent"
    title   = "claude"
    command = ["claude"]

    [[target.runtime.panels]]
    kind = "shell"

# An editor: a window on a desktop, nothing on a headless box.
[[target]]
name = "editor"
key  = "ctrl-o"

  [target.window]
  launch = ["code", "{{.Path}}"]
  match  = { class = "^code$", title = "{{.Name}}" }

# A diff viewer, realized either way. On a desktop the window wins; over SSH
# the runtime realization is the only one available.
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

# A page. --class gives the window an identity to match on; a normal browser
# window has none.
[[target]]
name = "pulls"
key  = "ctrl-g"

  [target.window]
  launch = ["google-chrome", "--app=https://github.com/hk9890/revier/pulls",
            "--class=revier-{{.Name}}-pulls"]
  match  = { class = "^revier-{{.Name}}-pulls$" }
```

`launch` runs only when `match` finds nothing. Every target is therefore a
run-or-raise binding, not a launcher.

A target with no `key` appears in the project picker instead. An instance bound
with `revier attach` also appears there and needs no config at all.

Set `prefer = "runtime"` on a target to override which realization wins when
both hosts are available.

Set `inside` on a runtime realization to open the target as a tab of another
target's instance, not as an instance of its own (D64). The tab
takes a `launch` or `panels`, one of the two, and no `match` or `name`: revier
marks the tab it opens and finds it again by that mark. A tab is the one place
a file has `panels`: on a runtime realization with no `inside` they are
refused at load, with the fix (D128). A tab needs a runtime with tabs: kitty,
where it is a tab of the OS window, or tmux, where it is a window of the
session (D70). On any other runtime the key is refused with the reason.

```toml
[[target]]
name = "tickets"
key  = "ctrl-shift-t"

  [target.runtime]
  inside = "home"
  launch = ["taskmgr-ui"]
```

`tabs` on the target the tabs are inside opens its instance with them (D126).
It lists the tab targets in the order they open, and `active` names the one
that has the focus; with no `active` it is the first entry. The target is then
only the window, and has no `launch`. To open the `home` above with the ticket
viewer first and the agent in focus:

```toml
  [target.runtime]
  name   = "session:{{.Name}}"
  match  = { title = "^session:{{.Name}}$" }
  tabs   = ["tickets", "agent"]
  active = "agent"
```

Both keys apply only when revier creates the instance: one that is open keeps
its tabs and its focus. A press on the key of a listed tab that creates the
instance leaves the focus on that tab. A tab that is not listed opens at its
key. A target with no `tabs` runs its `launch`. On a runtime with no tabs the
key of a target with `tabs` is refused with the reason, so such a runtime
opens a `launch` alone.

A listed tab that fails to open stops the ones after it: the instance stays
open with the tabs it has, and the press reports it with the error (D126). A
save records no step for a listed tab, because it comes back with its
instance.

The agent panel and the shell panel of a workspace are the first of each kind
in its listed tabs, in order (D128). `revier agent new` and
`revier shell new` copy those, and `-p <project>:agent` names the workspace as
`-p <project>:home` does. A save records the agents in the panels of a listed
tab under the workspace with their conversations, and a restore resumes them
in those panels; a tab that runs a `launch` is opened without a resume (D68).

Targets most projects have in common are declared once, in
`~/.config/revier/config.toml`, in the same form (D59). Every
project with a realization of its kind in them gets them, and its file then
holds only what is its own:

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
  launch = ["google-chrome", "--app=https://github.com/hk9890/platform/pulls"]
  match  = { class = "^chrome-github" }
```

A table merges key by key, at any depth. Any other value - a string, a list
such as `tabs`, `panels` or `launch` - replaces the shared one whole.

A link, in the same directory, is a project on another machine
(D41). It has a `[remote]` table and no directory here:

```toml
# The path and the repository are the host's, as `revier link` recorded them:
# a target here renders them, and neither names anything on this machine
# (D83). An argument that renders to nothing - a path the file
# does not hold - is refused at load rather than launched. The path is
# written out in full: a local project's "~" is expanded at load against the
# home directory here, and a link's is not, so a "~" written here reaches the
# launch below as a tilde.
path = "/home/user/dev/far"
git_url = "git@github.com:example/far.git"

[remote]
host = "buildbox"   # the ssh destination
project = "far"     # its name there; defaults to this file's name

# Optional: further targets, windows here that reach the project there. The
# home target and its one tab, agent, are derived: an agent panel and a shell
# panel, each an ssh onto the workspace (D84, D128). A declared home keeps
# every field it sets - a placement alone is enough - and a tabs it sets is
# dropped. A home that takes the derived tab cannot have the name agent, which
# is the tab's (D128).
[[target]]
name = "editor"
key  = "ctrl-shift-o"
  [target.remote.window]
  launch = ["code", "--remote", "ssh-remote+buildbox", "{{.Path}}"]
  match = { title = "far \\[SSH: buildbox\\]" }
```

A target reaches a local project and a link with different tools, so it
carries a realization for each: `[target.window]` and `[target.runtime]` are
the local project's, `[target.remote.window]` and `[target.remote.runtime]`
the link's (D82). A project file writes the part of its own kind
and is refused if it writes the other, which is why the editor above is under
`[target.remote.window]`.

Both parts together are written in `config.toml`, where one target serves
every project. A shared target with no remote part is a local target, and no
link has it:

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

Actions are for commands that produce no instance to return to — a script, a
sync, a clipboard copy. They live in `~/.config/revier/config.toml`:

```toml
[[action]]
key  = "ctrl-y"
name = "copy path"
run  = ["wl-copy", "{{.Path}}"]
```

Templates are Go `text/template` over `Project`, rendered by the core before a
host sees them. `run` and `launch` are argv lists, not shell strings: there is
no quoting to get wrong and no shell to inject into.

Anything specific to one person — an editor, a ticket viewer, a browser, a
company tool — is a target or an action. It never becomes code in this
repository.

## Level 2 — an external agent probe

For a coding agent revier does not know yet, without writing Go.

Declare it:

```toml
[[probe]]
name = "aider"
exec = "~/bin/aider-probe"
```

revier writes one `Panel` as JSON on the probe's stdin and reads one
`AgentState` as JSON from its stdout:

```json
{"id":"3","kind":"tool","title":"✳ refactoring the store","vars":{},"pid":41221,"command":["aider"]}
```

```json
{"harness":"aider","status":"running","activity":"refactoring the store"}
```

`status` is one of `unknown`, `idle`, `running`, `attention`. A non-zero exit,
malformed JSON, or a timeout means the panel reports `unknown`. One process per
panel per refresh, so a probe stays cheap or it becomes the reason the TUI feels
slow; the timeout is half a second, and a probe that overruns it is killed with
everything it started.

The probe runs in revier's working directory, which
[architecture.md](architecture.md#where-the-work-happens) sets. A relative
`exec` resolves against the home directory in every command.

`name` is also the foreground command the probe claims. A probe named `aider`
reads panels running `aider` and no others, so an unrelated agent pane never
costs a process, and a compiled-in probe for the same harness wins.

Only `AgentProbe` has a subprocess form. A `Host` is stateful and sits in the
latency path of every keystroke, so it is Go or nothing.

## Level 3 — a Go host

For a new terminal, a new multiplexer, or a new compositor.

1. Implement `Host` in `internal/adapter/<name>/`: the methods
   [interfaces.md](interfaces.md#host) lists.
2. Implement `Capabilities` as well when the adapter is a `Runtime`, and return
   panels on the instances it lists. Only a runtime host can see inside a
   terminal, and the agent monitor reads nothing else. A panel's kind is
   `shell` where `revier.IsShell` says so and `tool` otherwise, never `agent`
   (D119).
3. Make `Probe(ctx)` return nil only when the adapter can genuinely work. It is
   how automatic selection stays correct on a machine that has several.
4. Add one line to `cmd/revier/adapters.go`.
5. Ship the host tests
   [TESTING.md](../TESTING.md#what-every-host-test-must-cover) requires.

What an adapter must never decide, and what `Instances` may cost, are in
[CODING.md](../CODING.md#adapters-hold-no-policy) and its
[two invariants](../CODING.md#two-invariants).

## What is not extensible, on purpose

The store is TOML files on disk. The output formats are the TUI and `--json`.
Neither is behind an interface, because neither has a second implementation
anyone has asked for. When one appears, the interface gets written then.
