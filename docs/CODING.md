# Coding

```bash
mise run build      # ./bin/revier
mise run fmt        # goimports -local; plain gofmt leaves import groups the gate rejects
mise run quality    # the pre-commit gate
```

## Where a change goes

The tree does not tell you. An adapter is named after a tool but holds none of
the behaviour associated with it.

| Change | Package |
|---|---|
| A new port, or a type crossing the port boundary | `pkg/revier` |
| Any decision: which realization wins, what matches, when to toggle | `internal/core` |
| Speaking to one tool | `internal/adapter/<tool>` |
| A test fake | `internal/hosttest` |

Adding a public type to `pkg/revier` widens the surface an out-of-tree adapter
depends on. Put it in `internal/` unless an adapter must name it.

## Adapters hold no policy

Matching, template rendering, choosing between realizations, and toggle-back all
live in `internal/core`. An adapter that decides any of them lets two adapters
disagree about what a match means.

Concretely, an adapter never: compiles a `Match`, reads `Target`, `Project`, or
`Prefer`, or expands a `{{ }}` template. A `Realization` arrives rendered.

## Two invariants

**`Open` must produce an instance that the same realization's `Match` finds.**
Otherwise every keypress opens another copy. `Realization.Name` exists for hosts
that assign their own identity — a tmux window name, a browser `--class`. See
`internal/adapter/tmux.Host.Open`.

**`Instances` must cost the same whatever the project count.** It runs on every
TUI refresh, and this repository expects roughly ninety projects.

- Query in bulk: `tmux list-panes -a`, `kitten @ ls`, `wctl list --json`.
- A constant number of calls is fine. tmux needs two, because its format output
  carries one free-text field per line.
- Calls that scale with something else are fine. kitty issues one `ls` per
  kitty process, concurrently, because each process has its own socket.
- One call per project is not.

## Errors

Return a normal outcome as a sentinel, not as a failure. `core.ErrNoHost` means
a target is unavailable on this machine — a window-only target with no window
host — and the survey renders it as `available: false` rather than erroring.
Reserve errors for a tool that misbehaved.

A probe that fails reports `StatusUnknown`. One broken harness must not blank
the dashboard.
