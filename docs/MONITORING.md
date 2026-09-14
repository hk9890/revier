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
- `ERROR` — an operation that failed, or an error the TUI showed in its footer (`msg` is `tui`).
- `WARN` — an error revier carried on past: a state file it could not save, a probe that reported unknown, a window it could not place or bind.
- A failure that recurs every refresh — a probe, a survey, a remote host — is one `WARN` until its message changes, then one `INFO` `<msg>: recovered` (`logging.Repeat`). Silence after a `WARN` means still failing, per process.

## Operations

| `msg` | Written when |
|---|---|
| `command` | a CLI process ends, with `args` and `exit` |
| `resolve` | a command picks its project, `by` flag, directory, focused window or last project |
| `go` | run-or-raise ends: `launched`, `landed`, `ref`, and `agents` for a resume. A toggle back writes two. |
| `bind` | the wait for a launched window ends |
| `claim` | the TUI binds or attaches a window that appeared, `by` poll or event |
| `survey`, `remote survey` | a survey failed, or took 500 ms or more (at most once a minute per host); a fast one writes nothing |
| `probe` | an agent probe failed, and the agent shows unknown |
| `session saved` | a save, with its counts and gaps |
| `session restore`, `restore step`, `restore agent`, `session restored` | a restore: the session, each target's action, each recorded agent's `session`, `dir` and `outcome` |
| `action`, `clone`, `each project`, `agent new`, `keys install` | the operation named |
| `config written`, `project created`, `project deleted`, `runtime switched` | the file or setting changed |

## Queries

```bash
L=~/.local/state/revier/logs/revier-$(date +%F).log
jq -c 'select(.level != "INFO")' "$L"                                  # everything that went wrong
jq -c 'select(.msg | startswith("restore")) | {time, msg, project, target, session, dir, outcome}' "$L"
jq -c 'select(.msg == "go") | {time, project, target, launched, duration_ms}' "$L"
jq -s 'map(select(.duration_ms)) | sort_by(-.duration_ms) | .[:10]' "$L"   # the slowest operations
jq -c --argjson p 12345 'select(.pid == $p)' "$L"                      # one process
```
