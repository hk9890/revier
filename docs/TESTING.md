# Testing

```bash
mise run test              # L1 + L2   fast, no processes, no display
mise run test:integration  # L3        adapter parsing against recorded output
mise run test:live         # L4        real processes: tmux and sh, still headless
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
| L3 | recorded tool output | `integration` | adapter parsing: `kitten @ ls` JSON, `wctl list --json` |
| L4 | real processes: tmux on a private socket, `sh` scripts | `live` | the tmux host, the core against a real substrate, and the adapters that run a shell (`claude`, `execprobe`, `ssh`) |
| L6 | the user's GNOME session | manual | the GNOME host, and the kitty host's `Open` and `Focus` |

L2 is where most behaviour is pinned. The core's decisions depend only on what a
host *reports*, and `hosttest.Fake` reports whatever a test needs — which is why
resolution, toggle-back, and the degradation rules need no tool at all.

L2 does not replace L4. The fake has no opinion about focus, so `Go` not
focusing after `Open` passed L2 and failed L4.

## What every host test must cover

- **`Open` produces what `Match` finds** ([CODING.md](CODING.md)).
  `TestOpenThenMatchFindsIt` in `internal/adapter/tmux` is the shape.
- **`Instances` does not scale with the project count** ([CODING.md](CODING.md)).
  Assert on the tool invocation count, not on timing.
- **Free text survives the round trip.** Test a title that contains the host's
  field separator. tmux 3.4 escapes non-printable bytes that 3.7 passes
  through, so a control-character delimiter passes locally and fails in CI.

## Conventions

- Tests sit next to the code. A live test file ends in `live_test.go` and
  carries `//go:build live`.
- An adapter test that needs no recorded output and no tool stays untagged, so
  `mise run test` runs it: `internal/adapter/kitty/ref_test.go`,
  `internal/adapter/gnome/keyswrite_test.go`.
- A live test starts its substrate on a socket named after itself and kills it
  in `t.Cleanup`, so suites cannot collide.
- A missing substrate skips (`t.Skip`), never fails: `mise run test:live` must
  stay green on a machine without tmux.
- Leave tmux unpinned in `.mise.toml`: the live layer runs against
  whatever is installed, which is how CI caught the delimiter difference above.
- tmux leaves an inert socket file in `/tmp/tmux-$UID/` after `kill-server`.
  A `revier-test-*` entry there is a dead socket, not a leaked server; confirm
  with `tmux -L <name> list-sessions`, which reports no server running.
- `mise run test:coverage:summary` reports and gates nothing. A covered line is
  one that ran, not one whose behaviour anything asserted.

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
