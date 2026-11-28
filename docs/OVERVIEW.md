# Overview

A findability map: where things live, and how to search for them.

## Layout

| Path | Holds |
|---|---|
| `pkg/revier/` | The ports and the types that cross them, the project model included. |
| `internal/core/` | All policy: resolution, matching, run-or-raise, toggle-back, survey. |
| `internal/app/` | The use cases that cross stores, for both surfaces. |
| `internal/adapter/<tool>/` | One tool each: `kitty`, `hc`, `gnome`, `proc`, `claude`, `opencode`, `execprobe`, `ssh`. |
| `internal/panelstart/` | Why a panel's command did not start. |
| `internal/config/` | TOML loading, its validation, and the template `revier new` writes. |
| `internal/state/` | What revier learned at runtime: the last project, attached instances. |
| `internal/ledger/` | The state file, event file, state record and press lock, for the core. |
| `internal/session/` | The saved sets of open projects, and `seen.toml`. |
| `internal/checkout/` | git and mise on a project's directory: its origin, its clone. |
| `internal/userunit/` | Long-lived programs as user services: the workspace kitty, the hc core. |
| `internal/procgroup/` | The process group of each host command, and its end. |
| `internal/plain/` | Text with its escape sequences and control characters taken off. |
| `internal/shell/` | Quoting for a POSIX shell and for fish. |
| `internal/fsutil/` | Atomic and through-link file writes, and `RealPath`. |
| `internal/logging/` | The daily log file, operation timing, and the logs screen's reader. |
| `internal/events/` | The event file `revier events` prints. |
| `internal/record/` | The state record `revier state` replays. |
| `internal/runlog/` | The record of every `revier each`. |
| `internal/sshconfig/` | The hosts `~/.ssh/config` names, and this machine's `HostName`. |
| `internal/hosttest/` | The fakes of every port that layers L2 and L4 run against. |
| `internal/testenv/` | The scratch directories and PATH a test binary runs in. |
| `internal/popupswitch/` | The popup's socket, and the second press that switches its list. |
| `internal/curtain/` | The socket between a shutdown or a restore and its curtains. |
| `internal/tui/` | The TUI surface. |
| `internal/theme/` | Every colour and glyph of the TUI, named by role. |
| `internal/build/` | The version, commit and date stamped in at build. |
| `cmd/revier/` | The CLI entry point, and the wiring of every adapter. |
| `scripts/drive/` | Manual headless driver, on an hc core of its own. |
| `scripts/lease-desktop` | A werft lease made into a desktop revier can drive. |
| `scripts/release-notes` | One version's `CHANGELOG.md` section, the text of its release. |
| `scripts/demo/` | The recording of the README's clips. |
| `docs/decisions/` | One record per decision in force. |
| `docs/user-guide/` | The [user guide](user-guide/README.md); the binary embeds its reference pages. |

Each package comment says more: `go doc ./internal/<package>`.

## The ports

Which interfaces of `pkg/revier` are ports, and what each is:
[docs/design/architecture.md](design/architecture.md#ports).

## Finding things

```bash
rg 'func \(c \*Core\)' internal/core      # every decision the core makes
rg -n '^type \w+ interface' pkg/revier   # the ports and their optional capabilities, by file
rg 'revier\.(Runtime|View|Host)\b' --type go   # what implements or consumes a terminal or window port
rg -l '//go:build live'                   # tests needing a real substrate
rg 'ErrNo' pkg internal                   # the normal-outcome sentinels
rg 'func cmd' cmd/revier                  # every CLI command
rg 'decisions/0085'                       # replace 0085 with the cited number: every citation of that record
```

Behaviour is almost never in the adapter named after the tool. Search
`internal/core` first.

## Outside this repository

- `wctl` — <https://github.com/carlo9890/gnome-window-control>, the GNOME window
  host's dependency.
