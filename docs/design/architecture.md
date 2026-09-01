# Architecture

## Shape

Ports and adapters. The core holds the domain and the orchestration and knows
nothing about kitty, GNOME, tmux, or Claude. Every tool-specific fact lives
behind one of two interfaces, defined in [interfaces.md](interfaces.md).

```
                    ┌──────────────┐
                    │     TUI      │
                    └──────┬───────┘
                           │
                    ┌──────▼───────┐
     config ───────▶│     core     │  match, run-or-raise, toggle-back
                    └──┬────────┬──┘
                       │        │
            ┌──────────┘        └──────────┐
            │                              │
      ┌─────▼─────┐                 ┌──────▼──────┐
      │   Host    │                 │ AgentProbe  │
      └─────┬─────┘                 └──────┬──────┘
            │                              │
    Runtime │ WindowController          claude
    ────────┼────────────────           opencode*
    kitty   │ gnome                     aider*
    tmux*   │ sway*
    wezterm*│ hyprland*

  * not in the first version
```

## Two ports

| Port | Answers | Second implementation |
|---|---|---|
| **Host** | What instances exist, how do I open one, how do I focus one? | Every adapter. `Runtime` and `WindowController` are the same interface with different providers behind them. |
| **AgentProbe** | What is this agent doing right now? | opencode, then any other harness |

`Host` is one interface because run-or-raise is one operation. A compositor
provides instances as OS windows; a terminal provides them as panes and
windows. The core asks the same five questions of both.

`Runtime` adds `Capabilities` and is the only host whose instances carry panels,
because only it can see inside a terminal. `WindowController` adds nothing —
the named type exists so the wiring states which role an adapter fills.

Three things that look like ports and are not:

- **The store.** Projects are TOML files on disk. There is no second storage
  backend planned, so it is a package, not an interface.
- **Output format.** `--json` is a renderer over the same view type the TUI
  reads. A format is not a port.
- **Matching.** The core matches instances against realizations. Putting it
  behind the port would duplicate it in every adapter and let two adapters
  disagree about what a match means.

## Where the work happens

The core owns everything that is not tool-specific:

- **Template rendering.** A `Realization` is rendered once, at load, before it
  reaches a host, so no adapter ever sees `{{.Path}}` and no refresh renders.
- **Matching.** Each host returns its instances in one bulk call; the core
  matches them against every project's realizations locally. This is why the
  TUI refresh costs one call per host rather than one per project.
- **Resolution.** When both realizations of a target are available, the core
  picks: the target's `Prefer` field if set, otherwise the window host when a
  `WindowController` is configured. A separate window is what a desktop user
  expects; one line of config overrides it.
- **Toggle-back.** The core records the home ref before activating a target and
  returns to it when the same key is pressed again.
- **Focus after open.** Some hosts focus what they launch and some do not, so
  the core focuses explicitly on the run path. Without it the raise half of
  run-or-raise holds only by accident of the host: the instance opens behind on
  every host that does not focus its own launches.
- **Raising a terminal's OS window.** A runtime whose instances are OS windows
  says so (`Capabilities.OSWindows`), and the core raises the window through
  the window host after focusing inside the runtime. A terminal on Wayland
  cannot raise itself.

An adapter implements five methods and holds no policy.

## Adapter selection

Each adapter implements `Probe(ctx) error`, which returns nil when the adapter
can work in the current environment. Selection order:

1. The adapter named in config, if set. A named adapter that fails `Probe` is a
   hard error — the user asked for it by name.
2. Otherwise the first adapter in the preference list whose `Probe` succeeds.
3. No `WindowController` probes successfully: the core runs without one, and
   window-only targets report as unavailable rather than failing at the
   keystroke.

Detection signals the adapters use:

| Adapter | Signal |
|---|---|
| kitty | `kitten` on PATH or beside `kitty`; a `@kitty-<pid>` control socket answers, or no kitty runs yet |
| tmux | `$TMUX`, `tmux` on PATH |
| wezterm | `$WEZTERM_PANE` |
| gnome | `$XDG_CURRENT_DESKTOP` contains GNOME, `wctl` on PATH |
| sway | `$SWAYSOCK` |
| hyprland | `$HYPRLAND_INSTANCE_SIGNATURE` |

## Package layout

```
cmd/revier/            main; CLI, and the one file that wires adapters
pkg/revier/            the ports and the shared types
internal/core/         match, run-or-raise, toggle-back, resolution
internal/config/       TOML load, template rendering, validation
internal/state/        per-session attachments, which do not belong in config
internal/adapter/
    kitty/
    tmux/
    gnome/
    claude/
internal/tui/          the one TUI surface
docs/design/
```

`pkg/revier` is public because an out-of-tree adapter must import the
interfaces. Everything else is `internal/` so the public surface stays small and
stable. Go convention puts an interface in the package that consumes it; this
project overrides that convention because the interfaces are a product feature,
and adapter authors need one obvious import path.

Adapters are wired explicitly in `cmd/revier/adapters.go`, not through `init()`
registration. Explicit wiring is greppable, and it makes the set of compiled-in
adapters a single readable list.
