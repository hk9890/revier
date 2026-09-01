# Performance

What the hot path costs, measured rather than assumed. How to run the
benchmarks is [../TESTING.md](../TESTING.md)'s; this document owns what they
mean and what follows from them.

## The claim under test

The design routes every refresh through `core.Survey`, which asks each host for
*everything* it holds in one bulk call and then matches locally
([architecture.md](architecture.md), "Where the work happens"). The claim is
that a refresh therefore costs a constant number of host calls, and that cost
does not grow with the number of projects.

That claim is load-bearing. The TUI refreshes about once a second, and this
machine has roughly ninety projects. If it were false, every feature built on
`Survey` would inherit the mistake.

## Method

Measured 2026-09-01 on the development machine, against real tmux and a real
GNOME session, with ninety project files generated from the names in the
existing session store and five targets each — the shape the product will
actually see.

- End to end: `hyperfine --warmup 3 --min-runs 20` over `revier list`, one
  project against ninety.
- Core only, no subprocesses: `internal/core/bench_test.go` against the fake
  host, with one workspace instance per project plus forty unrelated windows.

## Results

End to end:

| Projects | Mean |
|---|---|
| 1 | 11.2 ms |
| 90 | 21.5 ms |

Core only:

```
BenchmarkSurvey1        33.8 µs      73 KB     732 allocs
BenchmarkSurvey90     2763   µs     6.1 MB   65369 allocs
BenchmarkSurvey360   13991   µs    24.4 MB  261522 allocs
BenchmarkRender         17.9 µs   per project, per refresh
BenchmarkMatchCompile    4.7 µs   per target,  per refresh
```

## What holds

**Host calls are constant in project count.** Two bulk calls for tmux
(`list-windows` and `list-panes`), one for GNOME (`list --json`), whether there
is one project or ninety. The central claim is correct, and the TUI can be built
on `Survey` as designed.

The 11.2 ms floor is process start plus those subprocess calls. Projects add
about 0.12 ms each on top, which is inside a keypress budget and irrelevant at a
one-second refresh.

## What does not hold

**Nearly all of the per-project cost is recomputed waste.** `core.Render`
re-parses templates and `Match.Compile` re-compiles regular expressions on every
refresh, from inputs that do not change between refreshes. At ninety projects
that is ninety renders and four hundred and fifty compiles per pass, which is
essentially the whole 2.76 ms.

The wall-clock cost is not the problem. The allocation rate is: **6.1 MB and 65k
allocations per Survey**, or about 6 MB/s of garbage at a one-second refresh,
produced entirely by re-deriving pure functions of constant input.

**Matching is O(targets x instances), and instances grow with open projects,**
so the shape is quadratic in how many projects are running. At ninety it is
invisible — the mild superlinearity from 90 to 360 is `Render`, not matching —
but it is the shape, and it should be measured rather than assumed before anyone
relies on it at a larger size.

## Recommended change, not yet made

Prepare projects once at load rather than once per refresh: render the templates
and compile the matches when `config.Load` returns, and have `Survey` and `Go`
take the prepared form. That removes most of the core cost and essentially all
of the garbage, and it needs no cache invalidation — it is the same principle
the design already applies to validation, which happens at load rather than at
the keystroke.

It changes the signatures of `Survey` and `Go`, so it is cheapest before the TUI
adds callers. Tracked as an issue in the `revier` taskmgr store.
