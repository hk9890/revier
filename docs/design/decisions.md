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

### D9 — window placement belongs to the compositor — Narrowed by D24

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


### D20 — claim-on-appear is the TUI's, and claims nothing before the wrong thing — Accepted

The launch that starts the clock and the window that ends it are seen by
different processes: `revier go` exits at once, and the window appears later.
So the launch is a record in state, `Launch{Project, At}`, written by whatever
launched, and the claim is made by the TUI, which is the one process alive to
see the window arrive. It re-reads state on every refresh for that reason.

Two paths, one policy. A window host that implements `WindowWatcher` - sway,
the first - delivers the window as an event and the claim is immediate. A host
that does not - GNOME, through `wctl` - is diffed between successive surveys,
and the claim takes up to two refresh intervals. The survey therefore returns
the window listing it was built from (`core.Report`) rather than the TUI
asking for a second one, which keeps a refresh at one call per host.

The bounds are the decision. A wrong claim binds an unrelated window to a
project and is only found later, when a key goes somewhere surprising, so:
five seconds after the launch and no more; never a window a declared target of
any project matches, since that window is reached by its key already; and
when two candidate windows appear at once, neither, because the launch does
not say which. `revier list` never claims: a one-shot process has no previous
listing to diff and no window to wait for.

### D21 — a key binds to the instance it landed on, and a launch waits for its window — Accepted

Amends D20, whose claim rule now covers actions only.

A rule - class and title - is how a target is found the first time, and how it
is found again after revier restarts. It is not how a key stays on its window.
Titles move: IntelliJ opens with no project in its title and gains one seconds
later, a browser window is "New Tab" before it is the page. Matching on every
press meant the key was weakest right after the launch, and D20's claim rule
attached the target's own window as a stray in that gap.

So every successful press pins its target to the instance id it landed on,
in state, beside the attachments and pruned with them. The next press finds
the target by id and consults the rule only when the binding is gone. A
detached launch - a window host started a process and could not name the
window - waits for the window in the launching process, up to thirty seconds
as the shell implementation did, and binds the first new window the target's
rule accepts by class alone, which is right from the first frame. A window the
full rule matches is taken at once; two class candidates at once bind nothing.
Bind raises the window through the window host, which also settles the
question of a compositor that opens a new window behind.

The launch is recorded before the wait, so a second press during it reports
that the target is coming up rather than launching a second copy, and so the
TUI can bind a window that took longer than the wait - a cold editor start -
on a later refresh, within a minute. Claim-on-appear keeps only the case D12
named: an action, `xdg-open` among them, whose window no target declares.

The pid was considered as a second witness and left out: a window host reports
it, and a fresh process is the launched one or its child, but the single-
instance applications this exists for - browsers, an editor already running -
hand the new window to a process that was already there. Class and time do
the same work for both kinds.


### D22 — a window with no name of its own is identified by the window manager — Accepted

Amends D19, whose accepted cost this removes.

D19 gave a runtime that owns OS windows the job of carrying the title a window
host reports for the same window, and named the case it could not cover: a
window kitty opened from a session file has no `--os-window-name`, so it
carries kitty's default instance name and is invisible to revier. On this
machine that was every session the shell tool had opened - five of them, with
`revier list` reporting nothing running while all five were on screen.

The window manager sees what the runtime cannot. `kitten @ ls` reports
`wm_name` of `kitty`; `wctl list --json` reports the same windows with the
titles `session:revier`, `session:setup` and the rest, because the session
template sets the OS window title. Both hosts report the process.

So the runtime says only that a window has no identity - the kitty adapter
reports an empty title where it would report the default name, which is a fact
about kitty and not a policy - and the core pairs it with a window of the same
process and takes that window's title. Neither host can do this alone, which
is what makes it policy.

The pairing is refused unless the process owns exactly one window on each
side. D19 rejected the process id for pairing a pane to a window because every
OS window of one kitty process shares its pid; that objection is precisely
this refusal. An ambiguous process is left unidentified rather than guessed
at, because a borrowed title sends the next keypress to the wrong window,
which is worse than the window staying invisible.

The panels are unaffected: they still come from the runtime, so a session
found this way reports its agents like any other. tmux cannot claim
`OSWindows`, so nothing about a pane changes.


### D23 — the list component ranks and filters; the scrolling is ours — Accepted

Amends D8's implementation, not its intent.

The surface was built on `bubbles/list`, which is paginated: the cursor walks
to the bottom of a page and the next press replaces every row on the screen
and puts the cursor back at the top. Measured at 120 by 24 with eighty-nine
projects, the tenth press of the down key moved the view from `proj-01..10` to
`proj-11..20`. With ninety projects reached by a held-down arrow key that is
the normal way through the list, so the rows flicker through unrelated names
on the way to a neighbour. The shell picker it replaces scrolls one line.

Continuous scrolling cannot be asked of the component: a page is a fixed
window, and there is no way to express a window that starts one row lower.

So the component keeps the work it is good at - fuzzy ranking by the algorithm
fzf uses, the filter, and which item is selected - and is given room for every
row it holds, so it renders a single page. A viewport clips that page and
scrolls it by the least that keeps the selected row on screen.

This takes back the scroll window that the move to the component handed over,
and it is the second half of that trade rather than a reversal of it: the
alternative was to hand-roll the filter and the ranking as well.

The cost is that the delegate renders every row the filter leaves rather than
one screen of them. Eighty-nine projects is a hundred and seventy-eight lines
per frame, which is a rounding error next to the survey that produced them.


### D24 — the compositor places what your script starts; revier places what revier starts — Accepted

Narrows D9, which is right about the popup and wrong about a workspace.

D9 said placement is a compositor rule the user writes, and removed geometry
from the product. That holds for the TUI popup: the user launches it from
`contrib/gnome/revier-popup`, which is a script they own, and a `wctl place`
line in it is exactly the rule D9 describes.

It does not hold for a window revier launched. No compositor rule can name
*the window this launch just produced*: a rule matches a class or a title, and
every session window of every project shares both. The shell tool this
replaces does not try - `place_new_session_window` waits for the new window by
its exact title and then places it, and the comment there records why a poll
is not enough.

So a realization may declare `place`, four tokens - x, y, width, height - in
the window host's own vocabulary of pixels and workarea-relative words. The
core applies it once, after a launch, through an optional `WindowPlacer`
capability on the window host. A host that does not implement it ignores every
placement, which is sway today.

Three limits keep this from growing into a window manager:

- It applies to a launch and never to a raise. A window the user has moved
  stays where they put it.
- A refusal is not an error. A window pinned by maximize or tiling keeps its
  geometry, and the keypress that opened it succeeded either way; failing the
  key over a rejected geometry would trade a cosmetic problem for a broken one.
- Nothing is placed that did not ask. A project with no `place` behaves as it
  did before.

For a runtime target the window is found the way D19 finds it, by the title
the runtime and the window host agree on, with a short wait: a terminal
reports its window before the compositor has mapped it, and a geometry request
made too early is overwritten by the compositor's own placement.


### D25 — a binding is re-checked against the class before it is trusted — Accepted

Extends D21, which said how a binding is made and not how far it is believed.

Pruning drops a binding whose instance the last survey did not see. What it
cannot drop is a binding that is alive and wrong: a window manager hands window
ids back out, so the id a target was bound to can return as an unrelated
window, and the next press raises that. Nothing about the failure is visible -
the wrong window comes forward, and the user has no way to tell a reused id
from a mistake of their own.

So `locate` re-checks a remembered instance against the class the target
declares before returning it, and falls back to the rule when it no longer
holds. This is the class D21 already trusts: bind-on-launch takes the first new
window "the target's rule accepts by class alone, which is right from the first
frame".

The title is never re-checked. Surviving a title the rule no longer matches is
the whole reason a binding exists - D21's own example is IntelliJ opening
without the project in its title - and re-checking it would undo D21 entirely.

A target whose rule constrains no class has nothing to check and is trusted as
before, and so is a binding to a runtime instance: a tmux pane id and a kitty
window id are not handed back out.

The manual half of the original question - a key that forgets the instance
under the cursor - is not built. It covers a wrong `attach` by hand, which has
not happened yet; the self-check covers the failure that cannot be seen, which
is the one worth spending a surface on.


## 2026-09-06

### D26 — the desktop's keyboard shortcuts are a port of their own — Accepted

revier and the shell tool it replaces both want the same four desktop keys.
Neither can be switched to without knowing who holds a key now, and today
`contrib/gnome/install-keybindings.sh` appends its entries beside whatever is
already there, so a press fires both systems.

Reading the shortcuts is not the window host's job. `Host` provides instances
of targets and takes part in run-or-raise; a shortcut store provides neither,
and on GNOME the two are different tools - `wctl` for windows, `gsettings` and
`dconf` for keys. Folding the reader into the window host would also make the
answer to "which key is bound" depend on a Shell extension being up, which is
when a user is most likely to ask.

So `KeyBinder` is its own port, with `List` and nothing else. A machine with no
desktop leaves it nil, which is a sentinel and not a failure.

Two things stay in the core rather than in the adapter. One chord has three
spellings in this system - `ctrl-shift-u` in a project file, `ctrl+shift+u`
from bubbletea, `<Shift><Control>u` from GNOME - so there is one canonical form
and a parser that accepts all three; comparing spellings is a decision, not a
GNOME fact. The other is what revier wants: the union of the key each target
declares, plus the chord that opens revier itself, which is `[ui] trigger_key`
because no project could own the key that opens the surface rather than a
target.

`revier keys status` reads and prints. Claiming a chord is a separate
operation, and a status command an agent may run against a live desktop is one
that cannot change it: the adapter names the read subcommands it may use and
refuses everything else, so `gsettings set` and `dconf write` are unreachable
rather than merely unused.

## 2026-09-07

### D27 — revier adds keys; it takes one only when told to — Narrowed by D29

Extends D26, which read the desktop's shortcuts and stopped there.

Two systems want the same four keys. `revier keys install` is how revier claims
them, and `os init` in the `setup` repository is how the shell session tool
claims them back. Neither knows the other exists. Whichever ran last holds the
keys, and that is the switch between the two systems: no environment variable
to read at a keypress, no dispatcher script in front of the four, no third
place where the choice lives.

The two never fight over storage. revier writes entries it names itself -
`revier-picker`, `revier-home` - while `os init` allocates GNOME's numbered
slots and skips any that already holds a definition.

Install adds. A key that something else holds is left exactly as it is, and
reported. `--force` is what takes one, and it is per key rather than all or
nothing: a machine where three keys are free and one is a desktop default
should get three keys, and `revier keys status` shows the half state
afterwards.

**Force destroys nothing.** Another program's shortcut is taken out of the list
GNOME acts on and keeps every word of itself, so whatever wrote it puts it
back. A desktop default is a setting rather than an entry, so it is emptied,
and the one `gsettings reset` line that returns it is printed beside the key it
was cleared for. This is why revier stores no copy of what it displaced: a
backup would be a third way back, competing with `os init` and with the desktop
itself, and it would age.

Uninstall is the mirror and nothing more. It removes what revier wrote,
including a shortcut left on a key nothing asks for any more, and restores
nothing: what to put back is a question only the tool that was displaced can
answer.

A disagreement inside revier's own configuration - two targets on one key, or a
target on the key that opens revier - is refused rather than installed. The
report names it and carries on, because a diagnostic that fails when something
is wrong is no diagnostic; installing either spelling of it would be a guess.

Writing is `KeyWriter`, a separate interface satisfied by a separate type in
the adapter, so the reader `revier keys status` is handed stays unable to write
whatever is added beside it. Its three verbs are separate because they are not
reversible in the same way: creating, switching off, and removing. Which one a
shortcut deserves is policy - revier removes only what it wrote.

## 2026-09-11

### D28 — Enter on a project opens its home — Accepted

The TUI was two levels, and Enter on a project only moved to the second: its
list of targets. Reaching the project took a second Enter on the home row, which
was already the cursor's default. A search for a project is almost always a
search to get to it, so the list was the common path paid for by the rare one.

Enter on a project is now `revier go home` for that project: run-or-raise, and
the surface stays where it was. Tab opens the target list. It is not the right
arrow, because left and right move the cursor in the query. A project with no
home target has nothing to open, so Enter shows its list instead of doing
nothing.

### D29 — `os init` is not a way back on its own — Accepted

Narrows D27, which is right about what revier does and wrong about the switch.

D27 said whichever of `revier keys install` and `os init` ran last holds the
four keys. It was tested on the author's desktop and it does not hold for
`os init`. After `revier keys install --force`, the shell tool's four shortcuts
are switched off. `os init` then finds their slots holding a definition,
writes four new copies into free slots and switches those on, and leaves
revier's four switched on beside them. Every key fires both tools. Each round
trip leaves four more copies behind.

revier's half is unchanged and was right: install with `--force` switches off
whatever else is live on a key, copies included, and never touches its own.
What changes is the way back. It takes both commands, in either order:
`revier keys uninstall` to stop revier firing, and `os init` to make the shell
tool fire. Neither alone is enough - `os init` alone leaves both firing, and
uninstall alone leaves nothing on the four keys.

A single-command way back is `os init`'s to provide, in the `setup`
repository: switch its own shortcuts back on rather than copying them, and
switch off anything else on its four keys, as `--force` does here. revier does
not reach into that tool to do it for it.

### D30 — revier writes project files, and clones a missing checkout — Accepted

A project file travels between machines; the checkout it names does not. Half
of the converted projects point at a directory that is not on this machine, so
a project records `git_url`, validated at load by the shell tool's rules, and
an explicit open - `revier open`, or Enter in the TUI - clones it first. The
clone is the only write revier makes outside its own files. It runs in the
foreground with git's output on screen, and never from a survey. With no
`git_url`, `revier open` fails and names the field.

`revier new` writes a project from a built-in template, with the origin
remote as `git_url`, and `revier open <unknown>` does the same before it
opens. The template carries no keys: a key means the same target in every
project, so one written here either repeats the user's choice or conflicts
with it. It refuses a directory that is already a project and the home
directory, which would own every directory under it that no project claims.

The TUI edits with alt+e and deletes with alt+d. Every other free key is
taken: a bare letter filters, del and ctrl+e edit the query, and the ctrl
chords are where target keys and actions live. Delete asks, and refuses while
any target of the project is running, as the picker refused a running session.
The file the TUI handed to the editor, or removed, is applied at once; D17
still holds for a file changed anywhere else. Renaming is out: the name is in
the file name, the window titles and the bindings in state.
