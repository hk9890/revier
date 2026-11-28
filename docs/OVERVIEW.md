# Overview

A findability map: where things live, and how to search for them.

## Layout

| Path | Holds |
|---|---|
| `pkg/revier/` | The ports and every type that crosses them, the project model included. Public, because out-of-tree adapters import it. |
| `internal/core/` | All policy: resolution, matching, run-or-raise, toggle-back, survey. |
| `internal/app/` | The use cases that cross stores, written once for both surfaces: create, link and rename a project, run an action, save a session. |
| `internal/adapter/<tool>/` | One tool each. No policy. `kitty` (runtime, view, terminal keys), `hc` (runtime), `gnome` (window, key binder, window rules), `proc` (a link's processes here, a process's parent, and the agent session variables a server must not hand on), `claude` (probe, assistant launcher), `opencode` and `execprobe` (probes), `ssh` (remote). |
| `internal/hosttest/` | The fake host, view, probe, key binder, terminal keys, window rules and remote that layers L2 and L4 run against. |
| `scripts/drive/` | Manual headless driver, on an hc core of its own. |
| `scripts/release-notes` | One version's `CHANGELOG.md` section, the text of its GitHub release. |
| `scripts/demo/` | The README's clips, `.github/assets/*.webp`: the recording scripts run here, `guest/` on a VM's GNOME desktop. |
| `docs/user-guide/` | The [user guide](user-guide/README.md). The package carries its reference pages (`configuration.md`, `extending/`) into the binary, which `revier assist --reference` prints. |
| `cmd/revier/` | CLI entry point. Constructs every port's adapter, also calls the `claude` adapter directly, and reads `proc`'s variable names. |

Every other package says what it owns in the first sentence of its package
doc comment; [Finding things](#finding-things) prints them all.

What each port is: [docs/design/architecture.md](design/architecture.md#ports).

## Finding things

```bash
go list -f '{{.ImportPath}}: {{.Doc}}' ./...   # what each package owns
go doc ./internal/<pkg>                   # one package's doc in full
rg 'type \w+ interface' pkg/revier        # every port, and the optional capabilities a host may add
rg 'func \(c \*Core\)' internal/core      # every decision the core makes
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
