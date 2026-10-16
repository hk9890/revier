# The TUI

`revier` with no command opens the TUI: every project, what its agents are
doing, and its targets. This page describes each part of the screen and every
key. To open it from a desktop key, read [the popup](#the-popup).

```
revier
```

When the output is not a terminal, for example `revier | grep`, revier prints
the table that `revier list` prints and exits.

## The screen

From top to bottom:

- **The top line.** A button for each thing that is not about one project:
  **agents** (or **projects**), **new**, **remote**, **sessions**,
  **shutdown**, **config**, **assist** and **help**, each with its key. The
  installed version is at the right edge where the terminal has room for it.
- **The thin rule.** It closes off the top line. A status can stand at its
  left end and at its right end. Refer to [Status places](#status-places).
- **The query line.** What you type filters the rows under it.
- **The rule.** It says how many projects the filter leaves out of all of
  them. On the right it shows the totals of the agents by state, in the same
  columns as the counts on the rows.
- **The project list**, and the **pane** beside it.
- **The footer.** The keys for the place the cursor is in. An error replaces
  the keys until the next key press.

On a terminal too narrow for the list and the pane together, there is no
pane beside the list. When you move the cursor into the pane (alt+t, or Tab
to the agents), the pane takes the place of the list.

revier uses colour unless `NO_COLOR` is set to a value that is not empty. Bold
and reverse video stay.

## The project list and how it sorts

Each project takes two lines: its name, and its path under it. To the left of
the name is a mark that says whether the project is open. To the right, the
project's agents are counted by state:

| State | Means |
|---|---|
| needs you | the agent waits for an answer or a permission |
| working | the agent is in a turn |
| idle | the agent is at rest |
| unknown | nothing says what the agent is doing |

The list is sorted in three groups:

1. projects with an agent that needs you;
2. projects that are open on this machine;
3. all other projects, in the order of their files.

A row moves only when a project opens or closes, or when an agent's state
changes. The cursor stays on its project when the rows move. When a project
closes, the cursor keeps its place in the list.

Before the first refresh answers, the rows carry no mark: revier does not yet
know what is open. The pane says `surveying` until then.

A project on another machine shows as its link name, for example
`far@buildbox`, with what each agent there is doing. Refer to
[links.md](links.md).

When revier starts in a terminal, the cursor goes to the project of the
working directory, else to the project of the focused window, else to the top
row.

## Filters

The project list and the pane's Agents section each have a query field over
their rows. The targets have none.

- Type to filter the section the cursor is in. The match is fuzzy. The
  letters that matched are highlighted.
- A project matches on its name only.
- The filter keeps the sort groups. In a group, a row that holds the query as
  one run of letters stands above a row that holds the letters apart.
- The first match is selected as you type.
- Esc clears the query, and the cursor goes back to the project it was on
  when you started to type.
- When a press opens what you found (Enter, a double click, a target key, an
  action), the query ends and the cursor stays on that row.

Every printable character goes to the query. That is why the TUI has no
single-letter keys: a project called `queue` must be reachable by typing.

## The pane

The pane shows the project under the cursor:

- **Project**: the name, as a button. A click on it, or alt+e, opens the
  [project screen](#the-project-screen). A project-line status can stand at
  the right end of this line.
- **Status**: `running`, `stopped`, `surveying`, `invalid`, `unreachable` or
  `not available`.
- **Host**, for a project on another machine.
- **Path** and **Git URL**. A path that does not exist is maroon, a step away
  from the red that marks an agent that needs you.
- For a file that did not load, or a host that does not answer, the reason in
  full.
- For a project whose directory is missing: `Directory is not on this
  machine`, and what Enter does about it. With a `git_url`, Enter clones the
  repository and opens the project.
- The project status section, if one is configured.
- **Targets**: each target, its state and its key. A tab target is drawn under
  the target it opens inside. A window you attached to the project is listed
  as `attached`.
- **Agents**: every agent of the project, with its harness, its state and
  what it is doing. This section has its own query field.
- The terminal of one agent as it is now, in its own colours. This is the
  agent under the cursor in the Agents section. Until you move to one, it is
  the agent that needs you, else an agent at rest before one that is working.
  A line wider than the pane continues on the next line. The wheel over it
  scrolls back through it.

A target row shows one of these states:

| State | Means |
|---|---|
| running | an instance is open |
| stopped | no instance is open |
| no host here | no host on this machine can realize the target, for example a window target with no window host |
| invalid | the target's own configuration was refused; `revier doctor` says why |
| unknown | the host could not list its instances on the last refresh; a press is refused until it answers |
| surveying | the first refresh has not answered yet |

## Sections and Enter

The cursor is in one of three sections: the project list, the pane's
Targets, or the pane's Agents.

- Tab moves between the project list and the Agents. Shift+Tab moves back.
- alt+t moves to the Targets. Tab from the Targets goes back to the project
  list.
- Esc clears the query, then brings the cursor back to the project list.

Enter acts on the row:

- **A project** opens its home target, the same run-or-raise as
  `revier go home`. A project with no home target moves the cursor to its
  targets, and the footer says why.
- **A target** is run, or raised when it is open already.
- **An attached window** is raised.
- **An agent** has its tab brought to the front. For a project on another
  machine, this is the tab on this machine that shows the agent.

A **target key** runs-or-raises that target of the selected project, wherever
the cursor is. An **action key** runs the configured action against the
selected project. The action gets the terminal while it runs. Refer to
[projects.md](projects.md) for targets and actions.

### Target keys in a terminal

A target key is a desktop key first, for example `ctrl+shift+o`. In a terminal
that reports key disambiguation, such as kitty, a ctrl+shift key arrives in
the TUI as itself. In other terminals it arrives as ctrl and its letter. Then
the TUI runs the target on that key only where the ctrl key means nothing else
in the TUI: not one of its own keys, an action, a query editing key, or
another target's key.

## Close and delete

- **del** closes the row under the cursor: a project, a target, an attached
  window or an agent's tab. When the close ends an agent, or a whole project,
  the TUI asks first and shows the plan. Closing a whole project saves the
  session first when it changed. In a query field, del deletes the character
  right of the cursor; it closes the row only when the cursor is at the end
  of the query.
- **alt+del** (or **alt+d**) deletes what the configuration holds of the row,
  after you answer `y`. Any other key keeps it. What is open of the row closes
  first.
  - A project loses its file.
  - A link loses only the link file. Nothing changes on the host.
  - A target is removed from the project file. A target from `config.toml` is
    every project's and is not deleted here.

A project with something running cannot be deleted or renamed. Close it
first.

## The mouse

- Whatever the pointer is over lights up, in a colour that is different from
  the selection.
- A click on a project, a target or an agent selects it. A double click opens
  it, as Enter does.
- A click on a top-line button, or on the project name atop the pane, runs it
  on release.
- A click on a query field moves the cursor there.
- The wheel over the pane scrolls the pane. Over an agent's terminal it
  scrolls back through that terminal. Anywhere else it moves the selection one
  row a notch.
- A drag selects a box of the screen. The screen holds still while you drag.
  On release, the box's text is copied to the clipboard with OSC 52, which
  also works over ssh. Esc drops the selection and copies nothing. In kitty
  and most other terminals, shift+drag still makes the terminal's own
  selection.

### The ≡ menu

A terminal too narrow for every button ends the top line in `≡`. A click on
`≡` shows the other buttons; a second click shows the first ones again. `≡`
has no key: every button behind it has its own.

## The agent list

The key that opens revier, pressed again on revier, shows the agent list in
the place of the project list. It is alt+space by default. The same key, or
the **agents** button, goes back to the projects.

The agent list holds every agent of every open project:

1. the agents that need you;
2. the working agents;
3. the agents at rest;
4. the agents whose state is unknown.

In the first and third group, the agent that spoke last is on top. Working
agents and agents in an unknown state are sorted by project, so the rows do
not move while the agents work.

A row is the agent's state, what it is on, and how long ago it spoke
(`now`, `5m`, `2h`, `3d`), over its project and its worktree. Beside the list,
the pane names the agent, its status, project, host, path, git URL, harness,
and when it spoke last. Under that it shows the agent's terminal as it is
now, read once a second, in its colours. The wheel scrolls back through it.

- Type to filter the agents, by what each is on and by its project.
- Enter goes to the agent's tab.
- del closes the agent's tab. It asks first when the agent works or waits for
  you.
- Esc clears the query; with no query, it quits, or hides the popup.

The agent list opens on its first row. revier always starts on the projects,
and the popup always comes back on the projects when it is raised.

## The popup

```
revier popup
```

`revier popup` opens the TUI in a kitty window of its own, with the window
class `revier-popup`. When the popup is open already, it raises it where you
left it. A desktop key runs this command, alt+space by default. Refer to
[getting-started.md](getting-started.md) to claim the keys.

The popup needs kitty, and GNOME with the Window Control extension and its
`wctl`. `revier popup` names whichever is missing. Refer to
[integrations/kitty.md](integrations/kitty.md) and
[integrations/gnome.md](integrations/gnome.md).

- A new popup is centred, at the full screen height and 1800 px wide. On a
  screen narrower than that, it fills the screen.
- The popup opens on the project of the window that had the focus when you
  pressed the key. A popup that is raised keeps its cursor where it was.
- Pressed while the focused window shows the popup, or a revier in a terminal
  of your own, the key switches between the projects and the agent list.
- Enter, or a target key, that brings a target or an agent to the front hides
  the popup. A failure stays on the screen for you to read.
- Esc with no query hides the popup. It keeps its state for the next raise.
  ctrl+c quits it.
- Each raise reads the configuration files again, so a project file you
  edited while the popup was hidden shows on the next raise.

### Refresh and focus

With the focus, the TUI refreshes once a second. Without the focus, the popup
or a revier in a terminal of your own refreshes every ten seconds, and at
once when the focus comes back. A hidden popup does not refresh until it is
raised. While a target you launched is still coming up, the TUI keeps the
fast refresh, hidden or not, because the refresh is what finds the new
window.

A host that a link lives on is asked at most once every five seconds. A host
that does not answer is asked less often, up to once a minute.

A revier in a terminal of your own reads the configuration files only when
it starts, and after the assistant (alt+a) exits. Changes you make in the TUI
itself apply at once.

## Dialogs

Each dialog takes the place of the list. Its name is on the top line, and the
pane stays beside it where it has something to show. Esc goes back one step.
ctrl+c quits.

### New project

alt+n, or **new**. The field takes one of three things:

- A full path, starting with `/` or `~`. Tab completes it, and ↑ ↓ choose
  among the folders under it. That folder becomes the project.
- A name. Enter lists the folders your projects already live in, the one
  that holds the most first, and the project goes in the one you choose.
- A git clone URL. As a name, with the repository's name. The repository is
  cloned there and the project opens. If the folder is there already, it is
  added as it is, and the URL is not recorded when it is not the folder's
  origin.

A folder that is not there is created, or cloned into, after you confirm with
Enter. The project name is the folder name. The dialog writes the same file
as `revier new`.

### Link a project on another machine

alt+r, or **remote**. Three steps:

1. The hosts that `~/.ssh/config` names, other than this machine. Enter asks
   the revier on that host for its projects.
2. The host's projects. Type to filter them. Enter on one goes to the next
   step. A project that is already linked is refused, with the name of its
   link.
3. The name of the link. revier offers `rs-<host>-<project>`. Type to
   replace it. Enter writes the link file, and the link is a row at once.

Refer to [links.md](links.md).

### The project screen

alt+e, or a click on the project name atop the pane. It shows the project's
file: its name, path and git URL, and every target the project has,
the ones from `config.toml` among them.

- ↑ ↓ move. Enter on a field edits it; Enter again writes it, Esc leaves it as
  it was.
- A change is written to the project file at once.
- A new name renames the file. A project with something running cannot be
  renamed.
- Enter on a target opens its form. Saving the form for a target from
  `config.toml` writes an override into the project file.
- alt+del on a target asks, and `y` removes it from the project file. A target
  that is not in the project file cannot be deleted here.
- For a link, the host and the project's name there are shown, not offered.
- Esc, or alt+e again, closes the screen.

### Sessions

alt+s, or **sessions**. The saved sets of open projects, newest first. The
pane says what a restore of the selected session would open now.

- Enter restores the selected session.
- alt+s on this screen asks for an optional name, and Enter saves the open
  projects under it.

Refer to [projects.md](projects.md) for sessions.

### Shutdown

alt+q, or **shutdown**. Choose every project or one, then, for one project,
what of it to close. The last step shows the plan. Enter on the plan saves the
session when it changed, then closes what the plan names. Confirming a plan
that names a busy agent closes it anyway. Esc goes back one step.

### Help

alt+h, or **help**. Every key the TUI answers to, grouped by what it acts on:
the TUI's own keys, the target keys, the configured actions, and the desktop
keys. ↑ ↓ scroll. Esc or alt+h closes it.

### Assist

alt+a, or **assist**. It hands the terminal to Claude Code, started in a
folder of its own and briefed to change the configuration and to find what is
wrong with it. It knows which project the cursor was on and what does not
load. It continues the conversation you held with it last; `/clear` there
starts a new one. When it exits, the TUI comes back and reads the files
again. Refer to [agents.md](agents.md).

## The config screen

alt+c, or **config**. Each change is written to `config.toml` and applied at
once. The comments in the file are kept. The line over the rows names the
file.

| Section | Rows |
|---|---|
| Appearance | **theme**, **glyphs**: ← → or Enter step through the values, and the TUI repaints. **trigger key**: Enter, type the key, Enter writes it. |
| Hosts | **runtime**: ← → step through `auto` and the runtime hosts. revier checks the host first and writes the choice only when the host answers. **window**: the window host found at start, shown and not offered. |
| Targets | The shared targets of `config.toml`. Enter opens a target's form; the last row adds one. alt+del asks, and `y` deletes. A delete that a project needs to load is refused, and the project is named. |
| Actions | The configured actions. Enter opens an action's form; the last row adds one. alt+del asks, and `y` deletes. |

In a target or action form, Tab, Shift+Tab, ↑ and ↓ move between fields,
Enter saves and Esc cancels. In a target form, the home flag changes with ←
→, and a panel opens its own form, where ← → change its kind and Enter keeps
it. The fields are the ones [configuration.md](configuration.md) describes.

## Status places

A `[[status]]` runs a command and shows what it prints at one place on the
screen. There are four places:

| Place | Where it shows |
|---|---|
| `rule-left` | the left end of the thin rule under the top line. Beside the pane, it stands over the list. |
| `rule-right` | the right end of the thin rule. Beside the pane, it stands over the pane. |
| `project-line` | the right end of the project name line atop the pane, for the selected project |
| `project` | a section of the pane, under its own title (`Info` when none is set), one segment a line, for the selected project |

Each segment has its own colour, from the level the command gave it. When a
place has no room for every segment, segments are left out from the right,
never cut in half. When a run fails, the place keeps the last good answer,
dimmed. A place shows nothing until its first run answers. While the popup is
hidden, nothing runs.

[Statuses](extending/status.md) has how to configure one.

## Quick reference

### Everywhere on the surface

| Key | Does |
|---|---|
| ↑ / ctrl+p, ↓ / ctrl+n | Move up, down |
| home, end | First row, last row |
| pgup, pgdown | A page up, down |
| typing | Filter the section the cursor is in |
| Enter | Act on the row: open a project, run-or-raise a target, go to an agent |
| Tab | Next section: the projects, the agents; from the targets, back to the projects |
| shift+tab | Previous section |
| alt+t | Move the cursor to the targets |
| Esc | Clear the query, then back to the projects, then quit or hide the popup |
| ctrl+c | Quit |
| alt+space, or the trigger key | Switch between the projects and the agent list |
| alt+e | Open the project screen of the selected project |
| del | Close the row; asks first when it ends an agent or a whole project |
| alt+del, alt+d | Delete what the configuration holds of the row, after a `y` |
| a target's key | Run-or-raise that target of the selected project |
| an action's key | Run that action against the selected project |

An `[[action]]` cannot take alt+space.

### Top line

| Key | Button | Does |
|---|---|---|
| alt+n | new | Add a project on this machine |
| alt+r | remote | Link a project on another machine |
| alt+s | sessions | Save and restore the set of open projects |
| alt+q | shutdown | Save the session when it changed, and close every project or one |
| alt+c | config | Change the configuration |
| alt+a | assist | Hand the terminal to Claude Code, briefed to configure revier |
| alt+h | help | Every key |
| — | ≡ | Show the buttons the line has no room for |

### Query editing

| Key | Does |
|---|---|
| ctrl+a | Start of the line |
| ctrl+e | End of the line |
| ctrl+w, alt+backspace | Delete a word |
| ctrl+u | Delete the line |
| del | Delete the character right of the cursor; at the end of the line, close the row |

### Agent list

| Key | Does |
|---|---|
| Enter | Go to the agent's tab |
| del | Close the agent's tab; asks first when it works or waits for you |
| typing | Filter by what the agent is on and by its project |
| wheel over the agent's terminal | Scroll back through it |
| Esc | Clear the query, else quit or hide the popup |

### Dialogs and screens

| Where | Key | Does |
|---|---|---|
| any dialog | Esc | One step back, or close |
| new project | Tab | Complete the path |
| new project | ↑ ↓ | Choose a folder |
| new project | Enter | Next step: choose the folder, create or clone, add |
| remote | Enter | List the host's projects, name the link, write it |
| remote | typing | Filter the host's projects |
| project screen | Enter | Edit a field and write it, or open a target's form |
| project screen | alt+del, then `y` | Remove the target from the project file |
| project screen | alt+e | Close the screen |
| sessions | Enter | Restore the selected session |
| sessions | alt+s | Name and save the open projects |
| shutdown | ↑ ↓, Enter | Choose, then confirm the plan |
| config | ← →, Enter | Change the value of the row |
| config | alt+del, then `y` | Delete a shared target or an action |
| target or action form | Tab, shift+tab, ↑ ↓ | Move between fields |
| target or action form | Enter | Save |
| help | ↑ ↓ | Scroll |
| help | alt+h | Close |
| delete question | `y` | Delete; any other key keeps it |

### Mouse

| Action | Does |
|---|---|
| Click | Select a row; run a button or the project name |
| Double click | Open the row, as Enter |
| Wheel | Scroll the pane or an agent's terminal; elsewhere move the selection |
| Drag | Select a box and copy its text on release |
| Shift+drag | The terminal's own selection, in kitty and most others |
