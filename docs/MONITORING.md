# Monitoring

Driving revier to make something happen is [RUNNING.md](RUNNING.md)'s. This
doc reads what it already did.

## The log

- One JSON line per record, in `$STATE/logs/revier-YYYY-MM-DD.log`.
- `$STATE` is `$REVIER_STATE_HOME`, else `$XDG_STATE_HOME/revier`, else
  `~/.local/state/revier` (`internal/state.Root`).
- Files older than 14 days are removed when a day's first file is created
  (`internal/logging.Keep`).
- Every process appends to the same file: keybindings, the TUI, a restore. Split
  them by `pid`.
- A remote project's operations run in the revier on its host, and log there.
- `revier list` logs only `WARN` and `ERROR`: a surface with a linked project
  runs it on that host every refresh.

## Fields

| Field | Holds |
|---|---|
| `time`, `level`, `msg` | slog's own. `msg` is the operation. |
| `pid`, `cmd` | the process, and its first argument (`tui` for none) |
| `duration_ms` | on every timed operation |
| `err` | on every `ERROR` and `WARN` |
| `project`, `target`, `ref` | on every operation that acts on one |

## Levels

- `INFO` — an operation that succeeded.
- `ERROR` — an operation that failed. An error the TUI showed in its footer that no operation logged has `msg` `tui`.
- `WARN` — an error revier carried on past: a state file it could not read or save, a probe that reported unknown, a window it could not place or bind.
- A failure that recurs every refresh — a probe, a survey, a remote host, the TUI's state file — is one `WARN` per different `err`, then one `INFO` `<msg>: recovered` whose `was` is the `err` of the last failed refresh (`logging.Repeat`). Silence after a `WARN` means still failing, per process, with an `err` already written.

## Operations

| `msg` | Written when |
|---|---|
| `command` | a CLI process ends, with `args` and `exit`. A surface that a signal ended, as a closed terminal does, has `signal` (`hangup` or `terminated`) and `exit` 128 plus its number. A surface with no `command` line was killed, or is still running. |
| `resolve` | a command picks its project, `by` flag, directory, focused window or last project |
| `go` | run-or-raise ends: `launched`, `landed`, `ref`, and `agents` for a resume. A toggle back writes a second line for the Go home, a tab target a second line for its workspace. |
| `bind` | the wait for a launched window ends |
| `claim` | the TUI binds or attaches a window that appeared |
| `instances` | a `host` could not list; its targets show unknown and a press on one is refused, while the other hosts' go on (`docs/design/decisions.md` D89) |
| `remote events` | `revier events` asked a `host` for its events, with the `events` count |
| `event` | one `WARN` when an event could not be appended; the operation went on |
| `event line skipped` | one `WARN` per line of the event file that is not an event, with `line` |
| `record` | one `WARN` per different `err` when a line could not be appended to the state record, with its `kind`; the operation went on |
| `record line skipped` | one `WARN` per line of the state record that is not a line, with `file` and `line` |
| `survey`, `remote survey` | a survey failed, or took 500 ms or more (at most once a minute per host); a fast one writes nothing |
| `probe` | an agent probe failed, and the agent shows unknown |
| `mirror` | one `WARN` per panel the mirror could not read, with `panel`; the pane keeps the screen it last read. A panel that closed before the next survey writes no `recovered` |
| `panel mark` | one `WARN` per panel of a launch that could not be marked; the instance opened, and a return home from a tab falls back to its guess (`docs/design/decisions.md` D100) |
| `session saved` | a save, with its counts and gaps; `by` is why: `saved` by hand, `shutdown`, `project-closed`, or `last-seen` for what a surface last saw, saved after an end with no shutdown. A plain restore never opens a `project-closed` save (`docs/design/decisions.md` D149) |
| `session seen` | one `WARN` when a surface could not write what it last saw, `$STATE/seen.toml`, with `path` (`docs/design/decisions.md` D150) |
| `unclean end` | a surface's first survey, or a plain `revier session restore`, found an end with no shutdown that lost a target: `seen_at` when the snapshot last changed, `missing` the targets not open now, and `session` the save it offers. A `WARN` with `err` alone when the check failed |
| `session restore`, `restore step`, `restore agent`, `session restored` | a restore: the session, each target's action, each recorded agent's `session`, `dir` and `outcome` |
| `session restore cancelled`, `shutdown cancelled` | ctrl+c in the TUI, or on `revier session restore` or `revier shutdown`, stopped a restore or a shutdown between two steps, with `walked` of `steps`. Both still write their closing line, `session restored` or `shutdown`, with its counts (`docs/design/decisions.md` D140) |
| `shutdown survey`, `shutdown recheck`, `shutdown save` | the phases of a close before its first step, each with `duration_ms`: the plan's survey, the reading of its agents again, the session save. A save made before a cancel logs no `err`: the cancel is `shutdown cancelled` with `walked` 0. The two surveys carry `asked`, the linked hosts waited for, and `unanswered`, why a host gave no answer for a link (`docs/design/decisions.md` D115) |
| `shutdown step`, `shutdown`, `shutdown refused` | a shutdown: each step's `ref`, `panel`, `busy`, `unread` and `err`, then the counts and the `duration_ms` from the recheck to the last wait; its save writes `session saved` with `by` `shutdown`, or `project-closed` for one project. A close the busy guard refused writes `shutdown refused` alone, with the plan's `steps` and `busy` counts, and closed and saved nothing (`docs/design/decisions.md` D99) |
| `shutdown state set`, `shutdown state cleared` | a full shutdown was recorded after its save, with `at` and the `session` it saved; or the record ended, with `set_at`: something opened, a cancel or a second ctrl+c left a step of the shutdown that wrote it not reached, a restore started, or the start dialog started fresh (`docs/design/decisions.md` D141) |
| `surface raise` | a restore in the TUI ended, and the TUI brought the terminal it runs in back to the front, with its `ref` and its `panel`, or `popup` when the TUI is the popup; with `err` alone when the runtime listing failed. A TUI in no terminal the runtime lists writes nothing |
| `start dialog: restore`, `start dialog: pick another session`, `start dialog: start fresh` | the choice made on the TUI's first screen after a full shutdown, or with `unclean` true after an end with no shutdown |
| `start directory` | one `WARN` when the home directory a process moves to cannot be reached, and it runs in `/` instead. Written by the surface, which always leaves, and by a command whose working directory was removed under the shell (`docs/design/decisions.md` D92) |
| `config problem` | one `WARN` per refusal at load, with `project` and `file`; not written by `list`, which runs every refresh on a linked host |
| `state load`, `state update` | a process could not read the state file, or could not update it; it went on with the state it read last, and with nothing remembered when it had read none |
| `action`, `clone`, `each project`, `agent new`, `shell new`, `go agent`, `agent focus`, `attach`, `focus attached`, `keys install`, `keys uninstall` | the operation named |
| `config written`, `project created`, `project deleted`, `runtime switched` | the file or setting changed |

## The event file

- `$STATE/events.jsonl` holds one JSON line per operation that succeeded in a
  project, and is never pruned (`docs/design/decisions.md` D112).
- Read it with `revier events --days <n>`; `revier events --help` lists the
  fields. `--local` leaves out the linked hosts.
- A press is in the log and in the event file. Explain a failure from the
  log: a failed operation writes no event. Its error is in the state record.
- `agent session` lines come from the TUI alone, so none are written while no
  surface runs or the popup is hidden.

## The state record

- `$STATE/record/YYYY-MM-DD.jsonl` holds one JSON line per thing revier did,
  `by` `did`, with `err` when it failed, and per change a surface's survey
  saw, `by` `saw`. It is never pruned (`docs/design/decisions.md` D151).
- Read it with `revier state --at <time>`: what was open then, per machine.
  `revier state --session <id>` names why each difference between a save and
  what was open when it was made is one; `revier state --help` lists the
  reasons.
- A `saw` line comes from a surface alone: with no TUI or popup running, the
  record holds only what operations did. `revier list` writes no `saw`
  line, only what its prune changed in the state file.
- A hidden popup surveys nothing unless a launch of its own is still coming
  up (`docs/design/decisions.md` D86), so it writes no `saw` line until its
  next raise. A change made while it was hidden carries the time of that
  raise, not of the change.
- A `snapshot` is a whole survey of one scope, this machine or one linked
  host (`machine`): at a surface's first survey, at the first of a day, when
  the project list changed, and at each save, with `saved`. Each other `saw`
  line is a change since the last survey. A scope with no change for five
  minutes writes a `survey` line; one with no line for six is stale.
- A host that did not list, or a link whose host did not answer, keeps what
  was last seen of it, marked unknown.
- `pid` and `cmd` are the log's: join a line with the log of the process
  that wrote it.

## Queries

```bash
L=${REVIER_STATE_HOME:-${XDG_STATE_HOME:-$HOME/.local/state}/revier}/logs/revier-$(date +%F).log
jq -c 'select(.level != "INFO")' "$L"                                  # everything that went wrong
jq -c 'select(.msg | startswith("restore")) | {time, msg, project, target, session, dir, outcome}' "$L"
jq -c 'select(.msg == "go") | {time, project, target, launched, duration_ms}' "$L"
jq -s 'map(select(.duration_ms)) | sort_by(-.duration_ms) | .[:10]' "$L"   # the slowest operations
jq -c --argjson p 12345 'select(.pid == $p)' "$L"                      # one process
R=${L%/logs/*}/record/$(date +%F).jsonl
jq -c 'select(.err) | {time, kind, project, target, err}' "$R"         # every operation that failed today
```
