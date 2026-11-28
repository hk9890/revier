# Monitoring

Driving revier to make something happen is [RUNNING.md](RUNNING.md)'s. This
doc reads what it already did.

## The log

- One JSON line per record, in `$STATE/logs/revier-YYYY-MM-DD.log`.
- `$STATE` is `$REVIER_STATE_HOME`, else `$XDG_STATE_HOME/revier`, else
  `~/.local/state/revier` (`internal/state.Root`).
- Only the newest `internal/logging.Keep` day files, today's included, stay:
  the older ones are removed when a process creates the day's file. A
  `log prune` `WARN` means some were not.
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
- A failure that recurs every refresh — a probe, a host's listing (`instances`), a remote host, the TUI's state file — is one `WARN` per different `err`, then one `INFO` `<msg>: recovered`. `go doc ./internal/logging Repeat` names the fields of both.
- Silence after a `WARN` means no refresh of that process succeeded since: it still failed, with an `err` already written, or it did not refresh. Read the end of an outage from `last_failed`, not from the time of the line.

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

## The state record

- `$STATE/record/YYYY-MM-DD.jsonl` holds one JSON line per thing revier did,
  `by` `did`, with `err` when it failed, and what a survey saw, `by` `saw`. It
  is never pruned (decisions/0151).
- `kind` names what a line is.
  `go doc ./internal/record KindSnapshot` prints every kind a survey writes,
  each with when it is written. `go doc ./internal/record KindGo` prints every
  kind an operation writes.
- Read it with `revier state --at <time>`: what was open then, per machine.
  `revier state --session <id>` names why each difference between a save and
  what was open when it was made is one; `revier state --help` lists the
  reasons.
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
