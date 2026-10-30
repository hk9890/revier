# The TUI

`revier` with no command opens the TUI: revier's full-screen view in a
terminal. It shows every project, what its agents do, and its targets. To open
it from a desktop key, use [the popup](#the-popup).

```
revier
```

When the output is not a terminal, for example `revier | grep`, revier prints
the `revier list` table and exits.

## The screen

- **The top line** has a button for each thing that is not about one project:
  agents, new, remote, sessions, config, logs, assist and help. Each
  button shows its key. In a narrow terminal the last buttons fold into `≡`;
  their keys still work.
- **The query line** filters the rows under it.
- **The project list** is on the left. **The pane** on the right shows the
  project under the cursor.
- **The footer** shows the keys for where the cursor is. An error replaces
  them until the next key press. When an operation such as a go, a clone or an
  action fails, the footer names it and the project, for example
  `clone failed: alpha (alt+l for details)`; the [logs screen](#screens) has
  the full error.

A [status](extending/status.md#places) can show on the rule under the top
line, and in the pane.

### The project list

Each project shows its name, its path, a mark when it is open, and its agents
counted by [state](concepts.md#agents). Projects with an agent that needs you
are on top. Open projects come next, then all others. A project on another
machine shows its link name, for example `far@buildbox`; see
[links.md](links.md).

### The pane

The pane's top line holds `logs`, which opens the [logs screen](#screens), and
the project's name, which opens the project screen. Under it the pane shows the
project's status, path and git URL, then two sections:

- **Targets**: each target, its key and its state, then each window you
  attached with `revier attach`.
- **Agents**: each agent, its harness, its state and what it does. Under the
  list is the terminal of the selected agent, as it is now.

When a file does not load or a host does not answer, the pane gives the
reason. When the project directory is missing and the project has a
`git_url`, Enter clones it.

| Target state | Means |
|---|---|
| running | an instance is open |
| stopped | no instance is open |
| surveying | revier is still looking |
| no host here | no host on this machine can open it; see [troubleshooting.md](troubleshooting.md#a-target-says-no-host-here) |
| invalid | its configuration is wrong; `revier doctor` says why |
| unknown | the host did not answer; a press is refused until it does |

## Move and act

Type to filter the project list or the Agents, where the cursor is. The
Targets have no filter. The match is fuzzy. The TUI has no single-letter
keys, so every letter goes to the query.

| Key | Does |
|---|---|
| ↑ ↓, ctrl+p ctrl+n, pgup pgdown, home end | Move |
| Tab, shift+tab | Move between the project list and the Agents |
| alt+t | Move to the Targets |
| Enter | A project: open its home target; with no home target, move to its Targets. A target: run or raise it. An agent: bring its tab to the front. |
| a target's key | Run or raise that target of the selected project |
| an action's key | Run that action in the selected project |
| del | Close the row: the project, the target or the agent. It asks first when this closes a whole project, a target that holds an agent, or an agent that works or waits for you. |
| alt+del, alt+d | Delete the project file, or the target's entry in it. On a link, delete the link file alone: nothing on the host changes. When nothing of it is open, it asks `y`; when something is open, it offers to close it and then delete. An agent is in no file. |
| alt+space | Switch between the project list and the [agent list](#the-agent-list) |
| Esc | Clear the query. Then, from the Targets or the Agents, go back to the project list. Then quit, or hide the popup. In the agent list, Esc with no query quits or hides at once. |
| ctrl+c | Quit. While a shutdown, a close or a restore runs: cancel it; a second ctrl+c quits. |

While a shutdown, a close or a restore runs, the TUI shows each step as it
runs and what came of it. No other key and no click does anything until it
ends. ctrl+c lets the step that runs finish, then stops, and the TUI stays
open to show what was done and what was not reached. A restore stopped after
its last target says `return home cancelled`. When a step hangs, press ctrl+c
again to quit: the command the step waits for ends too.

While a shutdown or a close runs, its title says to wait until it ends. After
the last step it waits for what it closed to go: each such step shows
`waiting` until then. A shutdown in which nothing failed, stayed open or was
cancelled ends on `Shutdown complete`, and you can leave. A close with del
says in the footer when it is complete.

A restore ends on the project list, with the cursor on the project the
session was left on. The footer names what did not open, or how many targets
a cancel did not reach. revier does not bring a window target a restore
opens, such as an editor, to the front, but the desktop brings most of them
up over the TUI. A terminal a restore opens is focused, so its tabs come up
as saved. When the restore ends, failed or cancelled too, the TUI brings its
own window back to the front once. A restore that stopped before its first
step opened nothing, and raises nothing. A TUI in a terminal that revier does
not list stays where it is.

Closing a whole project saves the session first. You cannot rename a project
while something in it runs: close it first.

**Mouse.** A click selects, a double click acts as Enter. A click on the
error in the footer opens the logs screen, as alt+l does. The wheel
scrolls the pane or an agent's terminal. A drag copies the text of a box to the
clipboard (OSC 52, works over ssh). Use shift+drag for the terminal's own
selection.

## The agent list

Press alt+space to see every agent of every open project. In the popup, press
the trigger key instead. Agents that need you are on top, then working
agents, then idle ones, then those in state `unknown`. Among those that need
you and among the idle ones, the one that spoke last is first. Each row shows
how long ago the agent spoke. Type to filter by project or by what the agent
does. The pane shows the agent's [address](agents.md#drive-an-agent-from-a-script),
the name its harness gives it, such as `claude:shop-a0` for the name Claude
Code's agents message it by, and the agent's terminal.

## The popup

```
revier popup
```

`revier popup` opens the TUI in its own kitty window, or raises the one that
is open. The trigger key runs it, Alt+Space by default; see
[getting-started.md](getting-started.md#install-the-desktop-keys). It needs
kitty, and GNOME with the Window Control extension. See
[integrations/kitty.md](integrations/kitty.md) and
[integrations/gnome.md](integrations/gnome.md).

- The popup opens on the project of the window that had the focus.
- When Enter or a target key brings a window to the front, the popup hides.
  It stays when you press a key, click, scroll or raise it again before the
  window comes up, so a question you started, such as a delete, waits for
  your answer.
- After a full shutdown the popup quits, so the next press starts it again
  and shows the [start dialog](#after-a-full-shutdown). When `revier shutdown`
  shut everything down while the popup was hidden, the next raise shows the
  dialog.
- Which edits a raise reads again is in [Files](configuration.md#files).

## Screens

Each screen takes the place of the list. Esc goes back one step.

| Key | Screen | Does |
|---|---|---|
| alt+n | new | Add a project. Type a path, a name, or a git URL to clone. |
| alt+r | remote | The machines you added, each with its revier version. Pick a machine, a project, a name to link a project there. On the screen, alt+n adds a machine from `~/.ssh/config` and alt+d removes one. See [links.md](links.md). |
| alt+e | project | Edit the project's name, path, git URL and targets. Each change is written at once. |
| alt+s | sessions | Restore a saved session with Enter, save the open projects with alt+s, or shut down with alt+q. The cursor starts on the session `revier session restore` opens. Its top line has the save and shutdown buttons. See [projects.md](projects.md#save-and-restore-sessions). |
| alt+q | shutdown | Close every project or one. It shows the plan and saves the session first. It belongs to the sessions screen; alt+q opens it from the project list too, and Esc goes back to where you opened it. |
| alt+c | config | Change theme, glyphs, trigger key, runtime host, shared targets and actions. Each change is written to `config.toml` at once. |
| alt+a | assist | Raise `revier-config-assistant`: Claude Code beside the TUI, briefed to fix your configuration. See [agents.md](agents.md#let-an-agent-configure-revier). |
| alt+h | help | Every key the TUI knows, including your target keys and actions |
| alt+l | logs | The warnings and errors of this machine's log over the last 14 days, newest first. When the footer shows an error, the cursor starts on the newest line the TUI wrote by the time of that error, which is usually the error's own. Enter, or a click, shows every field of a line. It opens from every screen; Esc or alt+l goes back to the screen you opened it from. A linked project's operations are in the log of its host. |

In a form, Tab and ↑ ↓ move between fields, Enter saves, Esc cancels. The
fields are those of [configuration.md](configuration.md).

## After a full shutdown

A full shutdown shows what it closed for about 1.5 seconds, then the TUI
quits. Press any key to quit at once. A project shutdown, a close with del,
and a full shutdown that is not remembered when it ends keep the TUI open,
and the next start does not show the dialog. A full shutdown is not
remembered when it found nothing left to close, when revier could not write
its state, or when you opened something while it ran.

The next start of the TUI, or of the popup, opens on a dialog:
`revier was shut down at 18:30.` The pane shows what the saved session holds.
A TUI that stays open while `revier shutdown` runs elsewhere shows the same
dialog at its next refresh, once you are back on the project list with no
query.

- **Restore this session** restores the session the shutdown saved. The
  sessions screen shows each step, then the project list opens.
- **Pick another session** opens the sessions screen.
- **Start fresh**, or Esc, opens the project list. It deletes no session.

Anything you open before that, with a desktop key, `revier go`,
`revier open` or `revier run`, ends the shutdown, and the dialog does not
show. `revier session restore` ends it as it starts, also when it opens
nothing.

## After an end without a shutdown

When revier ended without a shutdown, for example because its terminal was
killed, the next start of the TUI, or of the popup, saves what it last saw
open as a `last seen` session, if something of it is not open now. It then
opens on a dialog: `revier ended without a shutdown; last changed 18:30.`
The pane shows what that session holds. The choices are those of the dialog
after a full shutdown. The dialog stays until you choose, and shows once: a
later start does not offer the session again. See
[Save and restore sessions](projects.md#save-and-restore-sessions).
