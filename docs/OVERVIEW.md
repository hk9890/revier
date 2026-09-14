# Overview

A findability map: where things live, and how to search for them.

## Layout

| Path | Holds |
|---|---|
| `pkg/revier/` | The ports and every type that crosses them. Public, because out-of-tree adapters import it. |
| `internal/core/` | All policy: resolution, matching, run-or-raise, toggle-back, survey. |
| `internal/adapter/<tool>/` | One tool each. No policy. `kitty` and `tmux` (runtime), `gnome` and `sway` (window), `claude`, `opencode`, and `execprobe` (probes), `ssh` (remote). |
| `internal/config/` | TOML loading and the validation that rejects a project before a keypress can fail on it; the template `revier new` writes. |
| `internal/state/` | What revier learned at runtime: the last project, and hand-attached instances. |
| `internal/session/` | The saved sets of open projects `revier session` writes and reads, under the state root. |
| `internal/checkout/` | git and mise on a project's directory: the origin a new project records, the clone of a missing one. |
| `internal/logging/` | The daily log file every process appends to, and the helpers that time an operation. |
| `internal/runlog/` | The record of every `revier each`: each project's output and a summary, under the state root. |
| `internal/sshconfig/` | The hosts `~/.ssh/config` names, for the link dialog and `revier link`. |
| `internal/hosttest/` | The fake host, probe, key binder and remote that layer L2 runs against. |
| `internal/tui/` | The TUI surface: the project list, the pane, the dialogs and the config screen. |
| `internal/theme/` | Every colour and glyph the TUI draws with, named by role. |
| `internal/build/` | The version, commit and date that `.goreleaser.yaml` and `.mise.toml` stamp in by ldflags. |
| `scripts/drive/` | Manual headless driver. |
| `scripts/release-notes` | One version's `CHANGELOG.md` section, the text of its GitHub release. |
| `contrib/gnome/` | The shipped GNOME desktop-key scripts: `revier-go` runs a target, `revier-popup` opens the TUI. |
| `cmd/revier/` | CLI entry point, and the one file that wires adapters (`adapters.go`). |

## The three ports

`Host` (`pkg/revier/host.go`) provides instances of targets. `Runtime` and
`WindowController` are the same interface with different providers behind them.
`AgentProbe` (`pkg/revier/agent.go`) reads a panel and reports what an agent is
doing. `Remote` (`pkg/revier/remote.go`) is the revier on another machine,
asked about the projects that live there.

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
