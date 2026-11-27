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
- The TUI's logs screen (alt+l) lists the `WARN` and `ERROR` lines of the days
  kept, newest first (`internal/logging.Read`). It writes nothing to the log,
  and counts the lines it cannot decode on its rule.
- A footer error opens that screen on the newest `WARN` or `ERROR` line of the
  TUI's `pid` at or before the time the footer took the error. That is the
  error's line only when the error has one:
  - A survey error has no time: the cursor starts on the newest line.
  - An outcome logged at `INFO`, such as `session save: nothing open` or a
    cancelled restore or shutdown, has no line: the cursor starts on an older
    one.

## Fields

| Field | Holds |
|---|---|
| `time`, `level`, `msg` | slog's own. `msg` is the operation. |
| `pid`, `cmd` | the process, and its first argument (`tui` for none) |
| `duration_ms` | on every timed operation |
| `err` | on most `ERROR` and `WARN` lines; the call-site comment names the fields of each |
| `project`, `target`, `ref` | on every operation that acts on one |

## Levels

- `INFO` — an operation that succeeded.
- `ERROR` — an operation that failed. An error the TUI showed in its footer that no operation logged has `msg` `tui`.
- `WARN` — an error revier carried on past: a state file it could not read or save, a probe that reported unknown, a window it could not place or bind.
- A failure that recurs every refresh — a probe, a host's listing (`instances`), a remote host, the TUI's state file — is one `WARN` per different `err`, then one `INFO` `<msg>: recovered` (`logging.Repeat`). Its `was` is the `err` of the last failed refresh, `last_failed` when that refresh failed, and `failures` how many refreshes failed.
- Silence after a `WARN` means no refresh of that process succeeded since: it still failed, with an `err` already written, or it did not refresh. A hidden popup does not refresh, so its `recovered` comes at its next raise. Read the end of an outage from `last_failed`, not from the time of the line.

## Operations

A line's `msg` names the operation. The comment at the call site that writes
it says when it is written and which fields it carries. Find it:

```bash
rg -n '"<msg>"' internal cmd pkg -g '!*_test.go'
```

A `msg` that the code builds, such as `setup uninstall: term`, has no literal:
search its comment for the same quoted text, without the closing quote.

## The event file

- `$STATE/events.jsonl` holds one JSON line per operation that succeeded in a
  project, and is never pruned (decisions/0112).
- Read it with `revier events --days <n>`; `revier events --help` lists the
  fields. `--local` leaves out the linked hosts.
- A press is in the log and in the event file. Explain a failure from the
  log: a failed operation writes no event. Its error is in the state record.
- `agent session` lines come from the TUI alone, so none are written while no
  surface runs, or while the popup is hidden and no launch of its own is
  coming up.

## The state record

- `$STATE/record/YYYY-MM-DD.jsonl` holds one JSON line per thing revier did,
  `by` `did`, with `err` when it failed, and per change a surface's survey
  saw, `by` `saw`. It is never pruned (decisions/0151).
- `kind` names what a line is: the `ran`, `snapshot` and `survey` lines below
  are values of `kind`.
- Read it with `revier state --at <time>`: what was open then, per machine.
  `revier state --session <id>` names why each difference between a save and
  what was open when it was made is one; `revier state --help` lists the
  reasons.
- A `ran` line is a run of a command target that moved the focus, with the
  `host` and `ref` the focus moved to. It is written only when that differs
  from the run on record, and it opens nothing: `revier state` lists it as
  done and holds no target open for it.
- A `saw` line comes from a surface alone: with no TUI or popup running, the
  record holds only what operations did. `revier list` writes no `saw`
  line, only what its prune changed in the state file.
- A hidden popup surveys nothing unless a launch of its own is still coming
  up, so it writes no `saw` line until its
  next raise. A change made while it was hidden carries the time of that
  raise, not of the change.
- A popup that wctl's overlay hid logs `popup: hidden by the desktop` when
  the next press raises it, or when a slow refresh's survey lists it
  minimized; until then it surveys every ten seconds and writes `saw` lines.
  A `popup: raise` line with `minimized=true` follows Esc's hide too. A popup
  that shows a screen, asks a question, such as a delete, or walks a restore
  or a shutdown logs no such line.
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
