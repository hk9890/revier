# Overview

A findability map. What revier *is* and why it is shaped this way is
[design/](design/README.md); this file is where things live.

## Layout

| Path | Holds |
|---|---|
| `pkg/revier/` | The ports and every type that crosses them. Public, because out-of-tree adapters import it. |
| `internal/core/` | All policy: resolution, matching, run-or-raise, toggle-back, survey. |
| `internal/adapter/<tool>/` | One tool each. No policy. `kitty` and `tmux` (runtime), `gnome` (window), `claude` (probe). |
| `internal/config/` | TOML loading and the validation that rejects a project before a keypress can fail on it. |
| `internal/state/` | What revier learned at runtime: the last project, and hand-attached instances. |
| `internal/hosttest/` | The fake host and fake probe that layer L2 runs against. |
| `scripts/drive/` | Manual headless driver ([RUNNING.md](RUNNING.md)). |
| `cmd/revier/` | CLI entry point, and the one file that wires adapters (`adapters.go`). |
| `docs/design/` | The specification and the decision log. |

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
