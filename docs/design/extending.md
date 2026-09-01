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

# The workspace. Home is where toggle-back returns.
[[target]]
name = "home"
key  = "ctrl-shift-h"
home = true

  [target.runtime]
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
slow.

Only `AgentProbe` has a subprocess form. A `Host` is stateful and sits in the
latency path of every keystroke, so it is Go or nothing.

*Designed, not built in the first version.*

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
a single bulk query — `kitten @ ls`, `tmux list-panes -a -F`, `swaymsg -t
get_tree` — never one call per project.

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
