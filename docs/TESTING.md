# Testing

```bash
mise run test              # L1 + L2   fast, no processes, no display
mise run test:integration  # L3        adapter parsing against recorded output
mise run test:live         # L4        real processes: hc and sh, still headless
mise run test:all          # everything runnable without a screen
mise run quality           # fmt + vet + lint + build + L1-L3   (pre-commit)
mise run quality:full      # + L4                               (pre-handoff)
```

## Layers

The layer decides whether a test can run at all on a machine with no display.
Only L6 ever reaches one. L5 was retired and its number is not reused.

| Layer | Substrate | Tag | Covers |
|---|---|---|---|
| L1 | none — pure functions | — | matching, status encoding, project lookup (`pkg/revier`) |
| L2 | `internal/hosttest.Fake` | — | resolution, run-or-raise, toggle-back, survey, probe dispatch (`internal/core`) |
| L3 | recorded tool output | `integration` | adapter parsing: `kitten @ ls` JSON, `wctl list --json`, hc `hello` and `terminal.list`; the kitty view over hc on a listing built from a recorded one |
| L4 | real processes: hc on private sockets, `sh` scripts | `live` | the hc host, the core and the CLI against a real hc core beside `hosttest.FakeView`, the adapters that run a shell (`claude`, `execprobe`, `ssh`), and the process group of what `kitty` and `gnome` run |
| L6 | a werft lease's GNOME session ([RUNNING.md](RUNNING.md#drive-the-desktop-on-a-werft-lease)) | manual | the GNOME host, the kitty host's `Open` and `Focus`, and the kitty view over hc |

L2 is where most behaviour is pinned. The core's decisions depend only on what a
host *reports*, and `hosttest.Fake` reports whatever a test needs — which is why
resolution, toggle-back, and the degradation rules need no tool at all.

L2 does not replace L4. L4 pins that a press on a real hc core focuses what
`Open` made, as `hosttest.FakeView` reports the focus. Check at L6 whether a
real view moves the focus, and a read's history: neither has a headless test.

Keep hc the one headless runtime: a second runtime kept for tests alone is a
host maintained for nobody.

## What every host test must cover

- **`Open` produces what `Match` finds** ([CODING.md](CODING.md)).
  `TestOpenThenMatchFindsIt` in `internal/adapter/hc` is the shape.
- **`Instances` does not scale with the project count** ([CODING.md](CODING.md)).
  Assert on the tool invocation count, not on timing.
- **Free text survives the round trip.** Test a title that contains the host's
  field separator, a newline and a non-ASCII letter, as
  `internal/adapter/hc/testdata` records one.

## Conventions

- Tests sit next to the code. A live test file ends in `live_test.go` and
  carries `//go:build live`. An integration test file ends in
  `integration_test.go` and carries `//go:build integration`.
- Put a test file in the external `_test` package. When it needs an unexported
  name, put it in the package and end its name in `_internal_test.go`, or in
  `_internal_live_test.go` when it is live. Two exceptions: `export_test.go` is
  in the package and has no suffix, and no test file in `cmd/revier` has the
  suffix: all of them are in `package main`.
- Put a helper or a fake that more than one test file uses, or that a helper
  there uses, in the `helpers_test.go` of the package.
  - Use `helpers_internal_test.go` when in-package test files use it.
  - Use `export_test.go` when the external test package needs something from
    inside the package.
  - Use `helpers_live_test.go` or `helpers_integration_test.go`, with the
    build tag, when only tests of that tag use it.
  - Leave one that a single test file uses in that file.
- Give each package whose code reaches `config.Root`, `state.Root` or
  `sshconfig.Path`, by its own call or through another package, a `TestMain`
  in its `helpers_test.go` that runs `os.Exit(testenv.Run(m))`. A test that
  forgets its own `t.Setenv` then writes into a scratch directory whose
  configuration names no host, and the package fails, naming the file.
  - Set a new `REVIER_*` override in `testenv.Run`, and name it in the
    failure message there.
  - Add the function that resolves it to the list this rule opens with
    (`config.Root`, `state.Root`, `sshconfig.Path`).
  - `testenv.Run` sets the `REVIER_*` overrides, `KITTY_CONFIG_DIRECTORY` and
    `XDG_CONFIG_HOME`: `revier setup` writes kitty's configuration and wctl's
    rules file, and either can be a link into a dotfiles repository.
  - Set the variable of any other tool with `t.Setenv` in each test that
    reaches that tool's path.
  - Set `KITTY_CONFIG_DIRECTORY` with `t.Setenv` in each test that reads or
    writes kitty's configuration.
  - Give a package whose code reaches `kitty.ConfigDir` the `TestMain` above.
  - Give `revier setup` in a `cmd/revier` test fake stores through
    `app.stores`, as `termOnly` in its `helpers_test.go` does: the default
    probes this machine and writes its desktop keys.
- Call `testenv.NoTools(t)` in a test that must start no tool, such as git or
  mise, rather than setting PATH inline.
- Give a new test the context `t.Context()` returns, not
  `context.Background()`.
- Give a `t.Cleanup` function its own context: `t.Context()` is cancelled
  before cleanups run.
- In `internal/tui`, feed a command that a key press or an `Update` returned
  back to the model with a helper from `internal/tui/helpers_test.go`, not with
  an inline `m.Update(cmd())`: `deliver` returns the commands the model answers
  with, `run` drops them. Feed by hand only to assert between the run and the
  feed, as `TestEnterOnATargetRunsGo` does.
- In `internal/tui`, build a model that a test clicks with `refreshed` or
  `projectSurface`, whose clocks stand still, or give it the `clocked`
  helper's clock: on the real clock the double-click window in `mouse.go`
  passes or fails with the machine's load.
- In `internal/tui`, move time with the hand `clocked` returns, also on a model
  `refreshed` built: `refreshed` keeps no hand of its own.
- An adapter test that needs no recorded output and no tool stays untagged, so
  `mise run test` runs it: `internal/adapter/kitty/ref_internal_test.go`,
  `internal/adapter/gnome/keyswrite_test.go`.
- A live test starts its substrate on a socket named after itself and kills it
  in `t.Cleanup`, so suites cannot collide.
- A missing substrate skips (`t.Skip`), never fails: `mise run test:live` must
  stay green on a machine without hc.
- Keep hc pinned in `.github/workflows/ci.yml`: it is pre-1.0. To
  bump the pin, take the `linux-x86_64` URL and digest from
  `https://releases.herdr.dev/core/<version>/manifest.json`, and record a new
  `internal/adapter/hc/testdata/hc-<version>/`: `hello.json` and `list.json`,
  the `hello` and `terminal.list` answers of the private core in
  [RUNNING.md](RUNNING.md#drive-the-cli-against-a-scratch-configuration), with terminals that carry
  revier's labels. Point the paths in `hc_integration_test.go` at them.
- Run hc in a live test on a private core only: `world` in
  `internal/adapter/hc/helpers_live_test.go`, `scratch` in
  `cmd/revier/helpers_live_test.go`. Call `running` there before a test reads
  a panel's pid.
  - A live test of the core on hc sits in `internal/adapter/hc` for that
    helper, with `hosttest.FakeView` as its view: `view_live_test.go`.
  - Assert that a panel whose program exits at once fails the start only on
    the fake core, with `exitRun` in `internal/adapter/hc/helpers_test.go`:
    a real core lists the terminal before or after it ends, so a live test
    accepts either outcome.
- Build a `kitten @ ls` listing of the kitty view over hc from a recorded
  one, as `internal/adapter/kitty/testdata/ls-hc-view.json` is: recording
  one opens windows on a desktop. Run an `hc` command only as
  [RUNNING.md](RUNNING.md#drive-the-cli-against-a-scratch-configuration) says.
- Call `testenv.NoServiceManager(t, dir)` in a live test that reaches a start
  through `internal/userunit` with the real `systemd-run` on PATH, as `world`
  does: without it, the test starts a unit in the user's own service manager.
  Test the start itself with scripts named `systemd-run` and `systemctl` first
  on PATH, as `internal/userunit/live_test.go` does.

## Benchmarks

`internal/core/bench_test.go` holds the hot path benchmarks. They are not a
gate; run them when changing `Survey`, `Render`, or matching:

```bash
go test -run='^$' -bench='Survey|Render|MatchCompile' -benchtime=200x ./internal/core/
```

`BenchmarkSurvey90` is the size that matters. Put a result in the PR that
changes the hot path, never in the repository — a measurement is one run on
one machine and ages silently in a doc.

A change is green when `mise run quality:full` passes. Driving the product by
hand is [RUNNING.md](RUNNING.md)'s, and it is never a substitute for a layer.
