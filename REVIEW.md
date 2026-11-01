# Full code review — 2026-11-01

Reviewed at `32715d3` (main). This review changes no code; it only reports.

Scope: every package under `cmd/`, `internal/`, `pkg/` and `scripts/`, the CI and release
configuration, and the docs, judged against the rules in `AGENTS.md` and `docs/`
(CODING.md invariants, TESTING.md conventions, REVIEWING.md citation rules,
DOCUMENTING.md).

## How this was verified

Two passes:

1. **Find.** Six reviewers, one per area (core, adapters, config and stores, TUI, cmd and CI,
   docs), read the code against the docs. Each one wrote repro tests where it could.
2. **Refute.** Three more reviewers took the 28 claims of pass 1 and tried to disprove each
   one. They checked:
   - whether production wiring reaches the code path;
   - whether `decisions.md` or the user guide makes the behaviour deliberate;
   - whether a real user would hit it.

   They also repeated the repros. Pass 2 dropped three claims, dropped half of two more,
   and changed several severities. The dropped claims are listed at the end.

Every finding below survived pass 2. A finding is **reproduced** when a temporary test or a
run showed the failure; otherwise it is **by reading**. The repro files were deleted
afterwards.

## Summary

| # | Severity | Area | Finding |
|---|---|---|---|
| 1 | High | core | `Core.clone` drops `Run`: every command target fails when pressed from the TUI |
| 2 | Medium | tui | The action form saves through a stale index after a reload: wrong action overwritten, or a panic |
| 3 | Medium | tui | The shared-target form has the same stale index: wrong target written, or a panic |
| 4 | Medium | kitty | Abstract `@kitty-<pid>` sockets are still queried, though the v0.14 changelog promised their removal |
| 5 | Low-Med | config | A relative project `path` is accepted, and the TUI project screen can write one |
| 6 | Low | core | The window host's failure is reported as "lists no window" |
| 7 | Low | core | Keys whose last part is `+` or `-` pass validation but never fire in the TUI |
| 8 | Low | config | `place` is validated before its template is rendered |
| 9 | Low | tui | The dialog after an end with no shutdown is dropped if the surface is not plain |
| 10 | Low | session | Two concurrent saves in the same second become one |
| 11 | Low | app | A shutdown record that failed to save still deletes `seen.toml` |
| 12 | Low | kitty | `Open` leaves a half-built OS window when a later step fails |
| 13 | Low | herdr | `Open` leaks a workspace when the create reply is lost |
| 14 | Low | kitty | `Open` passes `--hold` to a panel with no command |
| 15 | Low | tmux | A `\|` in a pane's argv[0] shifts the vars field |
| 16 | Low | ssh | Remote command quoting breaks under a fish login shell |
| 17 | Low | cmd | Six subcommands drop extra positional arguments without an error |
| 18 | Low | state | A corrupt `state.json` is discarded with no log line |
| 19 | Low | cmd | The piped TUI path has no deadline |
| 20 | Low | cmd | `agent wait --timeout` overflows on large values and `inf` |
| 21 | Low | cmd | `revier shutdown` exits 0 with targets still open |
| 22 | Low | config | A project name with whitespace loads |
| 23 | Low | tui | ctrl+c at a y/n question dismisses it instead of quitting |
| 24 | Low | gnome | `execRun` sets no `WaitDelay` |
| 25 | Low | sshconfig | A relative `Include` in a nested file resolves against the wrong directory |
| 26 | Low | CI | Release and build hardening |

---

## High

### 1. `Core.clone` drops `Run`, so command targets cannot be pressed in the TUI — reproduced

- **Where:** `internal/core/core.go:132-152`. `clone()` copies every configured field except
  `Run` (`internal/core/core.go:68`).
- **Path:**
  - `cmd/revier/adapters.go:214` builds the core with `Run: runTargetCommand`.
  - The TUI gets `a.core.WithLedger(...)` (`cmd/revier/tui.go:36`), so its core has
    `Run == nil`.
    - `WithRuntime` (`internal/tui/configscreen.go:136`) and `internal/tui/sessions.go:396`
      also clone.
    - Nothing sets `Run` again.
  - Enter or a target key reaches `runCommand` through `goTarget` → `ActivateWaiting`
    (`internal/tui/tui.go:1571`). `runCommand` refuses with
    `<target>: no command runner is configured` (`internal/core/command.go:91-92`).
- **Failure scenario:** every press of a command target in the TUI fails. Two things still
  work:
  - the second press, which toggles home, because it returns before the check;
  - `revier go`, which uses the original core (`cmd/revier/app.go:308`).
- **Evidence:** reproduced in both passes. The same press runs on the original core and fails
  on `WithLedger(l)` and on `WithRuntime(rt)`.
- **Test gap:** `internal/tui/commandtarget_test.go:29` builds its core directly, so it never
  clones. No test asserts that `clone` keeps every field. `WithLedger` has 0% coverage.

## Medium

### 2. The action form saves through a stale index — reproduced

- **Where:** `internal/tui/configactions.go:54` keeps `index: i` when the form opens.
  `saveAction` uses it at `:116-123`.
- **Defect:** a blur, then a focus, sends `reloadAssisted` (`internal/tui/tui.go:928-971`).
  That replaces `sf.actions` while the form stays open. It happens when the user edits
  `config.toml` elsewhere, or when the assistant writes it, and comes back.
  `config.ReplaceAction(root, f.index, sf.actions[f.index], act)` cannot detect this, because
  `was` is read from the reloaded list.
- **Failure scenarios:**
  - **Overwrite.** The form is open on `build` of `[sync, build]`, and an action is added
    above both. The user changes the name and the key and saves. `sync` is replaced and
    `build` stays.
  - **Crash.** The file loses actions while the form is open on an index past the new end.
    Enter panics with `index out of range [1] with length 0` at `configactions.go:121`.
  - **Mitigation:** if the name or the key is unchanged, `checkAction` refuses the save as a
    duplicate. A silent overwrite needs both changed.

### 3. The shared-target form has the same stale index — reproduced

- **Where:** `internal/tui/configtargets.go:199-214` (`saveTarget`, `cs.tform.index`).
  The panic is in `internal/config/targets.go:451` (`putPanels`).
- **Defect:** the same mechanism as finding 2. `ReplaceTarget` checks only `i >= len(have)`.
- **Failure scenarios:**
  - **Wrong target.** The form is open on `editor` (index 1), and a target is inserted above
    it. Save writes to `home`. In the repro, project validation happened to refuse the write;
    a replacement that breaks no rule is written.
  - **Crash.** The form is open on `agent`, which has two panels, and a target is inserted
    above it. Save panics with `index out of range [0] with length 0` in `config.putPanels`:
    the form's panels point into a different target.

### 4. kitty still queries abstract `@kitty-<pid>` sockets — by reading

- **Where:** `internal/adapter/kitty/kitty.go:84-89` (`abstractSocket`), `:130-140`
  (`socketPID`), `:365-374` (discovery from `/proc/net/unix`).
- **Defect:** an abstract socket has no file permissions, and nothing checks the peer's uid
  (no `SO_PEERCRED`).
- **Failure scenario:** another local user, or a container on the host network, binds
  `@kitty-<N>`. It can then:
  - answer `ls` with forged windows;
  - receive what revier sends with `SendText`, which is agent prompts;
  - answer `ReadPanel`.
- **Why medium:** the comment keeps the path for kitties started before 0.13. The v0.14.0
  changelog (`CHANGELOG.md:712-713`) says such a kitty is found in that release and no longer
  in the next. The tree is at v0.24.0, and the path is still there.

## Low-medium

### 5. A relative project `path` is accepted — reproduced

- **Where:** `internal/config/config.go:568` (`validateProject`) refuses only an empty path.
  The path is used at `internal/checkout/checkout.go:87, 98, 109`.
- **Reachable from the TUI:** `revier new` and the new-project form write an absolute path
  (`internal/tui/newproject.go:298`). The project screen's path field
  (`internal/tui/projectscreen.go:259`) writes any value through `config.SetProjectValue`.
- **Failure scenario:** with `path = "src/rel"`:
  - `checkout.Ensure` stats and clones relative to the process's working directory.
  - `mise trust` trusts that directory.
  - Resolving the project from the working directory never matches it.

## Low

### 6. The window host's failure is reported as "lists no window" — reproduced

- **Where:** these paths check only the target's own host in `failed`, then call `raisable`:
  - `internal/core/core.go:667, 764` (`goResuming`)
  - `internal/core/agentfocus.go:71`
  - `internal/core/agenttab.go:648`
  - `internal/core/panel.go:435`
- **Defect:** `Core.Focus` (`core.go:1153-1157`) checks the window host first, as decisions.md
  D89 asks. These paths do not.
- **Failure scenario:** a kitty workspace is open and `wctl list` times out. The press is
  refused with `cannot raise: the window host lists no window for it` (`ErrUnraisable`). The
  TUI shows that as a refusal (`internal/tui/failure.go:41`), and the timeout never reaches
  the footer.

### 7. Keys that end in `+` or `-` are dead in the TUI — reproduced

- **Where:** `internal/core/chord.go:112-118` and `internal/tui/targetkeys.go:23`.
- **Defect:** config load accepts `ctrl-plus` and `alt-minus`, which are canonically
  `ctrl+plus` and `alt+minus`. bubbletea names those presses `ctrl++` and `alt+-`, and
  `ParseChord` refuses both ("has an empty part").
- **Failure scenario:** an `[[action]]` bound to `alt-minus` loads, the config screen accepts
  it, and it never fires. `Chord.Terminal` does not help:
  - with key disambiguation it returns the chord unchanged;
  - legacy terminals send ESC followed by `-`.

### 8. `place` is validated as written, not as rendered — reproduced

- **Where:** `internal/config/config.go:727-731`. Rendering happens later, at
  `internal/core/render.go:75`.
- **Defect:** `docs/user-guide/configuration.md:189` lists `place` among the fields that take
  templates.
- **Failure scenarios:**
  - **Wrongly refused:**
    - `place = "{{.Vars.geom}}"` with `geom = "right top 50% 100%"`;
    - `place = "{{ .Name }} top 50% 100%"`.
  - **Wrongly accepted:** `place = "{{.Vars.x}} top 50% 100%"` with `x = "left top"` loads
    clean and renders to five tokens. wctl then refuses it after the window has opened, which
    is the case the rule's comment says it prevents.
  - **Workaround:** write the four tokens literally.

### 9. The dialog after an end with no shutdown is dropped if the surface is not plain — reproduced

- **Where:** `internal/tui/seen.go:104-107`.
- **Defect:** `m.recovered = true` is set unconditionally, and `openUnclean` runs only if
  `m.plain()` (`internal/tui/tui.go:1167-1169`). Nothing retries it.
- **Failure scenario:** in the popup, the user types a query before the first full survey
  answers. The offer is dropped for good, though `docs/user-guide/tui.md:216-220` says the
  surface opens on that dialog.
- **Impact:** the session is still saved as `last-seen` (`internal/app/session.go:161`) and
  is listed on the sessions screen. Only the offer is lost.

### 10. Two concurrent session saves in the same second become one — reproduced

- **Where:** `internal/session/session.go:177-186` (`free`) and `:163-173` (`Save`).
- **Defect:** `free` checks whether the name is taken, and the write renames over it with no
  `O_EXCL`. This breaks the promise in `Save`'s doc comment that "two saves in one second do
  not silently become one".
- **Realism:** this needs two `Save` calls in the same second, inside the same few
  milliseconds. For example, a TUI alt+s and a scripted `revier session save`. `PromoteSeen`
  cannot take part, because it uses a past timestamp.
- **Evidence:** two concurrent `Save` calls with the same `At` left one session in 99 of 100
  rounds.

### 11. A shutdown record that failed to save still deletes `seen.toml` — by reading

- **Where:** `internal/app/shutdown.go:139-157`.
- **Defect:** `recorded` and `r.at` are set inside the `apply` closure. `state.Update` can
  still fail at `s.Save` (`internal/state/state.go:187-189`), and `Ledger.Update` returns no
  error, so `DropSeen` runs anyway. The doc comment at `:27-28` says the shutdown "is then not
  on record".
- **Failure scenario:** the state write fails, for example on a full disk. No unclean-end
  offer is made at the next start. After `--no-save`, the snapshot is lost.

### 12. kitty `Open` leaves a half-built OS window when a later step fails — reproduced

- **Where:** `internal/adapter/kitty/kitty.go:656-676`.
- **Defect:** `openFirst` (`:727-728`) names the OS window at once. A later failure returns
  without `close-window`. The failing step can be:
  - `title()` at `:660`;
  - a `launch` at `:669`;
  - `ls` at `:678`.
- **The rule it breaks:** tmux `Open` (`tmux.go:344-349`), herdr `Open` (`open.go:75-79`) and
  kitty `OpenTab` (`:912-916`) all clean up, because what is left running "would be matched".
- **Failure scenario:** the next press raises the partial window, for example an agent with
  no shell beside it.
- **Likelihood:** rare. It needs a timeout or a kitty that died right after `openFirst`.

### 13. herdr `Open` leaks a workspace when the create reply is lost — by reading

- **Where:** `internal/adapter/herdr/open.go:65-68`.
- **Defect:** `workspace.create` sets `label: r.Name`, and `titleOf` (`herdr.go:181-186`)
  falls back to the label. If ctx ends after herdr made the workspace but before the reply is
  read, `Open` returns before its close path. `OpenTab` handles this case by diffing the tabs
  (`panels.go:62-73`).
- **Failure scenario:** the next press raises a workspace that holds only herdr's default
  shell.

### 14. kitty `Open` passes `--hold` to a panel with no command — by reading

- **Where:** `internal/adapter/kitty/kitty.go:664` and `:728`. `OpenTab` omits `--hold` for an
  empty command (`:934-939`), since commit f119df8.
- **Failure scenario:** in a workspace opened by `Open`, the user types `exit` in a shell
  panel, and kitty starts a new shell instead of closing the window. kitty 0.49's
  `--hold` help: "Keep the window open even after the command being executed exits, at a
  shell prompt".
- **Stale comment:** the comment at `:935-936` ("waiting for a key") describes older kitty
  behaviour.

### 15. A `|` in a pane's argv[0] shifts the tmux vars field — reproduced

- **Where:** `internal/adapter/tmux/tmux.go:147-150, 165`.
- **Defect:** `#{pane_current_command}` is free text placed in the middle of the line, and the
  separator is `|`. tmux cuts argv[0] at the first space but keeps `|`.
- **Failure scenario:** a pane runs `exec -a 'x|y' sleep 60`, and on a private socket,
  `list-panes` printed `%0|x|y|tab=agent|ttl`. The 7-field split keeps the line:
  - the command reads `x`;
  - the vars read `y`;
  - the real `@revier` vars move into the title.

  A tab's identity is its vars mark (decisions.md D64), so nothing matches that tab, and the
  next press opens another copy.
- **Realism:** this needs a `|` in argv[0] before the first space. No common tool does this.
- **Test gap:** TESTING.md requires a free-text separator test.
  `TestFreeTextContainingTheSeparatorSurvives` covers the session name and pane title only.

### 16. Remote command quoting breaks under a fish login shell — by reading

- **Where:** `internal/adapter/ssh/ssh.go:85-96` (the comment and `login`), `:135-142`
  (`Quote`), `:186-188` (`doubleQuoted`).
- **What fish does:** per the fish docs (Quotes):
  - inside double quotes, the only escapes are `\"`, `\$`, a backslash before a newline, and
    `\\`;
  - inside single quotes, the only escapes are `\'` and `\\`.
- **Failure scenarios:** the line goes through fish twice, once in sshd's login shell and
  once in `$SHELL -lc`.
  - A name with a backtick arrives with an extra backslash.
  - A name that ends in `\` leaves an unterminated quote.

  `list` (`:240-245`) sends every project name of a host in one call, so one such name fails
  the whole host listing. `ValidateName` (`internal/config/create.go:31-41`) allows both
  characters.
- **Stale comment:** the comment's "POSIX shells and fish read alike" is wrong for "\`".

### 17. Six subcommands drop extra positional arguments without an error — reproduced

- **Where:**
  - `cmd/revier/session.go:53, 203`
  - `cmd/revier/keys.go:82`
  - `cmd/revier/keysapply.go:28`
  - `cmd/revier/term.go:60, 166`

  All of them discard `parseArgs`' positional result.
- **Failure scenario:** `revier session save before-reboot` saves an unnamed session and
  exits 0, so the label the user typed is lost. Every other command rejects extra positionals,
  for example `attach.go:24`, `link.go:27` and `events.go:71`.

### 18. A corrupt `state.json` is discarded with no log line — by reading

- **Where:** `internal/state/state.go:155-160`. `internal/ledger/ledger.go:64-65` logs only a
  non-nil error.
- **Defect:** the code comment calls the discard deliberate because state is "only a
  convenience". That is stale: state now holds `Bound` and `ShutDown` (decisions.md D141).
  `docs/MONITORING.md:79` documents a `state load` line, which this case never writes.
- **Failure scenario:** a hand edit or a disk fault corrupts the file. Bindings and the
  shutdown record vanish, and the next `Update` overwrites the file, with nothing in the log.

### 19. The piped TUI path has no deadline — by reading

- **Where:** `cmd/revier/tui.go:27` calls `cmdList(context.Background(), …)`. `revier list`
  gets the `commandWait` context (`cmd/revier/main.go:345, 357`). `Survey` sets no bound of
  its own.
- **Failure scenario:** `revier | grep foo` with a linked host whose remote revier hangs after
  connecting blocks for good. ssh's `ConnectTimeout` and keepalives end only a dead host.

### 20. `agent wait --timeout` overflows — reproduced

- **Where:** `cmd/revier/agent.go:177-179`.
- **Defect:** the float-to-`Duration` conversion is not range-checked.
- **Failure scenario:**
  - `--timeout 1e10` exits 2 within microseconds with "timed out after 1e+10s".
  - `--timeout inf` does the same.
  - `NaN` waits for good.

### 21. `revier shutdown` exits 0 with targets still open — by reading

- **Where:** `cmd/revier/shutdown.go:178-189`. `Counts` (`internal/core/shutdown.go:496-508`)
  counts targets still open, or not read, as `open` and not as failed.
- **Failure scenario:** `revier shutdown && poweroff` powers off while an editor still asks
  about unsaved work.
- **Defensible:** `projects.md:176-178` documents the line `shutdown complete; safe to leave`
  as the success signal, and `cli.md` has no shutdown-specific exit code. Decide whether to
  document exit 0 or change it.

### 22. A project name with whitespace loads — reproduced

- **Where:** `internal/config/config.go:571` calls only `CanAddress`. `ValidateName` is called
  only on create (`create.go:182`, `tui/newproject.go:308`, `tui/link.go:776`).
- **Failure scenario:** a file named `my proj.toml` loads with no problem.
  `configuration.md:66-68` tells the user to follow the naming rule, but revier does not
  enforce it. No breakage was found, since argv is passed without a shell.

### 23. ctrl+c at a y/n question dismisses it instead of quitting — by reading

- **Where:** `internal/tui/files.go:235-242`, `internal/tui/configactions.go:171-176`,
  `internal/tui/configtargets.go:225-230`.
- **Defect:** this is deliberate ("any other key: keep"). But it differs from every other
  screen, which matches `keys.Quit` first, and from `docs/user-guide/tui.md:84`
  ("ctrl+c: Quit").
- **Impact:** a second ctrl+c quits, and nothing is lost.

### 24. gnome `execRun` sets no `WaitDelay` — by reading

- **Where:** `internal/adapter/gnome/keys.go:147-155`, used for `gsettings`, `dconf` and
  `wctl`.
- **Defect:** `CommandContext` kills the direct child only. A grandchild that holds the pipe
  would block `Wait` past the deadline. None of today's tools forks such a child, so this is
  consistency only: the ssh and claude adapters set `WaitDelay` (`ssh.go:124`,
  `claude.go:304`).

### 25. A relative `Include` in a nested ssh config file resolves against the wrong directory — by reading

- **Where:** `internal/sshconfig/sshconfig.go:118-119`.
- **Defect:** ssh_config(5), Include: "Files without absolute paths are assumed to be in
  ~/.ssh if included in a user configuration file". revier resolves them against the
  including file's directory.
- **Failure scenario:** the two agree for `~/.ssh/config`. They differ for a relative Include
  inside `~/.ssh/config.d/x`, and then the remote screen's host list misses hosts.

### 26. Release and build hardening — by reading

- `.github/workflows/release.yml:62`: the GoReleaser binary floats (`version: "~> v2"`)
  in a job that holds `contents: write`, `id-token: write` and `attestations: write`. The
  action itself is SHA-pinned.
- `.github/workflows/release.yml:27-30`: `setup-go` caches by default in the signing job.
  Only main's cache scope can poison it, and that needs push access.
- No CI job runs `goreleaser check`, so a broken `.goreleaser.yaml` shows up only at release
  time.
- `makefile:13`: `make test` runs `go test -race` without `CGO_ENABLED=1`. `.mise.toml`
  exports `CGO_ENABLED=0`, so it fails in a mise-activated shell. The makefile is meant for use
  without mise, which lowers the impact.

## Refuted in pass 2

- **The claude `Holds` cache is wiped on every frame.** `Holds` has one production caller,
  `startAgent` (`internal/core/session.go:685`). That runs once per agent per restore or
  resume, not per frame. Only the comment at `internal/adapter/claude/claude.go:279-281`
  ("asks for every recorded agent on every frame") is stale.
- **The logs screen freezes the TUI.** `logging.Read` runs inside `Update`
  (`internal/tui/logs.go:131`), but reading a real 14-day log directory (2.0 MB) took
  8-10 ms. A visible freeze needs about 100 MB.
- **`list --conversations` without `--json` is ignored without a word.** The flag's help text
  scopes it to `--json` (`cmd/revier/list.go:23`).
- **`HostName %h` is not expanded.** This is true, but the result does not change.
  `config.Machine.Is` already adds the alias, and `core.ThisMachine` compares only the first
  DNS label.
- **`PromoteSeen` races a `Save`.** It uses a past timestamp, so it cannot share a second with
  a fresh save. Finding 10 is narrowed to two `Save` calls.

## Test gaps

- No test that a `Core` clone keeps every field (finding 1).
- No TUI test that saves a form after a `reloadedMsg` (findings 2 and 3).
- No core test that presses a target, or goes to an agent, while the window host fails on an
  OSWindows runtime (finding 6).
- The tmux free-text test does not cover `pane_current_command` (finding 15).
- No test at all:
  - `revier agent exec`
  - `revier shell exec`
  - `serve()` (`cmd/revier/serve.go`)
- Never driven through `run()`:
  - `cmdStatus`
  - `cmdAgentClose`
  - `holdOpen`
  - `becomeAgent`
- 0% coverage in core:
  - `Unsurveyed`
  - `ClosesTab`
  - `BusyRefusal.Error` / `Unwrap`
  - `InstalledTermKeys`
  - `RemoteAnswers.Renamed`

## Checked and found clean

- **Decision citations.** Every `Dnn` cited in Go and in the docs names an existing `### Dnn`
  heading. Also checked:
  - no heading number is duplicated;
  - no entry is over 8 lines;
  - no conflict marker is left;
  - Go citations use the `(decisions.md Dnn)` / `(Dnn)` form.
- **Docs against the code.** These are true against the code:
  - OVERVIEW.md: the layout table and the search commands;
  - CODING.md, TESTING.md, MONITORING.md, RUNNING.md and RELEASING.md: the symbols, tasks,
    paths and recipes;
  - the user guide's 267 links and anchors;
  - `cli.md`'s list of commands;
  - the v0.24.0 changelog section.

  The exceptions are findings 9, 22 and 23.
- **Test conventions.** Every package that reaches `config.Root`, `state.Root`,
  `sshconfig.Path` or `kitty.ConfigDir` has a `TestMain` with `testenv.Run`. Test file names
  and build tags agree.
- **Invariants.**
  - No adapter compiles a `Match` or reads `Target`, `Project`, `Prefer` or templates.
  - Every host has an Open-then-Match test and an invocation-count test.
  - The survey makes no per-project host call.
- **Injection.**
  - Command targets, `assist`, `each` and `run` build argv without a shell. `each --filter`
    uses `sh -c` by design.
  - ssh puts `--` before the host, and config refuses a host that starts with `-`.
  - `tmux send-keys -l --` is safe.
- **CI.** Every action is SHA-pinned, and no `${{ github.event.* }}` appears in a `run:` step.
  Permissions are least-privilege outside the release job.
- **Stores.**
  - The atomic write is correct: sync, chmod, rename, and temp cleanup.
  - The state `flock` and the single-write `O_APPEND` appends are correct.
  - The runlog path-traversal guard holds.
  - `checkout` clones with `--`.
- **TUI robustness.** A resize sweep over every screen ran with no panic: widths 0-250 and
  heights 0-40, with keys and clicks at each size. Stale async answers are sequence-guarded.
