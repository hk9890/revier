# Testing

`mise` is the canonical surface; `make` covers the subset that must work
without it.

```bash
mise run test              # L1 + L2   fast, no processes, no display
mise run test:integration  # L3        adapter parsing against recorded output
mise run test:live         # L4 + L5   real substrates, still headless
mise run test:all          # everything runnable without a screen
mise run quality           # fmt + vet + lint + build + L1-L3   (pre-commit)
mise run quality:full      # + L4/L5                            (pre-handoff)
```

`make test`, `make test-integration`, `make vet`, `make fmt` are the fallback.
There is no `make lint`: golangci-lint is pinned in `.mise.toml`.

## Layers

The layer decides whether a test can run at all on a machine with no display.
Only L6 ever reaches one.

| Layer | Substrate | Tag | Covers |
|---|---|---|---|
| L1 | none — pure functions | — | matching, status encoding, project lookup (`pkg/revier`) |
| L2 | `internal/hosttest.Fake` | — | resolution, run-or-raise, toggle-back, survey, probe dispatch (`internal/core`) |
| L3 | recorded tool output | `integration` | adapter parsing: `kitten @ ls` JSON, `swaymsg -t get_tree`, `wctl list --json` |
| L4 | real tmux, private socket | `live` | the tmux host, and the core against a real substrate |
| L5 | real sway, `WLR_BACKENDS=headless` | `live` | a real window host with no screen |
| L6 | the user's GNOME session | manual | the GNOME host only |

L2 is where most behaviour is pinned. The core's decisions depend only on what a
host *reports*, and `hosttest.Fake` reports whatever a test needs — which is why
resolution, toggle-back, and the degradation rules need no tool at all.

L4 is not redundant with L2. It caught that `Go` never focused after `Open`,
which L2 could not see because the fake had no opinion about what launching
does to focus. An adapter layer proves the tool is actually driven; the fake
proves only that the core is self-consistent.

L5 is why the sway host is worth writing before Hyprland or KWin: `sway` on the
headless wlroots backend gives a real compositor with real IPC and no display,
so window control gets the same treatment as tmux. GNOME cannot be tested this
way — `wctl` needs a live logged-in session — which is what confines L6 to that
one adapter.

## Two invariants every host test must cover

- **`Open` produces what `Match` finds.** Otherwise run-or-raise opens a second
  instance every time. `TestOpenThenMatchFindsIt` is the shape; a new host owes
  the same test.
- **`Instances` does not scale with the project count.** A host that queries
  per project turns a refresh into O(projects). Assert on the tool invocation
  count, not on timing.
- **Free text survives the round trip.** Window and pane titles carry arbitrary
  characters, including whatever a host uses as a field separator. tmux 3.4 also
  escapes non-printable bytes that 3.7 passes through, so a control-character
  delimiter passes locally and fails in CI. Test a title containing the
  separator.

## Conventions

- Tests sit next to the code. Live tests are `live_test.go`, behind
  `//go:build live`.
- A live test starts its substrate on a socket named after itself and kills it
  in `t.Cleanup`, so suites cannot collide.
- A missing substrate skips (`t.Skip`), never fails: `mise run test:live` must
  stay green on a machine without sway.
- tmux leaves an inert socket file in `/tmp/tmux-$UID/` after `kill-server`.
  A `revier-test-*` entry there is a dead socket, not a leaked server; confirm
  with `tmux -L <name> list-sessions`, which reports no server running.
- `mise run test:coverage:summary` reports and gates nothing. A covered line is
  one that ran, not one whose behaviour anything asserted.

A change is green when `mise run quality:full` passes. Driving the product by
hand is [RUNNING.md](RUNNING.md)'s, and it is never a substitute for a layer.
