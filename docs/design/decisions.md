# Decisions

Newest last. **Accepted** means decided. **Proposed** means it is in the design
above and still needs a yes.

## 2026-09-01

### D1 — revier is a new project, not an extraction — Accepted

The `os` command in the `setup` repository stays and keeps working. revier
reimplements rather than ports, and drops what it does not need. Nothing in
`setup` is removed on revier's account.

### D2 — the product is agent monitoring, not session management — Accepted

The value is seeing what every agent across every project is doing, and
reaching the one that needs attention. Session and window handling is the
substrate that makes that view possible. This is what separates revier from
`sesh`, `tmuxinator`, and `tmux-sessionizer`, which are session managers with
no notion of what runs inside.

### D3 — Go — Accepted

Single static binary, no runtime to install, fast start. The current
implementation is bash plus python and pays a bootstrap cost on every keystroke
of a hot path; a compiled binary removes that class of problem along with the
two-language split.

### D4 — GNOME is an adapter, never core — Accepted

GNOME needs a shell extension to report windows, which makes it the hardest
target, not the reference one. It ships first because it is the daily driver.
sway and Hyprland expose real IPC and will be easier; writing one of them second
is how the abstraction gets proven.

### D5 — TOML, and no backward compatibility — Accepted

The existing `.session` format is shell `KEY=value` with known sharp edges — a
trailing comment becomes part of the value. revier does not inherit it. The
roughly ninety existing session files are converted once by a throwaway script
that is not part of the product.

### D6 — dev containers are out, SSH is deferred — Accepted

Container execution is the heaviest and least general transport and is cut.
SSH is cut from the first version, but the `Runtime` port keeps the seam, so a
remote runtime can be added later without a redesign.

### D7 — bulk execution across projects is not in the first version — Accepted

`os run` is a separate tool that happens to share the project list. Anyone who
needs it builds it on `revier list --json`.

### D8 — one TUI, not a picker and a dashboard — Proposed

The picker and the monitor are the same surface: projects sorted with the ones
needing attention first, enter opens or focuses, action keys run commands. The
current implementation splits this across two shell scripts of about 1,570
lines combined.

### D9 — window placement belongs to the compositor — Proposed

revier is a TUI. Opening it as a positioned popup on a global hotkey is a
compositor rule the user writes — a GNOME extension rule, a sway `for_window`,
a Hyprland `windowrule`. This removes popup geometry, workarea lookup, and the
`gdbus` dependency from the product entirely.

### D10 — three ports and no more — Superseded by D14

`Runtime`, `WindowController`, and `AgentProbe` each have a real second
implementation planned. The store, the output formats, and action dispatch do
not, so they stay concrete until one exists.

### D11 — a project is a set of OS windows — Superseded by D14

The terminal workspace is one window among several, not the container for
everything. An editor, a diff viewer, and a page are bound to the project the
same way, through one `WindowMatch` mechanism shared with workspace focusing.

This supersedes the earlier model in which a web view was a panel kind. A web
view is a window like any other, so `PanelKind` loses its `web` member and
`Project` gains `Apps`. The consequence is that `WindowController` grows from
"raise this window" to enumerate, find, and report focus, and that the half of
the product doing app binding depends on it.

### D12 — two tiers of binding, and three ways to acquire one — Accepted

A key is a promise about where you land, so only a declared app holds one.
Windows attached at runtime are reached through the project picker instead.

Windows become bound by revier launching them with a marker, by explicit
`revier attach`, or by claim-on-appear after a launch. The third is best effort
and exists for `xdg-open`, where the browser is already running and offers no
new process to correlate against. It needs compositor window-created events,
expressed as the optional `WindowWatcher` capability.

### D13 — browser tabs are out of scope — Accepted

A tab cannot be enumerated or activated from outside the browser. A bound page
is an app-mode window with a known class, which can. This is why the earlier
attempt to bind a normal Chrome window did not work.

### D14 — targets are logical, realizations are per host — Accepted

Supersedes D10 and D11.

A binding is to a name, not to a window. `ctrl+shift+d` means "diff viewer" in
every project that declares one. On a desktop that resolves to a Meld window
found by WM class; in tmux it resolves to `nvim -d` in a pane found by title.
The key, the name, and the promise are identical; only the host differs.

D11 modelled a bound app as a window-manager concept, which baked one
realization into the abstraction and made half the product depend on a
compositor. That was wrong. The correct abstraction is run-or-raise over a named
target, with the host supplying the realization.

Consequences:

- `Runtime` and `WindowController` collapse into one `Host` interface. They are
  the same five operations over different providers. `Runtime` adds
  `Capabilities` and is the only host whose instances carry panels.
- The port count drops from three to two: `Host` and `AgentProbe`. D10 counted
  three because it counted providers rather than operations.
- Matching, template rendering, realization preference, and toggle-back move
  into the core. An adapter holds no policy, so two adapters cannot disagree
  about what a match means.
- Degradation becomes per target rather than per feature. A target is available
  when a host that can realize it is available, and its config says which hosts
  those are.

This pays off in the first version on one machine, not only in a planned second
implementation: the ticket viewer is a terminal TUI in a kitty tab and the
editor is an IntelliJ window. Under D11 those were two code paths. Under D14
they are one mechanism with two realizations.

### D15 — two corrections the first adapter forced — Accepted

Writing the tmux host and running it headless changed two things the design had
wrong. Both are recorded here because neither was visible from the model alone.

**`Realization` gains `Name`.** `Open` must produce an instance that the same
realization's `Match` then finds, or run-or-raise opens a second instance on
every keypress. A window host satisfies this through its launch argv — a
browser `--class`, a title flag. A multiplexer does not: `tmux new-window` needs
the name given to it. `Name` is that identity, and the invariant is now stated
on `Open`.

**The core focuses after opening.** The design assumed launching a target left
it focused. tmux does; a window manager need not. The L4 test caught it as a
raise that returned the wrong instance. `Go` now focuses explicitly on the run
path, so it always leaves the target focused regardless of host.

### D16 — two corrections the first end-to-end run forced — Accepted

Both were found by driving the CLI by hand against real tmux and a real GNOME
session, and neither was visible to the fake-host layer.

**Toggle-back requires the focus authority.** `focusedOn` compared a ref's id
against whichever host answered `Focused`, but ids are host-scoped: a GNOME
window id and a tmux pane id are unrelated numbers. With a window host present,
runtime targets therefore never toggled — or would have toggled on a numeric
coincidence. A ref now counts only when its own host is the focus authority.

The cost is real and accepted: toggle-back does not fire for runtime targets on
a machine that has a window host. The alternative — trusting a runtime's "current
pane" as global focus — sends the user home when they asked to go somewhere,
which is worse than a key that simply goes there. Recovering it needs a mapping
from a runtime instance to the OS window containing it, which the ports do not
carry.

*The mapping is D19's, and the cost is recovered for a runtime that claims it.*

**Agents are discovered in every target, not only home.** The survey probed the
home instance alone, so an agent given a target of its own was invisible — which
defeats the product's purpose. Every matched instance is now probed, deduplicated
by ref, because one instance can back two targets.

## 2026-09-02

### D17 — projects are prepared at load — Accepted

`config.Load` returns projects with every template rendered and every match
compiled. `Survey` and `Go` take that form and derive nothing per call.

The refresh path used to re-render ninety projects and re-compile four hundred
and fifty patterns every second, from inputs that had not changed since load.
The measured cost was garbage, not time, and would not have been noticed. The
reason to move the work is where errors surface: a template naming a missing
key passed validation, loaded, and either failed at the keystroke or was
swallowed by the survey as a project with no available targets. Both were
breaches of the rule this design already applies to every other validation
failure, that a bad file is refused at load, naming the file.

The consequence for a long-lived surface: the TUI holds prepared projects for
its lifetime, so a project file edited while it runs is read on restart, not on
the next refresh.

### D18 — a realization starts in the project directory, and a home target is its panels — Accepted

`Realization` gains `Dir`. The core fills it with the project path when the
config leaves it empty, and the path itself is tilde-expanded once at load, so
no host receives a literal `~`. A runtime realization with panels needs no
`launch`: the panels are what is launched, as the Level 1 example in
[extending.md](extending.md) always showed and the validator until now refused.

Without this the first kitty workspace opened in whatever directory the kitty
process happened to have, which is the one thing a session file never got
wrong.

### D19 — a runtime that owns OS windows says so, and the window host raises them — Accepted

Amends D16, whose cost this recovers.

`Capabilities` gains `OSWindows`. A runtime that reports it promises that
every instance is an OS window and carries the title a window host reports for
the same window. The core then does two things it could not do before:

- **Raise.** After focusing a runtime instance it finds the OS window by title
  in the window host's listing and activates it there. kitty needs this: under
  GNOME on Wayland `kitten @ focus-window` moves focus inside kitty and the
  compositor refuses to raise the window, because the request did not come
  from user input. Verified on this machine before the design was written.
- **Toggle back.** A runtime target's second press is judged by whether its OS
  window holds focus, asked of the window host, which stays the only focus
  authority. The false positive D16 refused stays refused: the OS window, not
  the pane, must be focused.

Two mappings were considered and rejected. PID lineage - walk from the pane's
process to the terminal's - fails both ways: every OS window of one kitty
process shares the kitty pid, so lineage cannot tell them apart, and a tmux
pane's ancestry ends at the daemonised server, never reaching a terminal.
Trusting the runtime's own focus report was D16's refusal and stays refused,
although kitty could in fact answer it; one authority is simpler than two that
must agree.

kitty gives each OS window its identity through `--os-window-name`, read back
as `wm_name`, and sets the OS window title to the same value. A window kitty
opened from a session file has neither and is invisible to revier, which is
the accepted cost of not parsing what `kitten @ ls` does not report. tmux
cannot claim `OSWindows`, so for it D16's cost stands.
