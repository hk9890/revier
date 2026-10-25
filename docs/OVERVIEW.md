# Overview

A findability map: where things live, and how to search for them.

## Layout

| Path | Holds |
|---|---|
| `pkg/revier/` | The ports and every type that crosses them. Public, because out-of-tree adapters import it. |
| `internal/core/` | All policy: resolution, matching, run-or-raise, toggle-back, survey. |
| `internal/app/` | The use cases that cross stores, written once for both surfaces: create, link and rename a project, run an action, save a session. |
| `internal/adapter/<tool>/` | One tool each. No policy. `kitty`, `tmux` and `herdr` (runtime; `kitty` also the terminal keys, `herdr` also a probe), `gnome` (window and key binder), `proc` (what a link on another machine started here, and a process's parent on this machine), `claude` (probe, and the assistant launcher in `assist.go`), `opencode` and `execprobe` (probes), `ssh` (remote). |
| `internal/config/` | TOML loading and the validation that rejects a project before a keypress can fail on it; the template `revier new` writes. |
| `internal/state/` | What revier learned at runtime: the last project, and hand-attached instances. |
| `internal/ledger/` | The state file and the event file as the core reads and writes them, and the state record's line for each change a state write made: the one caller of the state lock. |
| `internal/session/` | The saved sets of open projects `revier session` writes and reads, under the state root, and `seen.toml`, what a surface last saw, which an end with no shutdown saves. |
| `internal/checkout/` | git and mise on a project's directory: the origin a new project records, the clone of a missing one. |
| `internal/procgroup/` | The process group each host command runs in, and the end of every group still running when a second ctrl+c ends revier. |
| `internal/fsutil/` | The atomic file write every store uses, and the write through a link that config.toml and the terminal's configuration use. |
| `internal/logging/` | The daily log file every process appends to, and the helpers that time an operation. |
| `internal/events/` | The event file `revier events` prints: what revier did in each project, never pruned. |
| `internal/record/` | The state record `revier state` replays: what each operation did, failed or not, and what each surface's survey saw, never pruned. |
| `internal/runlog/` | The record of every `revier each`: each project's output and a summary, under the state root. |
| `internal/sshconfig/` | The hosts `~/.ssh/config` names, for the link dialog and `revier link`. |
| `internal/hosttest/` | The fake host, probe, key binder, terminal keys and remote that layer L2 runs against. |
| `internal/testenv/` | The scratch configuration, state, ssh configuration and kitty configuration a test binary runs in, in place of the user's own; the empty PATH of a test that must start no tool. |
| `internal/tui/` | The TUI surface: the project list, the pane, the dialogs and the config screen. |
| `internal/theme/` | Every colour and glyph the TUI draws with, named by role. |
| `internal/build/` | The version, commit and date that `.goreleaser.yaml` and `.mise.toml` stamp in by ldflags. |
| `scripts/drive/` | Manual headless driver. |
| `scripts/release-notes` | One version's `CHANGELOG.md` section, the text of its GitHub release. |
| `scripts/demo/` | The README's clips, `.github/assets/*.webp`: `record`, `compose.py` and `keycap.py` run here, `guest/` on a VM's GNOME desktop. |
| `docs/user-guide/` | The [user guide](user-guide/README.md). `embed.go` carries its reference pages (`configuration.md`, `extending/`) into the binary, which `revier assist --reference` prints. |
| `cmd/revier/` | CLI entry point. `adapters.go` constructs every port's adapter; `assist.go` and `each.go` call adapter helpers directly, and `serve.go` reads `proc`'s variable names. |

## The ports

- `Host`, with `Runtime` and `WindowController`: `pkg/revier/host.go`
- `AgentProbe`: `pkg/revier/agent.go`
- `Remote`: `pkg/revier/remote.go`
- `KeyBinder`: `pkg/revier/keys.go`
- `TermKeys`: `pkg/revier/termkeys.go`

What each port is: [docs/design/architecture.md](design/architecture.md#five-ports).

## Finding things

```bash
rg 'func \(c \*Core\)' internal/core      # every decision the core makes
rg 'revier\.Host' --type go               # what implements or consumes the port
rg -l '//go:build live'                   # tests needing a real substrate
rg 'ErrNo' pkg internal                   # the normal-outcome sentinels
rg 'func cmd' cmd/revier                  # every CLI command
rg 'D85\b' docs                           # replace 85 with the cited number: the entry, the design text tagged (D85), and docs/ citations
```

Behaviour is almost never in the adapter named after the tool. Search
`internal/core` first.

## Outside this repository

- `wctl` — <https://github.com/carlo9890/gnome-window-control>, the GNOME window
  host's dependency.
