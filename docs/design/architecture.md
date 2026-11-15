# Architecture

## Shape

Ports and adapters. The core holds the domain and the orchestration and knows
nothing about kitty, hc, GNOME, or Claude. Every tool-specific fact lives
behind one of the ports below.

```
                        ┌──────────────┐
                        │     TUI      │
                        └──────┬───────┘
                               │
                        ┌──────▼───────┐
         config ───────▶│     core     │  match, run-or-raise, toggle-back
                        └──────┬───────┘
                               │
  ┌─────────┬─────────┬────────┴───┬────────────┬──────────┬──────────┐
  │         │         │            │            │          │          │
  Runtime   View      Window       AgentProbe   Remote     KeyBinder  TermKeys
  │         │         │            │            │          │          │
  kitty     kitty     gnome        claude       ssh        gnome      kitty
  hc                  hyprland*    opencode
                                   aider*

  * not built
```

## Ports

| Port | Answers | Second implementation |
|---|---|---|
| **Runtime** | What runs, in which instances and panels; how do I start one, type into it, read it, close it? | hc, beside kitty |
| **View** | Which window and tab shows an instance; how do I show one, focus one, and which has the focus? | Another terminal that can list and focus its tabs |
| **Window host** | What OS windows exist, how do I open one, how do I raise one? | Another compositor |
| **AgentProbe** | What is this agent doing right now? | opencode, then any other harness |
| **Remote** | What does the revier on that machine know about these projects? | ssh. Another transport would be a second one; a second revier is not needed. |
| **KeyBinder** | Which keyboard shortcuts does the desktop hold? | Another desktop's shortcut store. A shortcut store is not a **Window host** (D26). |
| **TermKeys** | Which keys does the terminal's configuration hold for revier? | Another terminal's configuration. |

**Runtime** and **View** are the two halves of a terminal: one runs programs,
the other draws them (D176). kitty does both, and hc only the first, so each
half is a port. Run-or-raise stays one operation of the core: it asks the
runtime what runs, starts what is missing there, has the view show it, and
focuses it. The invariant it relies on is that what the runtime opens, the
same realization's match finds (D15).

- **Runtime** lists its instances, each with its panels and each panel's
  process, command, directory and marks; starts terminals in a new or a live
  instance; and types into, reads and closes a panel or an instance. It is the
  only port whose instances carry panels, because only it can see inside a
  terminal. A tab is its fact, kept in the marks of the panels, so every view
  draws the same tabs (D179).
- **View** opens a window for an instance, draws its tabs, focuses an
  instance, a tab or a panel, and reports which has the focus. It maps the
  panel id a terminal key passes to the runtime panel behind it, and reports
  which instances it shows. Every call takes and returns the runtime's refs
  and panel ids, so the core never holds an id of the view's own (D16). It
  says whether its windows are OS windows the window host lists (D177). It
  closes windows, never programs (D181).
- **Window host** lists, opens and raises OS windows, and reports the one with
  the focus. The terminal's split leaves it as it was.

Each port can also offer optional capabilities: a runtime may type into, read
or close; a view may focus a tab; a window host may hide or place a window.
The core detects each one, and an adapter without it loses only what it would
have given.

### Setups

A runtime is shown by the view that follows it, and three setups work (D178):

| Runtime | View | What the setup gives |
|---|---|---|
| kitty | kitty | The same kitty processes run and draw, so a kitty window id names a runtime panel directly, and nothing more is installed |
| hc | kitty | An agent outlives the window that shows it; tabs, focus and the terminal keys are kitty's |
| hc | none | Over ssh or with no display: every panel runs and is listed, read and typed into, and a terminal reaches one by attaching to it; no tab is drawn and nothing is focused |

Under hc, the kitty view opens one kitty OS window per instance, one kitty tab
per tab, and one kitty window per panel of a tab, each running hc's attach for
its terminal (D180). Each kitty window carries the hc address it attaches, so
a kitty window id maps to its terminal and a terminal to its window. A view's
window is never held open after its program ends: hc removes a terminal whose
program exited, so the window ends with it, and a tab still ends with its last
panel (D94).

What a setup without the kitty runtime does not read:

- **The user's own kitty windows.** Under hc revier reads no kitty but the
  view, so a terminal attached by hand there is its window alone (D95, D178).
- **A plain tab.** Outside a workspace a terminal key opens kitty's own shell,
  since an hc terminal no instance holds would list as an instance of its own
  (D183).

## The other ports

**AgentProbe** reads a panel and reports the agent's state. It receives the
whole panel rather than one signal, because which signal is trustworthy
differs per harness and changes: the Claude probe moved from the title to
Claude Code's own agent listing and the port did not change (D57). Only a probe says
whether a program is an agent; a runtime says shell or tool (D119).

**Remote** is a revier on another machine, not a runtime: it answers with the
survey the core produces, and lists, opens and focuses nothing here (D40). The
core lays its answer over the local survey of the projects that live there.
The panels that show a link's agents are this machine's runtime's (D84).

**KeyBinder** reads and writes the desktop's shortcut store, which is not the
window host's tool even on one desktop.

**TermKeys** reads and writes the terminal's keys: a file revier owns whole,
and one include of it in the terminal's own configuration (D147). It is not
**KeyBinder**: that store is shared with other programs, and taking a key there
displaces one of theirs; a terminal key opens a tab beside the window it was
pressed in. It is not the **View** either: the view is the terminal's remote
control, this is its configuration file.

Three things that look like ports and are not:

- **The store.** Projects are TOML files on disk. There is no second storage
  backend planned, so it is a package, not an interface.
- **Output format.** JSON output is a renderer over the same survey the TUI
  reads. A format is not a port.
- **Matching.** The core matches instances against realizations. Putting it
  behind the port would duplicate it in every adapter and let two adapters
  disagree about what a match means.

## Where the work happens

The core owns everything that is not tool-specific, and an adapter holds no
policy:

- **Preparation.** Templates are rendered once, at load, so no adapter sees a
  template and no refresh renders (D17).
- **Matching.** The runtime lists its instances and the window host its
  windows, each at a cost that does not grow with the project count; the core
  matches them against every project locally. A runtime target matches what
  the runtime reports of an instance, since a title the view gives a window is
  no fact of the runtime's (D15).
- **Resolution.** When both realizations of a target are available, the core
  picks: the target's own preference, otherwise the window host. A separate
  window is what a desktop user expects; one line of config overrides it.
- **Toggle-back.** A press on a target that already has the focus goes to the
  project's home instead, so a key is a round trip and not a one-way jump. The
  view, or the window host for an OS window, says what has the focus (D16).
- **Command targets.** The core runs a command target's command through a
  runner the wiring supplies, and focuses nothing: the command does. The
  command signals nothing, so the core judges the toggle on the run it
  recorded, not on an instance (D166).
- **Focus after open.** The runtime starts and the view shows, and some views
  focus what they show and some do not, so the core focuses on the run path
  (D15). Without it the raise half of run-or-raise holds only by accident of
  the view.
- **Raising a terminal's OS window.** A terminal on Wayland cannot raise
  itself, so the core raises a view's OS window through the window host
  (D177).
- **A view that is gone.** Under hc the runtime outlives the view, so
  run-or-raise on a live instance that no view shows opens a view over its
  terminals and starts nothing, as a restore that finds it running does
  (D181).
- **The popup's second press.** The popup's surface listens on a socket of its
  own, and the press finds the popup through the window host, by its class.
  No runtime is asked, so the popup works under every runtime (D182).

Each process settles its own working directory before it starts anything, so
no runtime sets a directory for what it launches (D92). A directory set per
adapter fixed only the adapters that set one.

## Adapter selection

Each adapter reports whether it can work in the current environment. An
adapter the config names and that cannot work is an error, because the user
asked for it by name. Otherwise the first adapter in the preference list that
can work is chosen. A prototype adapter stays out of that list, so it runs
only where the config names it (D174). The view is selected after the
runtime, by a list of its own; left out, it is the runtime's own view, as
under kitty, then kitty where it can show the runtime, else none, as under
hc (D178, D185). With no window host the core runs without one, and
window-only targets report as unavailable rather than failing at the
keystroke.

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

Every adapter is constructed explicitly in one place, not registered when
its package loads. Explicit wiring is greppable, and it makes the set of
compiled-in adapters one readable list.
