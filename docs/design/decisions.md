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

### D6 — dev containers are out, SSH is deferred — Superseded by D40

Container execution is the heaviest and least general transport and is cut.
SSH is cut from the first version, but the `Runtime` port keeps the seam, so a
remote runtime can be added later without a redesign.

### D7 — bulk execution across projects is not in the first version — Superseded by D32

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

### D26 — the desktop's keyboard shortcuts are a port of their own — Narrowed by D35

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

### D27 — revier adds keys; it takes one only when told to — Narrowed by D29 and D34

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

### D28 — Enter on a project opens its home — Accepted; the target list narrowed by D42

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

### D31 — a runtime can type into a panel; whether it may is the core's — Accepted

Anything that scripts the agents was still on `os agent`, whose `prompt` and
`wait` had no revier equivalent. `revier agent wait` and `revier agent prompt`
are that equivalent. `revier list --json` already covered `os agent list`, so
there is no third listing.

Waiting needs nothing new: it is the survey's probe path, read again every
half second until the status matches. Typing does. It is a fact about one
tool - `tmux send-keys -l`, `kitten @ send-text --stdin` - so it is a runtime's,
and it is an optional capability, `PanelWriter`, detected by type assertion as
`WindowPlacer` is. Not every runtime can type into a pane, and one that cannot
still does everything else; a method on `Runtime` would have made every
out-of-tree runtime implement it.

`SendText` delivers text as it is and adds nothing. The Enter that submits is
a `"\r"` the core sends after it, so the runtime knows nothing of a prompt, a
submit, or a dialog. The instance ref travels with the panel id because a kitty
window id means nothing without the process that holds it.

Every refusal is the core's:

- **Not an agent.** No probe claims the panel, or the runtime sees a shell in
  its foreground. The second is not redundant: the kitty marker the Claude probe
  reads outlives Claude in a `--hold` window, and text typed into the shell left
  behind runs as a command. The shell tool guarded the same case by the
  foreground process.
- **Attention, or unknown.** An agent waiting for the human shows a question or
  a permission dialog, where the Enter picks the highlighted option. An unknown
  state may be showing one too.
- **More than one line.** A newline submits early and sends the rest as further
  prompts.

Prompt returns once an idle agent has left idle. A runtime reports that text
was delivered, never that it was read, so this is the only confirmation there
is, and it is what makes `prompt && wait --until stopped` wait for the turn
that was asked for rather than match the rest before it. An agent still idle
after three seconds is a warning, not a failure, as it was in the shell tool.

An agent is addressed as `<project>`, `<project>:<target>`, or
`<project>:<panel>`. The target is revier's word for what `os` called a
session window; the panel id is there for a target holding two agents, which
the first two forms refuse as ambiguous. A wait that times out exits 2, the
status `os agent wait` used, so a script keeps its branch.

The cost is on tmux: its panes carry no hook variable, so the Claude probe
never reports attention there, and the attention refusal fires on tmux only for
a probe that reads it some other way.

### D32 — a command in every project is revier's after all — Accepted

Supersedes D7.

D7 left bulk execution to a separate tool built on `revier list --json`. Nobody
built it, and the fleet-wide chores - pull every repository, trust every
workspace, list the issues of every project that has a tracker - still go
through `os run`. That keeps the shell tool alive after the picker has moved
over. The project list is revier's, so the loop over it is too.

`revier each -- <cmd>` runs one argv in the directory of every project. It does
not take the `run` verb, which is one configured action in one project. Which
projects it reaches is a decision, so it is the core's: a project whose
directory is missing is skipped and named; a project on a directory an earlier
one already has is skipped as a duplicate, because two project files on one
checkout must not run a command that is not idempotent twice there; and
`--filter` is a shell test run in each remaining directory, so the selection is
data rather than a list.

Projects run one at a time. The commands this exists for write shared state -
one trust file in the home directory, one ssh agent, one credential helper - and
ninety of them side by side lose writes or prompt at once. Output does not go to
the terminal: each project's goes to its own file under the state root, beside a
summary saved after every project, so an interrupted run still says how far it
got. `revier each log` reads both back. A run in which any project failed exits
5, not 1, so a script can tell a failed project from a revier that could not
start.

### D33 — the TUI takes the mouse wheel, and plain drag-select with it — Accepted

The picker being replaced leaves fzf's mouse on, and the popup is a window on a
desktop with a pointer in it. With no mouse at all the wheel did nothing, which
reads as a dead window rather than a choice.

The wheel over the detail pane scrolls the pane, the one place with more to
read than the screen holds; anywhere else it moves the selection a row a notch,
as the arrow keys do. The keyboard is unchanged.

A terminal reports the wheel only to a program that asks for mouse events, and
asking takes plain drag-to-select from the terminal for as long as the surface
runs. Shift-drag still selects in kitty and most other terminals. Nothing on
the surface is text a user needs to copy - a path is in the project file, and
an activity line is the agent's own - so the wheel is worth more than the drag.

Clicking a row does nothing yet. It is the natural next step and needs no
further trade: the events it would use are already reported.

### D34 — `--force` takes one key out of a desktop default, not the whole setting — Accepted

Narrows D27, which said a desktop default "is emptied".

A GNOME default is one setting that can hold several keys: `close` is
`['<Alt>F4', '<Super>q']` on many machines. Emptying it to take Super+Q took
Alt+F4 as well, which nobody asked for and the printed undo line only
restored by resetting the whole setting. `--force` now removes the one key
revier needs and leaves the others, and the `gsettings reset` line beside the
key still puts the default back as it was.

### D35 — a key the terminal cannot deliver works on the desktop only — Accepted

Narrows D26, which listed `ctrl+shift+u` as bubbletea's spelling of a chord.

A terminal without the kitty keyboard protocol never sends it. Ctrl+Shift+U
arrives as Ctrl+U, Ctrl+I as Tab, Ctrl+M as Enter, and Super not at all, so a
target key compared against the chord it declares never fired in the TUI.

The TUI now reads every press as a canonical chord and binds a target key
under what the terminal actually sends - unless that press already means
something in the TUI: its own keys, a query editing key, an action, or another
target that declares it directly. Then the key works on the desktop only, and
the footer does not offer it. On the owner's keys that makes Ctrl+Shift+O work
in the TUI as Ctrl+O, while Ctrl+Shift+U (the query's clear-line) and
Ctrl+Shift+I (Tab) stay desktop keys. An action has no desktop half, so an
action key the terminal cannot send, or sends as another key, is refused at
load.

### D36 — a click selects a row, and a second click on it opens the row — Accepted

Completes D33, which left the click for later.

The events were already reported, and a pointer over a list that only the
wheel moves reads as a list that is broken. A click on a row moves the
selection to it, as fzf's does; two clicks on one row within the desktop's
double-click time do what Enter does there — open the project, or run the
target. Two clicks on different rows are two choices. A click on the pane,
the header or the footer moves nothing, so a pointer parked on the surface
cannot change what the keys act on.

### D37 — the frame stops growing at the width its content needs — Superseded by D38

On a wide terminal the list took every column the pane did not, so a state
sat three hundred columns from the name it belonged to, and the pane an
arm's length from the row it described. The popup being replaced was half
the screen wide, centred (os-fzf-popup.sh: `POPUP_PLACE_WIDTH_PERCENT=50`,
anchored center), which is why it never had the problem.

The list is capped at a hundred columns — a name, a state with its activity
and a path fit — and the pane at ninety, so the frame stops at their sum and
sits centred in whatever is left. Height is not capped: rows are what a list
of ninety projects is short of.

### D38 — the rows are a grid, and the frame takes the whole terminal — Accepted; border removed by D51

Supersedes D37, which capped and centred the frame.

The cap treated a symptom. The state sat a screen away from its name because
it was aligned to the row's right edge, and any wide list moves that edge. A
capped frame left a third of a wide terminal empty on each side, which is
space the surface was asked to use.

The rows are a grid instead. The name column is as wide as the widest name on
the list, within a cap of thirty-two so one long name does not move every
state, and the state starts right after it. The activity flows from the state
to the list's edge: it is the one part of a row that gets better with width.
On the second line, what is open or why nothing can be follows the path, so
the line reads as one sentence about the checkout. The pane keeps half the
width up to a hundred and twenty columns, where a Git URL and a tree row fit
whole; the list takes the rest, at any width.

### D39 — the list stops at its table's width, and the pane takes the rest — Accepted

Narrows D38: the grid stays, the split changes.

The list is a table of two columns on both lines of a row: the project - its
name, and its path under it - and the agent - its state and activity, and
under them what is open or why nothing can be. A table has a width its
content needs: a name up to thirty-two, a path, the state's glyph and word,
and fifty of the activity. That is a hundred and ten columns. Past it a
wider list was blank on the right, and at the owner's standard size, a popup
half a wide monitor, most of the list was. The list stops there, and every
further column goes to the pane, which is where a wide terminal has
something to show.

The agent column has a fixed width, up to forty-eight, and sits at the
right edge with its text left-aligned inside it, so the states line up in
one column whatever the names and paths beside them do. Nothing from the
project column crosses into it: a path is cut in the middle to fit. A
checkout that is not here says "not cloned" or "not on this machine" under
the state, and what Enter does about it is the pane's to say.

The project column is what the list is for, so it gives way last. As room
goes the agent column gives way in steps, rather than being cut mid-word:
first the activity, which the pane shows whole; then the words, leaving the
glyph, which is why every set's glyphs are told apart on their own; then the
glyph; then the column. Only then is a name or a path cut. With the column
gone, the sort and the header's counts still say who needs you.

The pane fills its height: the snapshot takes the rows the other sections
leave, where it stopped at twenty lines over twenty blank rows. At a hundred
and thirty columns the pane lays the snapshot beside the facts, each filling
the height, so neither waits under the other. A third column, the selected
agent's live output, is the next use of a wide pane and is a decision of its
own.

### D40 — a remote project is surveyed and driven by the revier on its host — Partly superseded by D41

Supersedes the SSH half of D6. Containers stay out.

A project can live on another machine: its file says `host = "buildbox"`,
an ssh destination. Nothing about that machine's terminal, tmux or agents is
read from here. The revier installed there is asked, over ssh, what it knows
- `revier list --json <names>`, one call per host for all its projects, every
host at once - and its answer is laid over the local view: the agents and
whether the checkout is there are the host's word. `revier agent prompt`,
`revier agent wait` and `revier run` on such a project run the same command
there, an action with this terminal over `ssh -t`, so an action is defined
in the host's configuration and runs in the checkout it has. The list shows
the project as `name@host`, with a server glyph in the icon column: the
name is the same on both machines, because it is what every command sends
the host, and the host is what tells two of them apart.

The alternative, an ssh-wrapping runtime adapter, was rejected. The runtime
side is not one command: it is `tmux list-panes`, the process tree the
Claude probe reads, the titles, `send-keys`. Wrapping each in ssh is a second
implementation of every adapter, and the core holds one runtime, so a second
one per host would have reached into resolution and the snapshot. The
remote's `list --json` is already the view both renderers read, so a port
that asks another revier costs one interface, `Remote`, and no change to the
hosts, the probes, resolution or state.

What is here is the window that reaches the project. The home target's
runtime realization is a local pane whose command is
`ssh -t <host> revier open <name> --attach`; the revier there opens the
workspace in its tmux and puts the pane on it, through a new optional runtime
capability, `Attacher`, that hands back the argv for it. Focus, toggle-back,
bindings and claims see an ordinary local instance. So `Running` and `Home`
stay local: an agent working on the host with no pane onto it here is an
agent in a stopped project, and Enter opens the pane. A host that does not
answer marks its projects unreachable, with the failure, and fails nothing
else: the survey renders, and a down machine is a row.

The file is on both machines, as D30 already assumed, and its path is the
host's to expand: a remote project's `~` is kept as written. The host reads
the same `host = "buildbox"`, so it has to know that it is buildbox, or it
asks itself over ssh, once per level, until the deadline. Its `config.toml`
says so, `host = "buildbox"`, and a project naming this machine loads as a
local one. A machine's hostname was rejected for it: an ssh alias is rarely
the hostname, and a mismatch reproduces the recursion silently. Leaving
`host` out of the host's copy was rejected too: the file is then not the
same on both sides, which is what the synced dotfiles rely on. The checkout is
the host's to clone, in the pane, by the `revier open` that runs there. A
name the host does not know is an error for that host, which is how a
missing file there is found. ssh runs in batch mode with a connect timeout,
so a survey never waits on a password prompt; `ControlMaster` in the ssh
config is what makes the round trip cheap, and revier does not stand in for
it.

### D41 — a link is its own kind of file, not a project file with a host — Accepted

Supersedes the file part of D40: what a remote project's file is, and where
it lives. The rest of D40 stands: the survey over ssh, the merge, the pane,
the attach, the forwarded agent commands and actions.

D40 put `host = "buildbox"` on the project file and had the same file read
on both machines. That was the wrong assumption. On the host, the file
defines a project: a directory, its targets, its agent. Here, the file
refers to one: which host, which name there, and the pane that reaches it.
A definition and a reference are different kinds of thing, and one shape
for both cost two workarounds - the host had to be told its own name in
`config.toml`, or it asked itself over ssh, and the path was written in the
host's terms so this side could not expand it.

A link is a file in `projects/` with a `[remote]` table and nothing a
project file needs: no `git_url`, no directory here. `host` is the ssh
destination; `project` is the name on the host, and defaults to the link's
own name. The two names may differ, since the file carries both: the host
is asked by its name, the list shows this one, as `name@host`. The home
target is derived - the ssh pane onto `revier open <project> --attach`
there - unless the link declares one. A link may declare further targets,
ordinary local windows onto the project: an editor over ssh, a page. A
`path` is allowed, kept as written, for their templates; it is the host's
path and is never a directory here.

Nothing is read by both sides. The project file on the host has no host
field, the host never asks itself, and the self-name line in `config.toml`
is gone. One directory holds both kinds, told apart by the table at the top
of the file, rather than a `links/` directory: a project is found by name
everywhere, and two directories would have been two places to look. The
onboarding flow that lists a host's projects and writes a link for one is
the next surface over this file.

## 2026-09-12

### D42 — Tab moves the cursor into the pane; there is no target level — Accepted; typing amended by D43, header removed by D51

Narrows D28: Enter stays as it is, and Tab no longer opens a list.

The surface had two levels. Tab replaced the project list with a list of the
highlighted project's targets, with its own header, path line, count and
footer. The pane beside the list already showed those rows, under its
Targets heading, with the same name, key and state. So the second level was a
second rendering of what was on screen, and it hid the list it was reached
from: which project this was, and what the other projects were doing.

There is one level and two places the cursor can be: the list, and the
Targets section of the pane. Tab moves it into the pane, onto the first
target; up and down walk the targets and attached instances; Enter runs the
one under the cursor; Esc brings the cursor back. The header keeps its
counts, and the list stays beside the pane. Enter on a project with no home
target, which used to open the list, moves the cursor into the pane. The
second rendering is gone.

Two cursors on one screen have to read as one thing. The pane's target rows
carry the list's bar column and its selection background, and the agent
rows are indented under them as before.

Typing puts the cursor back on the list and filters, as it does anywhere:
the query is a search for a project, and the list is where its result is. A
target key acts on the highlighted project wherever the cursor is, as it
did: `ctrl-shift-o` is "editor" everywhere, and a key that read the pane's
cursor would mean something different on each row.

A click on a target row in the pane runs it: one click, not the list's two.
The list's first click selects because a selected project is useful on its
own, the pane follows it; a target has no state worth selecting other than
running it, so a click that only highlighted it would be a click wasted.
The wheel over the pane still scrolls it.

On a terminal too narrow for the list and the pane together there is no pane
to move into, so Tab shows the pane in the list's place, at full width, and
Esc gives the list back. It is the same renderer as the wide case, so the
narrow terminal has no second implementation of the target rows.

### D43 — the query line follows the cursor — Accepted

Amends D42, where typing with the cursor in the pane put it back on the list.

With the cursor in the pane, the query line still showed the project query
with its cursor blinking, so the surface pointed at two places at once. And
a keystroke there threw the cursor back to the list, which was one rule for
where a letter lands with two outcomes.

The query line is one field whose scope follows the cursor. On the list it
is the project query, as before. In the pane it is the target query: empty
on entry, with its own placeholder, and matching the target rows by the
ranking the list uses for projects, so the best match is selected as it is
there. A project has a handful of targets today, and a query over three
rows is rarely typed; attached instances are unbounded, and the field costs
nothing while empty. Leaving the pane, by Tab or by Esc, drops the target
query and shows the project query again as it was: the target query is a
view over one project's rows and has no meaning on the list.

Tab toggles: from the list into the pane, from the pane back to the list.
Esc from the pane goes back too, so the one key that means "back" everywhere
keeps meaning it. A second line for the target query was rejected: it would
cost a row on every screen for a field that is empty nearly always.

### D44 — clearing the query puts the cursor back where it was — Accepted

Clearing the query used to leave the cursor on the project the search found,
on the reasoning that a search ends on the project searched for. In use the
query reads as a temporary view over the list, and clearing it, by Esc or by
deleting the last letter, reads as its undo: the list comes back as it was,
with the cursor where it was before the first letter. The row the search
found matters only until Enter, and Enter leaves the surface.

So the cursor's project is remembered when the query goes from empty to
typed, and restored when it goes back to empty. While the query is typed the
first row is selected on every keystroke, as the ranking puts the best match
there.

### D45 — a link is made from the host's own list, in the TUI or on the command line — Accepted

A link file is three lines a person can write (D41), and the first time it
is a page of the README to get right: which host, how it is spelled in the
ssh configuration, what the project is called there. The dialog answers
those from the two places that know: alt+r lists the hosts `~/.ssh/config`
names, as `ssh <tab>` does, and Enter on one asks the revier there for its
projects, `revier list --json` over ssh with no names. Enter on a project
writes the link under the project's own name and returns to the list with
the new row selected. A project already linked says under which name, and
is not linked twice.

The dialog is two steps in the list's own place, with the pane beside them,
and not a window of its own: the surface has one shape, and Esc walks back
through it a step at a time. It stands over the surface rather than beside
it, so while it is up it owns the keys and none of the surface's own act
under them; the cursor goes back to the list as it opens (D42), which is
where the row it writes appears. A host is asked off the terminal, with the
ask in the footer, so a host that is down is a message after its connect
timeout and not a frozen screen. `revier link [host [project]]` is the
same three steps as a command, so the flow is tested without a screen and
usable from a shell: nothing, the hosts; a host, its projects with the link
here that points at each; both, the link written, under `--name` when the
name here is to differ.

A host that fails says why in one line of revier's own words, not in ssh's.
`Host key verification failed.` names the fault and leaves the reader to work
out the remedy; ``host key not known - run `ssh router` once to accept it`` is
the same fault with the next step in it. The failures worth naming are ssh's
usual ones - an unknown host key, a refused key, a name that does not resolve,
nothing on port 22 - and the two the other end gives: a shell that cannot find
revier, and a code-hosting service, which answers and runs nothing. The shell
that cannot find revier is not told it is not installed, because it usually
is: a shim under a version manager is on an interactive shell's PATH and not
on the PATH an ssh command gets, so the line names the PATH and where the
shims belong. ssh's own
are told from the remote's by the status it exits with, 255 and nothing
else's, so a remote revier that says "permission denied" about a file of its
own is not read as a refused key. Anything unrecognised passes through whole,
the command with it. OpenSSH is not translated, so its words are the same
wherever ssh runs.

The hosts come from the ssh configuration alone, `Include` followed and
patterns left out, because a pattern is a rule and not a destination.
`known_hosts` was left out: a machine reached once by address is not a
destination anyone wants offered. A remote for a host is made when it is
first asked about, and kept, so a link written while the surface runs is
surveyed on the next refresh without a restart; the map the wiring built
from the project files at start is gone.

### D46 — the set of open projects is revier's; the contents of a pane are not — Accepted; narrows the scope row in product.md

Session persistence across reboot was cut, on the grounds that the runtime
owns persistence and revier does not paper over the difference between a tmux
that has it and a kitty that does not. That reasoning holds, and nothing here
contradicts it: the content of a pane — its processes, its scrollback, its
shell history — is still the runtime's, and revier records none of it.

What the cut also removed, and should not have, is the *declarative* half.
Which projects were open, and which of their targets, is not the runtime's
knowledge at all. It is revier's own model, it survives no reboot anywhere,
and reconstructing it by hand is the thing that makes a restart expensive
when twenty projects are open. So `revier session save` records that set and
`revier session restore` opens it again.

Restore is run-or-raise over the recorded names, which is what keeps this from
being a second mechanism: a target already up is left alone, so a re-run is
free and a half-finished restore is fixed by running it again. Launches are
sequential, because a launch is attributed to the window that appears after it
(D21) and two at once are two windows neither can claim.

The file holds names and nothing else — no host, no ref, no argv. All of it is
derived again at restore from the project file as it reads then, so a project
edited between the two wins over the recording. A timestamp is the identity
rather than a uuid: these files never leave the machine and never merge, so
uniqueness across machines buys nothing, while sorting and being typeable are
used on every restore.

Attached instances are not recorded and cannot be. An attachment is a live id
with no launch argv anywhere in the model (D20), so there is nothing that would
bring one back; the save says how many it dropped, because discovering that
after the reboot is the worst moment to discover it.

The word "session" is D2's own rejected word, and it is used anyway: the
private type `snapshot` in `internal/core` already means the instance listing
of one survey, and two meanings of that word inside the package that owns both
would cost more than one meaning of "session" across the product. D2 is
unchanged — this is not session management, it is the project list written
down.

Left out: closing anything. `Host` has `Open`, `Focus` and `Focused` and no
`Close`, and adding one across every host would buy nothing a reboot does not
already do. Left out too: waiting for agents to fall quiet before saving. The
survey already knows every agent's status, so it can be added when it is
wanted, and nothing here has to change to allow it.

### D47 — an agent panel is restored onto its conversation by its own probe — Accepted; where the id comes from superseded by D55

A restored workspace whose agent starts empty solves the cheap half of the
problem. Reopening twenty terminals in the right directories was never the
painful part; finding twenty conversations again is. So `AgentProbe` gains an
optional capability, `Resumable`, detected by type assertion like
`WindowWatcher` and `PanelWriter`: a probe that implements it names the
conversation a panel holds, and builds the argv that starts the harness on it.

It is the probe's and not the runtime's. tmux dies on a reboot like everything
else, and what survives one is a tmux plugin the user installed, which is
theirs and not revier's to mediate. There is no `Runtime` counterpart to this
capability and there should not be one: either the runtime already handles it
or it cannot, and in both cases revier adds nothing.

`ResumeCommand` is handed the `PanelSpec` as the project declares it *now* and
folds its own flag into that, rather than the snapshot storing a finished argv.
Storing the argv would freeze the config the same way storing the host would.
`--resume`, the uuid, and `~/.claude/projects` never reach the core; only an
opaque `SessionID` crosses the port.

The identity of a panel across a restart is its position among all the
target's panels, which is the position of its `PanelSpec`: a runtime lays
panels out in the order they are declared. A live panel's title is the agent's
to rewrite — Claude Code replaces it with a summary of the turn — so a title is
no identity at all. Counting only the agent panels was rejected: save sees
which panels a probe claims and restore sees which specs say `kind = "agent"`,
and one declared agent that no probe claims makes the two counts disagree, so
the conversation of the agent after it would start in it. A resume is applied
only where the project still declares an agent at that position, which is what
keeps a layout edited since the save from typing a resume flag into a shell.
Every way this can fail drops that one resume and starts the panel empty: a
harness not installed here, a probe without the capability, a position that is
no longer an agent.

The Claude probe reads the id from a user variable a `SessionStart` hook sets
(`contrib/claude/revier-session-hook`), the path `CS_TAB` and `CS_STATE`
already use. Deriving it instead from the newest transcript under
`~/.claude/projects` for the pane's directory was rejected: it cannot tell two
agents in one repository apart — the case this feature exists for — and
resuming the wrong conversation is worse than resuming none. It would also
make the probe do I/O, and the probe being a pure function is what keeps it at
layer L1.

### D48 — a tmux pane's variables arrive in one option revier owns — Accepted

kitty reports every user variable a pane set, because `kitten @ ls` carries
them as a map, and the Claude probe has always read `CS_TAB` and `CS_STATE`
from it. tmux reported none: `Panel.Vars` was left nil there, so attention
state never worked on tmux and a conversation id could not have either.

tmux has no map to report. A format can name one option but cannot enumerate
them, and asking pane by pane would be one call per pane, which is the cost
rule the host exists to respect. Naming `CS_TAB` and `CS_STATE` in the format
would have put a probe's vocabulary inside a runtime adapter, where two
adapters could then disagree about what a variable is.

So revier claims one pane option, `@revier`, holding space-separated
`NAME=value` pairs, and the program that writes it packs them. The host parses
pairs and knows nothing about which of them anything reads. A value containing
a space cannot survive it; the variables revier reads are tokens.

### D49 — what is not about one project stands on the top line, a button and a key each — Accepted

Every key on the surface acted on the row under the cursor, or on the query.
Adding a project, linking one on another machine and opening the
configuration act on the installation instead, so none of them had a row to
hang off, and a bare letter could not reach them: every printable rune is a
filter character. alt+r reached the link dialog, and nothing on the screen
said so.

The first line of the surface is a bar of those actions: new (alt+n), remote
(alt+r), config (alt+c). A button carries its key, declared in one place, so
the bar is a second way to reach an action and never the only one; the
surface is driven from the keyboard, and a button with no key would strand
it. One click runs a button, because a button has nothing to select. A
dialog takes the bar's line to name itself, since no button acts while one
is up.

new asks for one thing, the directory, and names the project after it, as
`revier new` does, writing the same file. A directory that is not on this
machine is refused: there is no `git_url` yet to clone it from. config opens
`config.toml` and re-reads nothing, because the theme, the glyphs and the
configured actions are read at start, and applying them while the surface
runs would rebuild it under the user.

A configured action is not a button. It runs against the selected project,
so it belongs with the row's keys in the footer.

### D50 — the pointer lights what it is over, a step below the selection — Accepted

Narrows D33 and D36: the wheel and the clicks stay, and the pointer is now
reported everywhere, not only while a button is down.

A row, a target and a button looked the same whether a click would do
anything or not. Whatever the pointer is over now takes a background. Two
things can be lit at once - the row the keys act on, and the row under the
pointer - so hover has its own background, a step below the selection's,
and the selection moved up a step to keep them apart. The pointer lighting a
row selects nothing, as D36 requires.

Reporting every motion takes nothing D33 had not already traded: drag-select
was given up for the wheel, and shift-drag still selects.

A project row keeps D36's two clicks: selecting it costs nothing and opening
it opens a window. A target keeps D42's one. Two clicks on a target were
tried and undone, because once hover says a target is live, a first click
that only moves the cursor is the wasted click D42 argued against.

### D51 — no header and no border; the rule carries the counts — Accepted

Narrows D38 and D42: the surface still takes the whole terminal, and D42's
counts move to the rule.

Above the list stood a header line with a badge naming the program and two
counts, a query line, and a rule carrying one more count, all inside a
border. The badge named the program the user had just started. The border
boxed a surface that already fills the terminal, and charged two rows and
four columns for repeating the terminal's own edges.

Both are gone. The rule, which already carried how many rows the filter
left and was otherwise empty, carries the counts too, and they count the
list by what its agents are doing - blockers, working, idle - each project
once under its worst agent, the state its row shows, so the rule is the key
to the rows. Where the rule is too narrow for all of it, the counts go and
the ratio stays, since the ratio changes as you type.

The separating a border did is done by lines: a thin rule under the action
bar, and a blank line between the query and the rule, so the query does not
read as the rule's caption. The top of the surface is six lines, as it was
with the header and the border, and the list gains the four columns.

### D52 — the session hook reaches kitty through remote control, not an escape — Superseded by D55

D47 said the `SessionStart` hook sets its variable by "the path `CS_TAB` and
`CS_STATE` already use", and the hook wrote a `SetUserVar` escape to
`/dev/tty`. Neither held. `CS_TAB` is an escape, but a launcher emits it before
exec, from a shell that has a terminal. `CS_STATE` is set by a hook, and that
hook calls `kitten @ set-user-vars` on `KITTY_WINDOW_ID`. A Claude Code hook
runs with no controlling terminal, so the escape never reached kitty: the hook
exited 0, the window carried no `CS_SESSION`, and a save recorded no
conversation. revier degraded as designed and restored an empty agent, which
is why nothing failed loudly.

It was found by the first run against a real agent in a real kitty, the one
layer the automated suites cannot reach. The tmux branch was unaffected: it
sets a pane option through the tmux client, which needs no terminal either.

The hook now calls `kitten @ set-user-vars --match id:$KITTY_WINDOW_ID` on
`$KITTY_LISTEN_ON`, the mechanism `CS_STATE` has always used, and does nothing
outside kitty. Verified on screen: the variable appears, the save records it,
and the restored agent answers from the conversation it held.

### D53 — config is a screen, and a change applies as it is made — Accepted

Narrows D49: config stays a button on the top line with alt+c, and no longer
opens `config.toml` in `$EDITOR`.

The editor opened an empty file on most machines, because the defaults are
not written down, so the user saw nothing to change and no list of what could
be. The screen shows every setting it offers with its current value: the
theme, the glyph set and the trigger key under `[ui]`, and the runtime host
under `[hosts]`. The window host is shown and not offered. Which one a
machine has belongs to its desktop, and sway cannot be chosen inside GNOME.

A change is written when it is made, and applied when it is written. D49 kept
the configuration unread while the surface ran, so that nothing was rebuilt
under the user. That reason does not hold for a change the user makes on the
screen. The theme and the glyphs repaint in place, and the lists keep their
cursors. A runtime choice is probed first and written only when it is usable,
because a configured host that does not probe refuses the next start. The
surface then swaps in a new core over that host, so a survey already running
finishes on the host it started with.

The file is edited line by line, not re-encoded. A TOML encoder writes back
only what it decoded, and comments are not decoded. The value is put in place
of the old one under its table, and every other line stays as it was. The
result is validated as a start validates it, and read back, before it is
written. A key the line editor cannot find, such as a dotted key, is refused
instead of being written twice.

The trigger key is only written to `config.toml`. The desktop binding is
still `revier keys apply`'s to change.

Configured actions and probes are not on the screen yet. They are lists of
records, and they are still edited in the file.

### D54 — alt+h lists every key, read from where each key is declared — Accepted

Extends D49 with a fourth button, beside the config screen of D53.

The footer names the keys of the focus and the row under the cursor, and a
narrow terminal cuts the rest with an ellipsis. The query's editing keys, the
target keys of other projects and the desktop keys were on no screen at all,
so a key used once a week had to be looked up in the configuration.

help (alt+h) stands in the list's place and lists them all, grouped by what
they act on: the list, the pane, the selected project, the top bar, the
query, the target keys, the configured actions, and the desktop keys `revier
keys install` claims. Each is read from its declaration - the key map, the
bar, the target vocabulary, `[ui] trigger_key` - so the screen cannot name a
key a press does not match. A target key the terminal delivers as another
chord is listed under the chord that reaches the surface, and a desktop key
that reaches it as nothing is listed only under the desktop.

The screen takes the full width, because it is about no project and the pane
would be empty. It only reads: the arrows and the wheel scroll it, Esc or
alt+h leave it, and a typed letter reaches no filter behind it.

### D55 — a conversation id comes from `claude agents --json`, not from a hook — Accepted

D47 read the id from a user variable a `SessionStart` hook set, and D52 fixed
how that hook reached kitty. Both assumed Claude Code offered no way to ask
which conversation a running process holds. It does: `claude agents --json`,
documented "for scripting", lists every active interactive session with the pid
of its process and its `sessionId`. A save now runs it once and matches each
agent panel by pid. The hook, and the `CS_SESSION` variable it set, are gone.

This is better on every axis the hook was judged on. There is nothing to
install, so the feature works on the first save. Agents already running are
named too, where the hook named only sessions started after it was installed —
and the case this exists for is twenty agents open when a restart is due. The
id no longer travels through a terminal, so there is no kitty path and tmux
path to get right separately. And the match is by pid, which is exact: two
agents in one repository are told apart, the property D47 rejected the
transcript guess for lacking.

Checked against Claude Code 2.1 with a real agent before it was built: the
listed pid is the one tmux reports for the pane when Claude is the pane's
command; after `/clear` the listing carries the new conversation's id; after
`--resume <id>` the new process is listed under the same id. The command
answers in about a tenth of a second.

`Resumable` asks for every panel of a save at once, `Sessions(ctx, panels)`,
rather than once per panel. The answer costs a process, and a save of twenty
agents must cost it once — the rule `Host.Instances` follows for the same
reason.

What it cannot see: a pane where `claude` was typed into a shell. tmux reports
the pane's first process, the shell, and the pid matches nothing. kitty reports
the foreground process and is not affected, and revier always launches an agent
as the pane's own command. A save counts the agents it could not name and says
so while they still run, which also makes a future change to the command
visible at the first save rather than after a reboot.

The same listing reports each session's `status`: `idle`, `busy`, and
`waiting` while a permission prompt is up. That is the attention signal the
`CS_STATE` hooks provide, available the same way, and it is left for a
decision of its own.

## 2026-09-13

### D56 — actions are edited on the config screen — Accepted

Narrows D53: configured actions are on the screen. Probes are still edited in
the file.

The screen lists each action by key, name and command, with a row that adds
one. Enter opens a form of three fields: name, command and key. alt+d deletes
the action after a y. A change is written when it is saved, and the surface
binds the new keys at once: the footer, the help screen and the target keys
that yield to an action are rebuilt from the new list.

The command is one line, split into words the way a shell splits them: quotes
group, a backslash escapes, and a `{{ }}` template is one word. It is written
as the argv the file already holds, so an action written by hand and one made
on the screen are the same. One field per word was the alternative, and it is
slower to type for the common case of a short command.

A key that already means something on the surface is refused and named: a
key of revier's own, a query editing key, another action's key, or a target
key. An action is run by name with `revier run`, so a name is refused when
another action has it. The rules `config.Load` applies to an action key still
hold, because the write runs them.

An `[[action]]` entry is edited line by line, as D53 edits a key. Only a
changed value is written, in place of the old one, so an unchanged array keeps
its lines and its comments. A deleted entry loses its header and its values,
and every comment in it stays, including one inside an array written over
several lines. Every edit, an add included, names the actions as the screen
read them, and it is refused if the file no longer holds them: the file was
changed by hand while revier ran, and an add would otherwise write a second
action under a name or a key the screen did not know was taken. Actions
written as an inline array are refused too, because the line editor does not
find them.

### D57 — a Claude agent's state comes from `claude agents --json`, not from its title — Accepted

D55 left this open. The Claude probe read state from two terminal signals: the
glyph at the start of the pane title, and the `CS_STATE` variable a hook set.
Both reach revier only if the terminal carries them. On tmux the title arrives
only when tmux accepts the title escape, and `CS_STATE` only when a hook writes
the `@revier` option (D48). Where either is missing, an agent that waits on a
permission prompt shows as idle, which is the one state the product exists to
surface.

The listing D55 already reads carries the state itself. Claude Code derives it
from its own UI and reports it per session: `status` is `busy` while a turn
runs, `idle` at rest, and `waiting` while it is blocked on the human, with
`waitingFor` naming why - `permission prompt`, `input needed`, `sandbox
request`, `worker request`, `dialog open`. Checked against Claude Code 2.1.270,
in its binary and in the agent-view documentation, which names `--json` as the
interface for scripts. `waitingFor` arrived in 2.1.162, and 2.1.212 moved the
sandbox and MCP-input waits from working to waiting, so 2.1.212 is the oldest
version whose answer is complete.

So the probe maps the listing, matched to the panel by pid as D55 does:
`busy` is running, `waiting` is attention, `idle` is idle, and any other value
is unknown. An unknown value reports unknown and not idle, the opposite of the
glyph rule it replaces: a status Claude Code adds later is more likely a new
kind of wait than a new kind of rest. A panel the probe matches with no session
in the listing is unknown too. The title glyph, `CS_STATE` and `CS_TAB` are no
longer read; a panel is claimed by its foreground command. The title still
supplies `Activity`, because the listing carries a session name and no summary
of the turn, and a title that does not arrive costs the summary and no longer
the state.

The listing costs a process: about 80 ms and 175 MB each run. Run on every one
second survey, that is a constant share of a core spent watching. So the probe
runs it again only when an answer may have changed. Claude Code rewrites
`~/.claude/sessions/<pid>.json` in place on every status change, and the probe
compares those files' modification times, and the set of files, against the
last run. It runs the listing when they differ, and at least every ten seconds
regardless. Only the times are read. The files' content is not a documented
interface, and parsing it would make revier depend on it: if Claude Code moves
or stops touching them, the state still comes from the listing, only up to ten
seconds late. inotify was considered for the same trigger and rejected: it
adds a watcher goroutine and a dependency to learn what one directory read per
survey already tells.

The cached answer makes `Inspect` read state the core did not pass in, which
D47 kept it free of. The source is injected, as `Sessions` already injects
`Agents`, so the probe stays testable at layer L1. The port does not change:
the cache is a fact about one tool, and it stays in the adapter.

What this does not cover. A Claude session in a container writes to the
container's home and reports pids from another namespace; containers are
unsupported (D40), and such a panel reports unknown. An agent on another
machine is already its host's word (D40), read by the revier there through the
same listing. `claude` typed into a tmux shell is not matched by pid, the limit
D55 records, and now reports unknown where it once read its title.

### D58 — the link dialog names every link, and lists a host's projects as the surface does — Accepted

Narrows D45: the hosts, the host's list and `revier link` stay as they are.
Enter on a host's project no longer writes the link under the project's own
name. It opens a third step, a field holding `rs-<host>-<project>`.

The project's own name was the wrong default. A project checked out on two
machines has the same name on both, so the first link from a machine that
shares projects with this one was refused. The refusal pointed at a command
to run outside the TUI. `rs-<host>-<project>` is not a name a project here
takes by accident, and it says where the project lives before the row's
`@host` does. The name is asked every time, not only on a collision,
so every link is named the same way and the one keystroke Enter costs buys
that. The offered name is drawn as a selection, and the first character
typed replaces it.

The field is checked on every keystroke: a name that is empty, not a file
name, or the name of a project here is said under the field as it is typed.
Enter on such a name writes nothing. A warning that Enter could confirm was
the alternative, and it would overwrite a project file from a dialog with no
undo.

The host's list is drawn by the project table (D39) and not by a table of
its own. The fixed columns of the old rows ran a long name into its path and
cut the path to a stub. The table sizes its project column to the list, and
a row already linked says under which name in the note under the state. A
path on the host is written with its home as `~`. The host's home is not
known here, so it is taken to be the directory under `/home` or `/Users` the
path starts in; the pane still shows the path whole.

`revier link` keeps the project's own name, with `--name` to change it. A
command has no field to offer a name in, and a script that links a project
expects the name it asked for.

### D59 — targets most projects share are declared once, in config.toml — Accepted

Narrows D14: a target is still a named binding a project has, but a project
file no longer has to declare every target it has.

The eighty-nine converted projects declared the same three targets - the
workspace, IntelliJ and the ticket viewer - each with `{{.Name}}` where they
differed. About four lines in ninety files were each project's own. Changing
the editor meant editing every file.

`config.toml` now takes `[[target]]` tables in the project file's form, and
every project gets them. A project's target of the same name is merged into the
shared one field by field, and a target of a new name is added after the shared
ones. The merge is on the tables as TOML decodes them, before anything is
validated or rendered, so everything after the load - validation, templates,
keys, the TUI - sees a complete project as before.

A table merges key by key at any depth, so a project that only differs in its
editor's window title writes that title and keeps the shared class and launch.
Any other value replaces the shared one whole, a list included. Merging `panels`
item by item has no rule a reader could predict: a panel has no name to be
matched on, and a position is not an identity.

A shared target is checked once, in `config.toml`, for a name, a name given
twice, and a value of the wrong type. Whether it is complete is checked per
project after the merge, because a project may supply the part it leaves out.
That error names the project file.

A link has no shared targets. Its home is the derived ssh pane (D41), which a
shared home would replace, and the project file on the host already has that
host's shared targets.

A project may not remove a shared target. Nothing needs to yet.

`revier new` writes a file without targets when `config.toml` has shared
targets, and the template of D30 when it has none.

A name with a character a regexp reads, such as the dot in
`dynatrace.agent.studio`, is matched through the shared `^session:{{.Name}}$`
as it is, unescaped. A dot then matches any character. That still finds the
project's own window, and it spares every shared match an escaping function.
D30's `revier new` template still escapes it, as the file it writes has its
own match.

### D60 — shared targets are edited on the config screen — Accepted

Narrows D59: the shared targets it put in `config.toml` are on the screen, with
add, change and delete, as D56 put the actions there.

The screen lists each shared target by key, name and where it opens. Enter
opens a form under the row: the name, the key and the home flag, then the
runtime and the window realization, each with its name, command, match title,
match class and place. The runtime's panels are a list inside the form, and
Enter on one opens a form of its kind, title and command. A realization whose
fields are all empty, with no panels, is none. A panel change is kept in the
form and written only when the target is saved, so Esc leaves the file as it
was.

The form shows what a target is made of and hides what it rarely needs. `dir`,
`prefer` and a match's `pid` are not on it; they are kept as the file has
them.

An entry is edited line by line, as D56 edits an action, one level deeper. A
changed value is written in place; a realization or a panel added is a table
added at the end of what it belongs under, indented two spaces a level as the
project files are; one deleted loses its tables and values, and its comments
stay. A match written inline is written whole, with the keys the form does not
show kept. The form tells the writer which panel of the file each of its
panels was, so a panel deleted before another does not move its comments onto
the next.

A write is refused unless every project still loads with the result. A shared
target is part of every project, so a change that breaks one - a target
deleted that a project only overrides, a place of three tokens - would refuse
the next start. The refusal names the project file. After a write the surface
loads the projects again and has the new targets at once.

The config screen now takes the whole width, as the help screen does. A form
with notes beside its fields needs the columns, and the pane beside the
screen was empty.
