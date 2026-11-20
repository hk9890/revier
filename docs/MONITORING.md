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
  TUI's `pid` at or before the time the footer took the error
  (`docs/design/decisions.md` D157). That is the error's line only when the
  error has one:
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
| `err` | on every `ERROR` and `WARN` |
| `project`, `target`, `ref` | on every operation that acts on one |

## Levels

- `INFO` — an operation that succeeded.
- `ERROR` — an operation that failed. An error the TUI showed in its footer that no operation logged has `msg` `tui`.
- `WARN` — an error revier carried on past: a state file it could not read or save, a probe that reported unknown, a window it could not place or bind.
- A failure that recurs every refresh — a probe, a survey, a remote host, the TUI's state file — is one `WARN` per different `err`, then one `INFO` `<msg>: recovered` (`logging.Repeat`). Its `was` is the `err` of the last failed refresh, `last_failed` when that refresh failed, and `failures` how many refreshes failed.
- Silence after a `WARN` means no refresh of that process succeeded since: it still failed, with an `err` already written, or it did not refresh. A hidden popup does not refresh (`docs/design/decisions.md` D86), so its `recovered` comes at its next raise. Read the end of an outage from `last_failed`, not from the time of the line.

## Operations

| `msg` | Written when |
|---|---|
| `command` | a CLI process ends, with `args` and `exit`. A surface that a signal ended, as a closed terminal does, has `signal` (`hangup` or `terminated`) and `exit` 128 plus its number. A surface with no `command` line was killed, or is still running. `revier assist` writes `assist` in its place. |
| `assist` | `revier assist` replaces itself with the agent, with `exec` and `dir`. The agent's process writes no `command` line |
| `resolve` | a command picks its project, `by` flag, directory, focused window or last project. A `WARN` `resolve: working directory` with `err` is a command with no `-p` that could not read its directory: the directory step is skipped. A `WARN` `resolve: focused window` with `err` is a focused window that could not be read, or whose host could not list: the last project is tried next |
| `go` | run-or-raise ends: `launched`, `landed`, `ref`, and `agents` for a resume. A toggle back writes a second line for the Go home, a tab target a second line for its workspace. A command target's line has `ran` and `focused`, the window the run left the focus on, empty when the focus did not move; its toggle back has `ran` false and `landed` the home, after the line of the Go home (`docs/design/decisions.md` D166). A `WARN` `toggle-back: focused, taken as not focused` with `host` and `err` is a focus that could not be read: the press raises, and does not go home. A command target's `WARN` `go: focused` with `host` and `err` is the same: before the run, the press does not go home, it runs the command, and its `focused` is empty; after the run, one line comes per failed read, every 50 ms for up to 500 ms. A tab target whose panel ended before its focus writes `tab gone at its focus, pressed again` with `project`, `target` and the focus `err`: the press is made once more on a new listing, which does not go home, and a second such end fails the press with its own focus error. |
| `view opened again over a live instance`, `place`, `place: the window host never listed the new window`, `raise: the window host never listed the new window`, `place: listing the hosts`, `raise new`, `show the new tab` | the steps that put an OS window of a runtime instance in front, under a view that shows its own windows. `view opened again over a live instance`, with `view`, `ref` and `target`, is a press, an agent focus, a focus of a ref or a restore that found an open instance no window of the view shows, and the view opened one. For a press or a restore, the target's place puts the window (`docs/design/decisions.md` D186); an agent focus places nothing. The window host must then list that window, or the one a launch opened, within 2 s. A `WARN` `place`, with `ref`, `place` and `err`, is a placement the window host refused. A `WARN` `place: the window host never listed the new window`, with `ref` and `place`, is a launch's window not placed and not raised. A `WARN` `raise: the window host never listed the new window`, with `ref` and `target`, is a window not raised and not placed. A `WARN` `place: listing the hosts` with `err` comes before either when the last listing of that wait failed. A `WARN` `raise new` with `ref` and `err` is a failed raise of a launch's window by the window host (`docs/design/decisions.md` D177); the press still succeeded. A `WARN` `show the new tab`, with `view`, `ref` and `err`, is a tab opened in an open instance that the view could not draw: the tab runs, and the next press that shows the instance draws it |
| `go: launch still coming up, not launched again`, `go: is the pending launch up` | a press or a restore step found a launch on record not up yet, and launched nothing; a plain press then writes no `go` line. `target` is the pressed target, or the home: for the second press of a command target, whose `go` line follows with `ran` false and no line of the Go home before it, and for an agent press in a linked project. An `ERROR` `go: is the pending launch up` with `err` is a check that failed, and the press failed |
| `tab outside a workspace` | a terminal key asked for a tab beside a panel or window that no open workspace holds, in place of `shell new` or `agent new`, before anything opens: with `panel`, `ref` (empty for a window of the view), `outside`, `kind`, `agent` (empty for the default agent and for a shell key) and `dir`. With `outside` `none` nothing opens. A `WARN` with `err` follows when `outside` is `agent`, the key names no agent, and `config.toml` declares no `[[agent]]`: the tab is a plain shell. A key that names an agent `config.toml` does not declare is refused, and the key's own fallback runs |
| `bind` | the wait for a launched window ends. A `WARN` `bind` with `err` comes first when no window appeared in the `wait`, or when more than one new window of the class did (`candidates`): the window is left for a `claim`. A new window that a binding or attachment holds is no candidate. A new window of a class that another target's pending launch awaits is no candidate, and is bound only when this target's full rule matches it. The same holds for a class that a kept late launch of another window target awaits (`docs/design/decisions.md` D190) |
| `claim` | the TUI binds or attaches a window that appeared |
| `claim: launch expired with no window claimed` | the launch on record passed its window, with `launched_at`, and no window was claimed for it: an action's launch, or a target's past its 10 minutes as well. A target's launch inside its 10 minutes is set aside instead |
| `claim: launch replaced` | a launch replaced an action's launch still pending: `project`, `target` (empty) and `launched_at` are the replaced one's, `by_project` and `by_target` the new one's, `age_ms` how old the replaced one was. No window is attached for the replaced one |
| `claim: launch set aside` | a target's launch left the record with no window and became late, kept until 10 minutes from `launched_at`: with `by_project` and `by_target` another launch replaced it while it was pending, else it passed its 60 s window. A launch past its 60 s window that another replaces before a survey saw it is set aside with no line. `age_ms` is its age then (`docs/design/decisions.md` D190) |
| `claim: late launch bound` | a settle bound a late launch's `target` of `project` to `ref`, the one window that its full rule matches, no binding or attachment holds, and no other target's rule matches. `age_ms` is the time since the launch. Any process that settles can bind one: a `revier list` that does writes no line, as it logs only `WARN` and `ERROR` |
| `claim: late launch expired with no window bound` | a late launch reached 10 minutes from `launched_at` with no window bound: the window never came, or its title never matched the rule of its target alone |
| `binding dropped` | a survey found the window or terminal of a binding gone, with its `project`, `target` (empty for an attachment) and `ref` |
| `instances` | a `host` could not list; its targets show unknown and a press on one is refused, while the other hosts' go on (`docs/design/decisions.md` D89) |
| `remote events` | `revier events` asked a `host` for its events, with the `events` count |
| `event` | one `WARN` when an event could not be appended; the operation went on |
| `event line skipped` | one `WARN` per line of the event file that is not an event, with `line` |
| `record` | one `WARN` per different `err` when a line could not be appended to the state record, with its `kind`; the operation went on |
| `record line skipped` | one `WARN` per line of the state record that is not a line, with `file` and `line` |
| `status` | a status command failed, or took 500 ms or more (`slow` true, at most once a minute per place and project); a fast one writes nothing. With `at`, the place (`rule-left`, `rule-right`, `project-line`, `project`), and `project`, empty for a place on the rule. A failure is one `WARN` per place, project and different `err`, such as `timed out after 5s`, then `status: recovered`; the place keeps its last good answer, dimmed. Written by the TUI and the popup; a hidden popup runs none |
| `survey`, `remote survey` | a survey failed, or took 500 ms or more (at most once a minute per host); a fast one writes nothing. A `survey` that waited for linked hosts has `asked`, the hosts, and `unanswered`, why one gave no answer; a TUI refresh's has neither |
| `probe` | an agent probe failed, and the agent shows unknown |
| `detail` | one `WARN` per panel and different `err` when a probe failed to read an agent's conversation, with `probe`, `ref` and `panel`; the pane and the agent list keep what that agent said last, and its status is unchanged. An agent that has said nothing yet, whose harness lists no conversation for it, or whose transcript's tail holds no turn writes no line; a tail none of whose lines parses, as after a Claude Code release that changed the transcript's format, writes one. A read whose error is the walk's deadline stops that probe for the walk, and writes one `WARN` for the probe in place of one per panel: `probe`, `cut` the error of the read it ended, and `read`, `failed` and `unread` the probe's agents read, whose read failed, and not read, which keep what they said last; the other probes' agents are still read. A read that fails for another cause after the deadline writes its own per-panel line. Its `recovered` comes at the next walk that reads that probe in time. An agent no longer read, because its project left the screen or its panel closed, or whose next read has nothing to read, writes no `recovered` |
| `mirror` | one `WARN` per panel the mirror could not read, with `panel`; the pane keeps the screen it last read. A panel that closed before the next survey writes no `recovered`. A read the runtime says crossed the panel's close writes `mirror: panel gone` at `INFO` instead, once per panel, with `panel` and `err` (`docs/design/decisions.md` D172) |
| `panel mark` | one `WARN` per panel of a launch that could not be marked; the instance opened, and a return home from a tab falls back to its guess (`docs/design/decisions.md` D100) |
| `session saved` | a save, with its counts and gaps; `by` is why: `saved` by hand, `shutdown`, `project-closed`, or `last-seen` for what a surface last saw, saved after an end with no shutdown. A plain restore never opens a `project-closed` save (`docs/design/decisions.md` D149). One `WARN` `session save: probe could not be asked` with `err` follows it per probe that could not answer at all, such as claude not on the PATH, and per linked host that gave no conversations: those agents are saved with no conversation. A `last-seen` save writes none. An `ERROR` `session save` with `err` is a save from the TUI's sessions screen whose survey or write failed |
| `session seen` | one `WARN` when a surface could not write what it last saw, `$STATE/seen.toml`, with `path` (`docs/design/decisions.md` D150) |
| `unclean end` | a surface's first survey, or a plain `revier session restore`, found an end with no shutdown that lost a target: `seen_at` when the snapshot last changed, `missing` the targets not open now, and `session` the save it offers. A `WARN` with `err` alone when the check failed |
| `session restore`, `restore step`, `restore agent`, `session restored` | a restore: the session, each target's action, each recorded agent's `session`, `dir` and `outcome`. A `WARN` `restore step` with `err` `planned project not loaded` is a step whose project no file loads now. A `WARN` `restore: show a running target` with `err` is a step that found its target running, under a view that opens windows of its own, and could not list the runtime, open the view's window over it again, or make the window's own tab current; for a tab, `target` is the target the tab is inside. The target runs, and the next press shows it (`docs/design/decisions.md` D181) |
| `session restore cancelled`, `shutdown cancelled` | ctrl+c in the TUI, or on `revier session restore` or `revier shutdown`, stopped a restore or a shutdown between two steps, with `walked` of `steps`; `walked` equal to `steps` is a shutdown whose wait after its last step the cancel ended. Both still write their closing line, `session restored` or `shutdown`, with its counts (`docs/design/decisions.md` D140) |
| `shutdown survey`, `shutdown recheck`, `shutdown save` | the phases of a close before its first step, each with `duration_ms`: the plan's survey, the reading of its agents again, the session save. A save made before a cancel logs no `err`: the cancel is `shutdown cancelled` with `walked` 0. The two surveys carry `asked`, the linked hosts waited for, and `unanswered`, why a host gave no answer for a link (`docs/design/decisions.md` D115) |
| `shutdown step`, `shutdown`, `shutdown refused` | a shutdown: each step's `ref`, `panel`, `busy`, `unread` and `err`, then the counts and the `duration_ms` from the recheck to the last wait; its save writes `session saved` with `by` `shutdown`, or `project-closed` for one project. A close the busy guard refused writes `shutdown refused`, with the plan's `steps` and `busy` counts, and closed and saved nothing (`docs/design/decisions.md` D99); the confirm it asks again ends as any other. A close left at its confirm unrun, by Esc, No, a raise of the popup or ctrl+c, writes `shutdown declined`: its `project`, empty for a full shutdown, the `target` a del picked, `steps`, and `planned` false when its survey had not answered, whose line then comes after it |
| `agent close` | a close of one agent, with its `address` and `force`, and `err` for a refusal. Before it, `shutdown survey`; a close past the busy guard adds `shutdown recheck`, then `shutdown refused`, or `shutdown step` and `shutdown`. It writes no `shutdown save` or `session saved` |
| `shutdown state set`, `shutdown state cleared` | a full shutdown was recorded after its save, with `at` and the `session` it saved; or the record ended, with `set_at`: something opened, a cancel or a second ctrl+c left a step of the shutdown that wrote it not reached, a restore started, or the start dialog started fresh (`docs/design/decisions.md` D141) |
| `surface raise` | a restore in the TUI ended, and the TUI brought the terminal it runs in back to the front, with its `ref` and its `panel`, or `popup` when the TUI is the popup; with `err` alone when the runtime listing failed. A TUI in no terminal the runtime lists writes nothing |
| `start dialog: restore`, `start dialog: pick another session`, `start dialog: start fresh` | the choice made on the TUI's first screen after a full shutdown, or with `unclean` true after an end with no shutdown |
| `start directory` | one `WARN` when the home directory a process moves to cannot be reached, and it runs in `/` instead. Written by the surface, which always leaves, and by a command whose working directory was removed under the shell (`docs/design/decisions.md` D92) |
| `built-in assistant left out` | one `WARN` per command when the built-in assistant could not be made, such as its directory under the state root: the command goes on without it, and alt+a hands the terminal to `revier assist`, which says why. Written by `list` too, every refresh on a linked host |
| `the service manager did not start the unit; starting it detached` | one `WARN` when `systemd-run` could not start a unit, with `unit` and `err`, and the unit is not running or starting: revier starts the program apart from the service manager. The units are the workspace kitty and the hc core |
| `log prune` | one `WARN` when a process created the day's first file and could not list the log directory, or could not remove a file older than 14 days, with `dir`; the files it did not remove stay |
| `config problem` | one `WARN` per refusal at load, with `project` and `file`; not written by `list`, which runs every refresh on a linked host |
| `state load`, `state update` | a process could not read the state file, or could not update it; it went on with the state it read last, and with nothing remembered when it had read none. A `state load` whose `err` says the file does not parse discarded it: the process went on with an empty state, the bindings and a shutdown on record lost, and its next write replaced the file |
| `close: nothing open`, `close: held open` | an `INFO` line when del in the TUI found nothing of the project open here, or what is open of it held open by another project, `by`. The footer says so, and no `ERROR` line is written |
| `close: plan dropped` | the plan of a del, for its `project` and `target`, came after a screen opened, a close started or the popup was raised, and nothing was asked |
| `popup: launched`, `popup: raise`, `popup: switch`, `popup: hide` | the popup key launched the popup, with `over`, the window it was pressed over, `workarea_width`, `place`, and `by_rules`: true when the window host's rules placed it, and revier did not place it again; raised it, with `minimized`; was handed to the surface it was pressed over; or Esc hid it. A `WARN` `popup: switch`, `popup: focused window`, `popup: mark the raise` or `popup: window rules` with `err` is a step of the key that failed; the key went on, and after `popup: window rules` revier placed the popup. `popup hide after go` and `popup hide, quitting instead` are `WARN`s of a hide that failed: after a Go the popup stays, otherwise it quits. A `WARN` `popup: no switch socket` with `err` is a popup that could not listen on its socket at start, and `popup: switch socket` one whose socket failed later and was closed: a press over the popup then raises it, and does not switch its list (`docs/design/decisions.md` D182). A `WARN` `reload on raise` with `err` is a raise whose read of `config.toml` and the project files failed: the popup keeps the list it read last |
| `action`, `clone`, `each project`, `agent new`, `shell new`, `go agent`, `agent focus`, `attach`, `focus attached` | the operation named |
| `setup install: keys`, `setup install: term`, `setup install: rules`, and the same three of `setup uninstall` | a part of `revier setup` changed something: each desktop key's `step`; the terminal's `file` and `include`; the rules' `action`, `file`, revier's own rules file, and `moved`, the number of an older revier's rules taken out of `rules.json`. An `ERROR` line carries the `err` the run printed |
| `config written`, `kitty config written`, `kitty config removed`, `kitty keys removed`, `project created`, `project file written`, `project renamed`, `project deleted`, `target deleted`, `runtime switched` | the file or setting changed. `project renamed` has `from` and `to`, not `project`. A `WARN` `rename: the saved sessions keep the old name` follows when the saved sessions could not be renamed. An `ERROR` `delete after the close` with `project`, `target` (empty for a project) and `err` is a del that closed every step and could not then delete the project or the target |

## The event file

- `$STATE/events.jsonl` holds one JSON line per operation that succeeded in a
  project, and is never pruned (`docs/design/decisions.md` D112).
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
  saw, `by` `saw`. It is never pruned (`docs/design/decisions.md` D151).
- Read it with `revier state --at <time>`: what was open then, per machine.
  `revier state --session <id>` names why each difference between a save and
  what was open when it was made is one; `revier state --help` lists the
  reasons.
- A `ran` line is a run of a command target that moved the focus, with the
  `host` and `ref` the focus moved to. It is written only when that differs
  from the run on record, and it opens nothing: `revier state` lists it as
  done and holds no target open for it (`docs/design/decisions.md` D166).
- A `saw` line comes from a surface alone: with no TUI or popup running, the
  record holds only what operations did. `revier list` writes no `saw`
  line, only what its prune changed in the state file.
- A hidden popup surveys nothing unless a launch of its own is still coming
  up (`docs/design/decisions.md` D86), so it writes no `saw` line until its
  next raise. A change made while it was hidden carries the time of that
  raise, not of the change.
- A popup that wctl's overlay hid logs `popup: hidden by the desktop` when
  the next press raises it, or when a slow refresh's survey lists it
  minimized; until then it surveys every ten seconds and writes `saw` lines.
  A `popup: raise` line with `minimized=true` follows Esc's hide too. A popup
  that shows a screen, asks a question, such as a delete, or walks a restore
  or a shutdown logs no such line (`docs/design/decisions.md` D169).
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
