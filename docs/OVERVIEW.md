# Overview

A findability map: where things live, and how to search for them.

## Layout

| Path | Holds |
|---|---|
| `pkg/revier/` | The ports and every type that crosses them. Public, because out-of-tree adapters import it. |
| `internal/core/` | All policy: resolution, matching, run-or-raise, toggle-back, survey. |
| `internal/adapter/<tool>/` | One tool each. No policy. `kitty` and `tmux` (runtime), `gnome` and `sway` (window), `claude`, `opencode`, and `execprobe` (probes). |
| `internal/config/` | TOML loading and the validation that rejects a project before a keypress can fail on it; the template `revier new` writes. |
| `internal/state/` | What revier learned at runtime: the last project, and hand-attached instances. |
| `internal/checkout/` | git and mise on a project's directory: the origin a new project records, the clone of a missing one. |
| `internal/runlog/` | The record of every `revier each`: each project's output and a summary, under the state root. |
| `internal/hosttest/` | The fake host and fake probe that layer L2 runs against. |
| `scripts/drive/` | Manual headless driver. |
| `scripts/release-notes` | One version's `CHANGELOG.md` section, the text of its GitHub release. |
| `scripts/migrate-sessions/` | One-shot converter, `.session` files to project TOML. Not part of the product (`docs/design/decisions.md` D5). |
| `cmd/revier/` | CLI entry point, and the one file that wires adapters (`adapters.go`). |
## The two ports

`Host` (`pkg/revier/host.go`) provides instances of targets. `Runtime` and
`WindowController` are the same interface with different providers behind them.
`AgentProbe` (`pkg/revier/agent.go`) reads a panel and reports what an agent is
doing.

## Finding things

```bash
rg 'func \(c \*Core\)' internal/core      # every decision the core makes
rg 'revier\.Host' --type go               # what implements or consumes the port
rg -l '//go:build live'                   # tests needing a real substrate
rg 'ErrNo' pkg internal                   # the normal-outcome sentinels
rg 'func cmd' cmd/revier                  # every CLI command
```

Behaviour is almost never in the adapter named after the tool. Search
`internal/core` first.

## Outside this repository

- The `os` command in `~/setup/scripts/sessions/` is the shell implementation
  revier replaces. It is not being ported; see `docs/design/decisions.md` D1.
- `wctl` — <https://github.com/carlo9890/gnome-window-control>, the GNOME window
  host's dependency.
