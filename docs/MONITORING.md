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
- A failure that recurs every refresh — a probe, a survey, a remote host, the TUI's state file — is one `WARN` until its message changes, then one `INFO` `<msg>: recovered` (`logging.Repeat`). Silence after a `WARN` means still failing, per process.

## Operations

| `msg` | Written when |
|---|---|
| `command` | a CLI process ends, with `args` and `exit` |
| `resolve` | a command picks its project, `by` flag, directory, focused window or last project |
| `go` | run-or-raise ends: `launched`, `landed`, `ref`, and `agents` for a resume. A toggle back writes a second line for the Go home, a tab target a second line for its workspace. |
| `bind` | the wait for a launched window ends |
| `claim` | the TUI binds or attaches a window that appeared |
| `instances` | a `host` could not list; its targets show unknown and a press on one is refused, while the other hosts' go on (`docs/design/decisions.md` D89) |
| `survey`, `remote survey` | a survey failed, or took 500 ms or more (at most once a minute per host); a fast one writes nothing |
| `probe` | an agent probe failed, and the agent shows unknown |
| `panel mark` | one `WARN` per launch whose first panel could not be marked; the instance opened, and a return home from a tab falls back to its guess (`docs/design/decisions.md` D100) |
| `session saved` | a save, with its counts and gaps |
| `session restore`, `restore step`, `restore agent`, `session restored` | a restore: the session, each target's action, each recorded agent's `session`, `dir` and `outcome` |
| `shutdown step`, `shutdown`, `shutdown refused` | a shutdown: each step's `ref`, `panel`, `busy`, `unread` and `err`, then the counts; its save writes `session saved` with `by` shutdown. A close the busy guard refused writes `shutdown refused` alone, with the plan's `steps` and `busy` counts, and closed and saved nothing (`docs/design/decisions.md` D99) |
| `start directory` | one `WARN` when the home directory a process moves to cannot be reached, and it runs in `/` instead. Written by the surface, which always leaves, and by a command whose working directory was removed under the shell (`docs/design/decisions.md` D92) |
| `config problem` | one `WARN` per refusal at load, with `project` and `file`; not written by `list`, which runs every refresh on a linked host |
| `state load`, `state update` | the TUI could not read the state file, or a process could not update it |
| `action`, `clone`, `each project`, `agent new`, `shell new`, `go agent`, `agent focus`, `attach`, `focus attached`, `keys install`, `keys uninstall` | the operation named |
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
