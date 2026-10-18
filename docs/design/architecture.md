# Architecture

## Shape

Ports and adapters. The core holds the domain and the orchestration and knows
nothing about kitty, GNOME, tmux, or Claude. Every tool-specific fact lives
behind one of the four ports below.

```
                        ┌──────────────┐
                        │     TUI      │
                        └──────┬───────┘
                               │
                        ┌──────▼───────┐
         config ───────▶│     core     │  match, run-or-raise, toggle-back
                        └──────┬───────┘
                               │
         ┌──────────────┬──────┴───────┬──────────────┐
         │              │              │              │
   ┌─────▼─────┐ ┌──────▼─────┐ ┌──────▼─────┐ ┌──────▼─────┐
   │   Host    │ │ AgentProbe │ │   Remote   │ │ KeyBinder  │
   └─────┬─────┘ └──────┬─────┘ └──────┬─────┘ └──────┬─────┘
         │              │              │              │
 Runtime │ Window       claude         ssh            gnome
 ────────┼───────       opencode
 kitty   │ gnome        aider*
 tmux    │ hyprland*
 wezterm*│

  * not built
```

## Four ports

| Port | Answers | Second implementation |
|---|---|---|
| **Host** | What instances exist, how do I open one, how do I focus one? | Every adapter |
| **AgentProbe** | What is this agent doing right now? | opencode, then any other harness |
| **Remote** | What does the revier on that machine know about these projects? | ssh. Another transport would be a second one; a second revier is not needed. |
| **KeyBinder** | Which keyboard shortcuts does the desktop hold? | Another desktop's shortcut store. A shortcut store is not a `Host` (D26). |

**Host** is one interface because run-or-raise is one operation. A compositor
provides instances as OS windows; a terminal provides them as panes and
windows. The core asks the same questions of both, and the invariant it relies
on is that what a host opens, the same realization's match finds (D15). A host
holds two roles under two names:

- **Runtime** is a host plus the capabilities it declares, such as whether its
  instances are OS windows (D19). It is the only host whose instances carry
  panels, because only it can see inside a terminal.
- **WindowController** is a host under a role name. It adds nothing; the name
  exists so the wiring states which role an adapter fills.

A host can also offer optional capabilities — open a tab, type into or read a
panel, close, hide, place a window. The core detects each one, and a host
without it loses only what it would have given.

**AgentProbe** reads a panel and reports the agent's state. It receives the
whole panel rather than one signal, because which signal is trustworthy
differs per harness and changes: the Claude probe moved from the title to
`claude agents --json` and the port did not change (D57). Only a probe says
whether a program is an agent; a host says shell or tool (D119).

**Remote** is a revier on another machine, not a host: it answers with the
view the core produces, and lists, opens and focuses nothing here (D40). The
core lays its answer over the local view of the projects that live there. The
panels that show a link's agents are this machine's runtime's (D84).

**KeyBinder** reads and writes the desktop's shortcut store, which is not the
window host's tool even on one desktop.

Three things that look like ports and are not:

- **The store.** Projects are TOML files on disk. There is no second storage
  backend planned, so it is a package, not an interface.
- **Output format.** `--json` is a renderer over the same view type the TUI
  reads. A format is not a port.
- **Matching.** The core matches instances against realizations. Putting it
  behind the port would duplicate it in every adapter and let two adapters
  disagree about what a match means.

## Where the work happens

The core owns everything that is not tool-specific, and an adapter holds no
policy:

- **Preparation.** Templates are rendered once, at load, so no adapter sees a
  template and no refresh renders (D17).
- **Matching.** Each host lists its instances at a cost that does not grow with
  the project count; the core matches them against every project locally.
- **Resolution.** When both realizations of a target are available, the core
  picks: the target's own preference, otherwise the window host. A separate
  window is what a desktop user expects; one line of config overrides it.
- **Toggle-back.** The core records the project's home before it activates a
  target, and returns to it when the same key is pressed again.
- **Focus after open.** Some hosts focus what they launch and some do not, so
  the core focuses on the run path (D15). Without it the raise half of
  run-or-raise holds only by accident of the host.
- **Raising a terminal's OS window.** A terminal on Wayland cannot raise
  itself, so the core raises a runtime's OS window through the window host
  (D19).

Each process settles its own working directory before it starts anything, so
no host sets a directory for what it launches (D92). A directory set per
adapter fixed only the adapters that set one.

## Adapter selection

Each adapter reports whether it can work in the current environment. An
adapter the config names and that cannot work is an error, because the user
asked for it by name. Otherwise the first adapter in the preference list that
can work is chosen. With no window host the core runs without one, and
window-only targets report as unavailable rather than failing at the keystroke.

The host that lists what a link on another machine started here is not
selected. It is wired beside whichever runtime was, because it holds what no
runtime does, and no target resolves to it (D84).

## Package layout

Where each package sits is the [Layout table](../OVERVIEW.md#layout).

`pkg/revier` is public because an out-of-tree adapter must import the ports.
Everything else is internal, so the public surface stays small and stable. Go
convention puts an interface in the package that consumes it; this project
overrides that convention because the ports are a product feature, and adapter
authors need one obvious import path.

Every adapter is constructed explicitly in one place, not through `init()`
registration. Explicit wiring is greppable, and it makes the set of
compiled-in adapters one readable list.
