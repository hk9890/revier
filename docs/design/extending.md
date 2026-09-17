# Extending

Three levels, in the order most people need them. Interface signatures are in
[interfaces.md](interfaces.md).

## Level 1 — configuration

No code. Covers new projects, the targets bound to them, and how each target is
realized.

A project, in `~/.config/revier/projects/<name>.toml`:

```toml
name = "revier"
path = "~/dev/github/revier"
# Optional. Opening the project clones it here when path is missing.
git_url = "git@github.com:hk9890/revier.git"

# The workspace. Home is where toggle-back returns.
[[target]]
name = "home"
key  = "ctrl-shift-h"
home = true

  [target.runtime]
  name  = "session:{{.Name}}"
  match = { title = "^session:{{.Name}}$" }

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
target's instance, not as an instance of its own (decisions.md D64). The tab
needs a `launch`, and no `match` or `name`: revier marks the tab it opens and
finds it again by that mark. It needs a runtime with tabs: kitty, where it is
a tab of the OS window, or tmux, where it is a window of the session (D70). On
any other runtime the key is refused with the reason.

```toml
[[target]]
name = "tickets"
key  = "ctrl-shift-t"

  [target.runtime]
  inside = "home"
  launch = ["taskmgr-ui"]
```

Targets most projects have in common are declared once, in
`~/.config/revier/config.toml`, in the same form (decisions.md D59). Every
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
such as `panels` or `launch` - replaces the shared one whole.

A link, in the same directory, is a project on another machine
(decisions.md D41). It has a `[remote]` table and no directory here:

```toml
[remote]
host = "buildbox"   # the ssh destination
project = "far"     # its name there; defaults to this file's name

# Optional: the path on the host, for templates in targets declared here.
path = "~/dev/far"

# Optional: further targets, windows here that reach the project there. The
# home target, the ssh pane onto the workspace, is derived; a declared one
# takes the pane for every field it leaves out - a placement alone is enough.
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
the link's (decisions.md D80). A project file writes the part of its own kind
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
{"id":"3","kind":"agent","title":"✳ refactoring the store","vars":{},"pid":41221,"command":["aider"]}
```

```json
{"harness":"aider","status":"running","activity":"refactoring the store"}
```

`status` is one of `unknown`, `idle`, `running`, `attention`. A non-zero exit,
malformed JSON, or a timeout means the panel reports `unknown`. One process per
panel per refresh, so a probe stays cheap or it becomes the reason the TUI feels
slow; the timeout is half a second, and a probe that overruns it is killed with
everything it started.

`name` is also the foreground command the probe claims. A probe named `aider`
reads panels running `aider` and no others, so an unrelated agent pane never
costs a process, and a compiled-in probe for the same harness wins.

Only `AgentProbe` has a subprocess form. A `Host` is stateful and sits in the
latency path of every keystroke, so it is Go or nothing.

## Level 3 — a Go host

For a new terminal, a new multiplexer, or a new compositor.

1. Implement `Host` in `internal/adapter/<name>/`. Five methods: `Name`,
   `Probe`, `Instances`, `Open`, `Focus`, plus `Focused`.
2. Implement `Capabilities` as well when the adapter is a `Runtime`, and return
   panels on the instances it lists. Only a runtime host can see inside a
   terminal, and the agent monitor reads nothing else.
3. Make `Probe(ctx)` return nil only when the adapter can genuinely work. It is
   how automatic selection stays correct on a machine that has several.
4. Add one line to `cmd/revier/adapters.go`.
5. Ship a test that runs without the tool installed. Adapters are tested against
   recorded output of the tool's query command, not against the live tool.

`Instances` is the one to get right. It runs on every TUI refresh, so it must be
a single bulk query — `kitten @ ls`, `tmux list-panes -a -F`, `wctl list
--json` — never one call per project.

An adapter holds no policy. Matching, template rendering, resolution between
realizations, and toggle-back all live in the core, so two adapters can never
disagree about what a match means.

Report `Capabilities.Layout` as false and open a single pane when the runtime
cannot arrange panes. The core handles that case rather than the adapter faking
it.

## What is not extensible, on purpose

The store is TOML files on disk. The output formats are the TUI and `--json`.
Neither is behind an interface, because neither has a second implementation
anyone has asked for. When one appears, the interface gets written then.
