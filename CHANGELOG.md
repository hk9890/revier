# Changelog

What changed in each release, written for whoever runs `revier`. This is the
history readable from a checkout with no network.

Each section is also the text of its release on the
[GitHub Releases](https://github.com/hk9890/revier/releases) page.

## v0.23.0

- **Action required: `revier agent new` and `revier shell new` without `-p`
  now fail in a directory that is in no project.** Before, the project came
  from `--dir`, else the current directory. If neither was in a project,
  revier used the focused window's project, else the last project. So an
  agent could start in a project you did not mean. Now the project is the one
  that `--dir` is in. Without `--dir`, it is the one the current directory is
  in. A `--dir` in no project fails, even when you run the command inside a
  project. Such a directory is, for example, a worktree outside the project's
  directory. The command exits with status 3. Where a script or alias does
  this, add `-p <project>`, for example
  `revier agent new -p shop --dir ~/dev/shop-wt`. `--panel` does not change.
- **`revier rename <old> <new>` renames a closed project.** Before, only the
  project screen (alt+e) could rename a project. Now an agent or a script can
  do it too. The file, the state and the saved sessions move to the new name.
  The command refuses while any part of the project runs, including a launch
  that still waits for its window. It also refuses while a window is attached
  to the project, or while a host cannot say what runs. Do not move a project
  file by hand: the saved sessions keep the old name, and revier no longer
  finds the project's open windows.
- **`revier agent close <agent>` ends one agent.** Before, no command ended
  one agent, so a script typed `/exit`, and that worked only for Claude Code.
  Now the command closes the agent and returns when it is gone. It closes the
  agent's tab, and the shell in that tab, when the agent is in a tab beside
  the workspace's own tab. In the workspace's own tab, it closes only the
  agent's panel. It refuses while an agent in what it closes is `running` or
  in `attention`, unless you give `--force`. An opencode agent outside herdr
  shows `unknown`, so the command does not refuse it in a turn. It saves no
  session.
- **`revier list --agents` prints one row per agent, with its address.** Each
  row shows the address that `revier agent` takes, the agent's name, status,
  activity, harness and directory. With `--json`, each agent is one flat
  object. Before, you had to build the address from `revier list --json`. An
  agent that no target holds has no address, so its row names its project
  and panel.
- **`revier list --match <word>` lists only the projects that a word
  finds.** The word can be part of a project's name, the name of its
  directory, its repository, its name on a link's host or an agent's
  activity. Case does not matter. It can also be the full name the harness
  gives an agent. `--match` takes no project names. When nothing matches,
  the command fails.
- **A terminal agent key can now start an agent in a window of no
  workspace.** Before, no value of `[terminal] outside` started an agent.
  With `tab`, the agent keys opened the same plain shell tab as the shell
  key. With `none`, they did nothing. Now `outside = "agent"` opens a tab in
  the window's directory that runs the agent the key asks for. A named
  agent's key runs that agent. `agent_key` runs the first `[[agent]]` of
  `config.toml`, and with no `[[agent]]` it opens a plain tab. An agent whose
  `command` uses a template opens no tab there, and the error names it. The
  shell key still opens a plain tab, and `tab` and `none` do not change. This
  works in kitty and tmux.
- **In tmux and herdr, a tab opened beside a pane starts in that pane's
  directory.** Before, `revier shell new --panel <id>` without `--dir`, and
  the plain tab of `outside = "tab"`, did not start in the pane's directory.
  In a workspace, the shell tab started in the project's directory. Outside a
  workspace, the plain tab started where the runtime put it. Now they start
  in the pane's directory, as in kitty.
- **`revier list` with an unknown name still lists the names it knows.**
  Before, one unknown name in `revier list a b` stopped the command, and it
  printed nothing. Now it lists the known projects. Each unknown name gets a
  line on stderr, with "did you mean ...?" when a project name is near. The
  exit status is still 1.
- **`revier agent wait` on a panel with no agent yet waits for the agent to
  start.** Before, on such an address, for example the one that
  `revier agent new --no-focus` prints, `wait` failed at once with "not an
  agent panel", and `--timeout` had no effect. Now it waits for an agent to
  start, within the same `--timeout`, then for the status. Without
  `--timeout`, `wait` on a panel where no agent starts waits for good.
- **The tmux keys open their tab where kitty is also installed.** Before,
  `revier agent new --panel "#{pane_id}"` and `revier shell new --panel
  "#{pane_id}"` failed with "no open target holds panel" when kitty was the
  first runtime that worked. Now a tmux pane id makes these commands use
  tmux, if `[hosts] runtime` names tmux before `none` or you set no list.
- **On a link, the project's address finds the default agent beside named
  agents.** Before, `<project>` and `<project>:<target>` were ambiguous on a
  link when two or more agents ran there, even when all but one were named
  agents. Now the address names the default agent, as it does on this
  machine. The project's machine must run v0.23.0. This works only for
  agents started after the upgrade.
- **A rename no longer refuses a project whose name is part of
  `jetbrains-idea`, such as "a", "idea" or "jetbrains".** Before, the
  project screen refused to rename such a project when `revier new` had
  written its file. Now the editor's window class in that file does not block
  the rename.

## v0.22.0

- **Action required: an agent address now has three parts,
  `<project>:<target>:<panel>`, and `revier agent focus --ref` is removed.**
  Before, `<project>:<panel>` failed when two kitty processes held one
  project's windows, and some agents had no address that worked. A script that uses `<project>:<panel>` must use the new form.
  The agent pane, `revier list --json`, `revier agent new --no-focus` and
  every "which agent?" error print it. `<project>` and `<project>:<target>`
  still name the default agent, now on a link too. Where you used
  `agent focus --ref`, give the address.
- **Action required: drop `--dir` from the tmux agent key.** If you copied
  the tmux binding from the guide, change it to
  `bind-key A run-shell -b 'revier agent new --panel "#{pane_id}"'`. With
  `--dir`, the agent still starts in the worktree.
- **Action required: an `[[action]]` on alt+l must move to another key.**
  alt+l now opens the logs screen, so `config.toml` with an action on it no
  longer loads.
- **A terminal key's agent tab starts in the project.** Before, ctrl+shift+z
  in an agent that works in a worktree started the new agent in that
  worktree, so it could enter no other worktree. A shell tab still starts in
  the panel's directory.
- **A logs screen.** alt+l, the new "logs" button in the top bar, or a click
  on the footer's error opens a list of this machine's warnings and errors,
  newest first. Enter shows a line in full. The footer now names only what
  failed; the reason is on the logs screen.
- **The remote screen lists your machines.** Before, alt+r and `revier link`
  listed every host of `~/.ssh/config`. Now they list the new `[[machine]]`
  tables of `config.toml`, so the list is empty until you add a machine:
  alt+n on the remote screen adds a host of the ssh configuration, and alt+d
  removes one. Both keep the comments in `config.toml`. Each row shows that
  machine's revier version, "no revier" or "does not answer". A link to a
  machine that is not on the list still works, and `revier doctor` names it.
- **A link shows a link icon.** In the project list and on the remote screen,
  a link now has the link icon, not the server icon. A host's projects list
  the linked ones first.
- **An agent row shows the agent's name.** Under its age, a row in the agent
  list and in the project pane shows the name the harness gives the agent,
  for Claude Code the name its peer messages use. The pane also shows the
  agent's address. An exec probe can report the name in a new `name` field.
- **revier no longer picks a herdr server it cannot drive.** Before, a running
  herdr server older than 0.9.5, or one that did not answer, was chosen, and
  every key then failed. Now revier goes on to the next runtime of
  `[hosts] runtime`.
- **herdr: revier finds a panel's program as herdr does.** Before, revier
  started from a desktop key refused a program that a herdr server started
  from a login shell could run, and let through one that herdr could not
  find.
- **A tab whose command exits at once says so.** Before, a tab whose only
  command exited right after it opened failed with "can't find pane", which
  named neither the panel nor its command.
- **A failed clone says why git failed.** Before, the error was only
  "exit status 128". A clone onto a terminal now shows git's progress.
- **del on a project whose window another project holds names that project.**
  Before, the footer said "nothing of it is open here" while the row showed
  the window open.
- **A config or state file is no longer left broken by a crash during a
  write.**

## v0.21.0

- **A restore no longer loses agents to a smaller save.** Before, a plain
  restore opened the newest save, so after you closed one project, or deleted
  a few, it opened only what was left. Now a plain restore, the first entry of
  the sessions screen and the start dialog take the newest save of the whole
  set: one saved by hand, by a full shutdown, or after a crash. A save made
  when you close a project is still listed, and you open it by its id.
  `revier session list` ends with the id a plain restore opens.
- **A crash no longer loses what was open.** While the TUI runs, revier
  remembers what it shows. When revier ended without a shutdown, for example
  because kitty was killed, the next start offers what was open as a
  `last-seen` session. `revier session restore` offers it too.
- **Named agents.** An `[[agent]]` in `config.toml` names a program that an
  agent tab runs, such as a second agent beside Claude Code. Give it a `key`
  for its own terminal key. `revier agent new <name>` opens one, and a
  restore brings it back as that agent, not as the default one. A project
  file can change an agent's command or add an agent of its own.
- **`revier term install` writes your terminal keys into kitty.** It writes
  `revier.conf` in kitty's configuration directory and adds one `include` at
  the top of `kitty.conf`, also through stow links. You no longer write the
  maps by hand. The keys come from the new `[terminal]` table (`shell_key`,
  `agent_key`) and from each agent's `key`. `revier term status` shows them,
  and `revier term uninstall` removes them.
- **A terminal key in a window of no project opens a plain tab.** Before,
  revier refused the key there. Set `[terminal] outside = "none"` to make it
  do nothing.
- **A terminal key works in a panel whose directory was removed.** Before,
  ctrl+shift+t and ctrl+shift+z did nothing in a panel of a deleted worktree,
  in kitty and with the tmux keys of the guide. Now the tab opens in the
  project. In a worktree that still exists, the new tab still opens in that
  worktree.
- **`revier state` shows what revier had open at a moment.** `--at <time>`
  goes back in time, and `--session <id>` explains why a save differs from
  what was open when it was saved. The record it reads is kept under
  revier's state directory.
- **Shutdown says when it is safe to leave.** While the TUI shuts down, it
  says to wait, and how many of what it closed are still open. Then it says "Shutdown complete", and
  `revier shutdown` prints "shutdown complete; safe to leave". Every restore in
  the TUI now ends on the project list, with the TUI raised. What did not
  open is named in the footer.

## v0.20.0

- **Action required: on GNOME, revier needs `wctl` 0.16.0 or later.** revier
  uses GNOME only when `wctl` is 0.16.0 or later and a Window Control
  extension runs. With an older `wctl` and no `[hosts] window`, revier now
  runs with no window host and says nothing: no window targets, no raise, no
  return home, no `place` and no popup. With `[hosts] window = ["gnome"]`, commands stop with
  `no configured window host is usable: gnome: wctl X is older than 0.16.0, the oldest revier drives`.
  To fix it, update the Window Control extension and `wctl` to 0.16.0 or
  later, then log out and in. Then check that `wctl version --json` shows
  `wctl` 0.16.0 or later and an `extension` version.
- **Action required: a script that reads `revier session list` by position
  must read NAME from the third column.** REASON is now the second column,
  after ID, so NAME and every column after it move one place to the right.
- **herdr is a new runtime host.** Before, revier ran workspaces in kitty or
  tmux only. Now revier can open each workspace as a herdr workspace, its tabs
  as herdr tabs and its panels as panes, and it reads every pane to find
  agents. You need herdr 0.9.5 or later on your PATH. revier uses the server
  that `herdr` would attach to, and starts it when none runs. The default
  runtime order is now kitty, tmux, herdr: a machine with only herdr uses it.
  When kitty or tmux is installed, set `[hosts] runtime = ["herdr"]`.
  `revier open --attach` and tab targets work with herdr, and `revier doctor`
  knows herdr. So that `revier session restore` resumes the agents and herdr
  does not, set `resume_agents_on_restore = false` under `[session]` in
  herdr's own `config.toml`. Some things do not work with herdr:
  - A focus moves every terminal that is attached to herdr. Attach one
    terminal.
  - revier does not raise the desktop window that shows herdr, a second press
    does not return home on GNOME, and `place` does nothing.
  - While you scroll a pane up, the TUI and `revier agent read --screen` show
    those lines, not the newest.
  - When the herdr server starts, for example at the first open after a
    reboot, herdr brings its workspaces back as shells that revier does not
    know. revier then opens a second workspace or tab. Run `herdr`, close
    those workspaces, then run `revier session restore`.
  - The popup is a kitty window. With herdr and no kitty, run `revier` in a
    terminal.
- **In herdr, agents other than Claude Code show a state.** Before, only
  Claude Code and agents with a `[[probe]]` of your own showed a state, and
  opencode was always `unknown`. Now, in herdr, an agent such as Codex, Gemini
  or opencode shows the state that herdr detects: working, needs you, idle, or
  unknown. A `[[probe]]` that you declared for that agent still wins. Outside
  herdr, opencode still shows no state.
- **The TUI and `revier shutdown` show each step of a shutdown or a restore
  while it runs, and ctrl+c stops between steps.** Before, a shutdown or a
  restore could run for a minute and showed nothing until it ended: the TUI
  showed one line, and the CLI printed one table at the end. A host that hung
  on one step looked like a hung revier. Keys and clicks in the TUI still acted
  on a half-changed desktop, and ctrl+c killed `revier shutdown` or
  `revier session restore` in the middle of a step. Now:
  - The TUI shows each step of a shutdown, a close or a restore, and its
    result. No other key or click does anything until it ends.
  - `revier shutdown` and `revier session restore` print one line per step:
    its name before the step, its result after it. A hung step is the last
    line.
  - ctrl+c lets the running step finish, then stops. revier counts and lists
    the steps it did not reach, and the TUI stays open to show the result. A
    restore stopped after its last target says `return home cancelled`. A save
    stopped by ctrl+c writes no incomplete session. An alt+del stopped
    during its last close still deletes the entry.
  - When a step hangs, a second ctrl+c quits at once and also stops the
    command that the step waits for, such as kitten, tmux, ssh, `wctl` or
    claude.
- **After a full shutdown, the next start offers to restore it.** Before,
  the next start offered nothing, the restore was three keys deep on the
  sessions screen, and the TUI stayed open after the shutdown. Now revier
  remembers a shutdown of all of every project. The TUI and the popup show the
  result for about 1.5 seconds, then quit; any key quits at once. The next
  start of the TUI or the popup opens on a dialog, for example
  `revier was shut down at 18:30.`, with Restore this session, Pick another
  session, and Start fresh (or Esc). A TUI that stays open, or a hidden popup,
  while `revier shutdown` runs in another place shows the same dialog at its
  next refresh or raise. The offer ends when you open something first, with a
  desktop key, `revier go`, `revier open` or `revier run`, and when
  `revier session restore` starts. revier does not remember the shutdown, and
  the TUI stays open, when:
  - you shut down one project, or closed with del;
  - the shutdown found nothing to close;
  - revier could not write what it remembers;
  - you opened something while the shutdown ran;
  - you stopped it with ctrl+c before its last step. A ctrl+c after its last
    step started keeps it.
- **`revier session list` shows why each session was saved.** Before, a
  session without a name showed a dash on the TUI sessions screen and a blank
  NAME in `revier session list`, and you could not tell the saves of a
  shutdown, a project close and a save by hand apart. Now
  `revier session list` has a REASON column: `shutdown`, `project closed` or
  `saved by hand`. The TUI sessions screen shows the reason where a name is
  missing. A session that an older revier saved shows `unknown`. NAME is still
  the value that `revier session restore` accepts.
- **The shutdown button moved to the sessions screen.** Before, the top line
  of the TUI had a shutdown button. Now the sessions screen (alt+s) has the
  save and shutdown buttons on its top line, and alt+q there opens the
  shutdown. alt+q in the project list still opens it, and Esc goes back to
  where you opened it.
- **A restore no longer brings each window that it opens to the front, and
  the TUI comes back to the front when the restore ends.** Before, a restore
  raised every window that it opened, so the desktop flipped through each
  editor and browser, and the last window hid the result of the restore. Now
  revier still finds and places each window, but does not raise it. GNOME
  itself still shows most new windows over the TUI. A terminal that a restore
  opens still gets the focus, so its tabs come up as saved. When the restore
  ends (done, failed or cancelled), the TUI or the popup brings its own window
  to the front once. A TUI in a terminal that revier does not list stays where
  it is. A normal key press still raises the window that it opens.
- **A `[[probe]]` named `opencode` now runs.** Before, revier ran its own
  opencode probe before your `[[probe]]` entries, so a `[[probe]]` named
  `opencode` never ran. Now the Claude Code probe runs first, then your
  `[[probe]]` entries in file order, then the probes that come with revier.
- **A press that opens a window, and each step of a restore, end sooner.**
  Placing each window takes about 150 ms less, and revier finds a new window
  sooner.
- **A new terminal window is placed also when another host does not
  answer.** Before, when one other host, such as tmux or herdr, could not
  list what it runs, revier left the new terminal window where it opened.
- **A del or alt+del question from before the popup hid no longer answers
  itself with your next key.** Before, when you pressed del or alt+del in the
  popup just before it hid, for example while the window that Enter opened
  came up, the popup could hide with the question still open. The next raise
  showed that question, and the first key answered it: Enter closed the
  project, and after alt+del also deleted the project file. Now a raise drops
  a question from before the hide. Press del or alt+del again. When you press
  a key, click or turn the wheel before the window comes up, the popup now
  stays on the screen.
- **`revier open --attach` in a tmux pane no longer ends with an error.**
  Before, in a pane of the tmux server that holds the workspace, the command
  switched the pane to the workspace and then printed
  `sessions should be nested with care, unset $TMUX to force`. Now it switches
  the pane, prints the workspace, and exits 0. A terminal that you started
  from a tmux pane still attaches.

## v0.19.0

- **A wrong `[ui]` value or `[[probe]]` in `config.toml` no longer stops every
  command.** Before, one such mistake stopped every command that reads the
  projects: the TUI, the desktop chords and `revier popup`. `revier doctor`
  showed only that one error. The mistake could be a `[[probe]]` without
  `name` or `exec`, an unknown `theme` or `glyphs`, or a `trigger_key` that
  revier cannot read. Now only the wrong value is affected:
  - An unknown `theme` or `glyphs` uses its default.
  - A `[[probe]]` without `name` or `exec` does not run.
  - A `trigger_key` that revier cannot read stops only `revier keys status`,
    `install` and `uninstall`. They do not bind alt+space in its place, and
    the TUI help shows no trigger key.

  `revier doctor` names each mistake under the `config.toml` heading and
  exits 1. Correct the value. revier still stops every command when
  `config.toml` is not valid TOML, or when a value has the wrong type, such as
  `theme = 3`.
- **The config screen (alt+c) shows the problems of `config.toml`.** Before,
  the TUI showed none of them. The one warning line on stderr disappeared when
  the TUI started, and only `revier doctor` named the problems. Now each
  problem shows above the rows of the screen, and a long problem wraps. A
  problem can be an unknown theme or glyph set, a trigger key that revier
  cannot read, an action, shared target or `[[status]]` that cannot be used,
  or a `[[probe]]` without `name` or `exec`. A theme name with a typo now
  shows as the default theme, and this screen tells you why. If `config.toml`
  stops being valid TOML while the TUI runs, the screen shows the path and the
  reason the next time you open it. The screen reads the list again when it
  opens, after each change you make on it, and when the TUI reads the files
  again without an error. When you choose a valid theme on the screen, its
  problem goes away.
- **An agent whose Claude Code conversation is gone starts empty, and revier
  tells you.** Claude Code keeps no conversation for an agent that never got a
  prompt. It also deletes conversations older than `cleanupPeriodDays`, which
  is 30 days by default. Before, a restore resumed such an agent anyway.
  Claude Code stopped at once with `No conversation found with session ID`,
  the window showed a failed command, and the restore counted the agent as
  resumed. Now the agent starts empty in the directory where it worked. The
  restore summary adds, for example, `2 agents empty: conversation gone`. The
  sessions screen shows `empty: its conversation is gone`, also in the plan
  before you restore. `revier agent new --resume <id>` does the same check,
  and so does the agent of a linked project when its host runs this version.
  Both print `revier: warning: <id> was not resumed (conversation gone)` and
  then tell you that the agent starts empty. For the agent of a linked
  project, only that warning on the host tells you: the restore summary and
  the sessions screen still count it as resumed.
- **A double click in the TUI opens only the row that you clicked two times.**
  Before, a refresh between the two clicks could move another project or agent
  into the place that you clicked. For example, a project that needs you moved
  to the top, or an agent closed and the rows below it moved up. The second
  click then opened that other row, which you had clicked only once. This was
  also true for the agent and target rows of the pane and for the rows of a
  screen. Now a double click counts only when the same project, agent or
  target is still in the same place. If the row moves, the second click is a
  new single click.

## v0.18.0

- **Action required: a target with a key cannot be named `popup`.** The
  trigger key, the chord that opens `revier popup` (alt+space by default), is
  now named `revier: popup`, not `revier: picker`. A target that has a `key`
  and is named `popup` or `picker` is refused at load, and `revier doctor`
  names it. Rename the target. Then run `revier keys install` once. It renames
  the old `revier: picker` entry in GNOME Settings and prints `renamed` for
  that row. `--dry-run` shows the rename too. A shortcut that you bound to
  revier by hand keeps your name. `revier keys status`, `install` and
  `uninstall` name the row `popup`, also in `--json` (`"target": "popup"`,
  before `"picker"`). Change a script that reads the old name.
- **Action required: a panel `kind` must be `agent`, `shell` or `tool`.**
  Before, revier accepted any value without a message. A typo such as
  `kind = "agnet"` made the panel a `tool`, so a restore did not resume its
  conversation, a link did not fill its command, and `revier agent new` did
  not copy it. Now revier refuses only that target, and `revier doctor`
  shows `target "<name>": panel <n> has kind "<value>"; use agent, shell or
  tool`. Correct the value. A panel with no `kind` is still a `tool`.
- **Action required: an unknown name in `[hosts]` makes `revier doctor`
  fail.** An unknown name in `hosts.runtime` or `hosts.window` is reported
  under the `config.toml` heading, with the known names, and doctor exits 1.
  This also applies to a name after a host that works, or after `none`, which
  no command reported before. Correct the name or remove it from the list.
  Doctor does not check if a host works.
- **A user guide is on
  [GitHub](https://github.com/hk9890/revier/tree/main/docs/user-guide).** It
  has pages for getting started, concepts, the TUI, projects, agents, links,
  configuration, CLI commands, troubleshooting, statuses and agent probes. It
  also has one page each for kitty, tmux, Ghostty, GNOME, Claude Code and
  opencode. `revier assist --reference` now prints the configuration, status
  and probe pages of the guide, so the assistant also gets the status and
  probe formats.
- **revier sees an agent in a tmux pane that an interpreter runs.** Before,
  revier showed no agent state for an agent that started as, for example,
  `node .../bin/claude`, and an agent typed into a shell pane showed as
  `unknown`. Now, in tmux as in kitty, the agent shows its state in the TUI,
  the agent list and probes, and a session save records it. An external probe
  gets the whole command line and the foreground pid of a tmux panel.
- **`revier <command> --help` prints that command's usage and exits 0.**
  `-h` and `-help` do the same. Before, most commands printed a different
  usage text and exited 1, and some were refused or contacted the hosts.
  Help opens no window and contacts no host. `revier link help` and
  `revier <family> help` print the usage. `revier help` now names every
  command and flag. `revier agent help` says that `agent prompt` also
  refuses an agent whose state is unknown, as an opencode agent always is.
  For such an agent, use `agent send-keys`. An unknown flag is reported
  once. Each word after `--` is text, also a word that starts with a dash.
- **The agent mirror shows the whole screen.** v0.17.0 cut Claude Code's
  screen above its input box. When Claude Code drew something under the box
  or in its place, such as the background-agent selector or the workflow
  view, the cut was in the wrong place. Now the mirror shows the agent's
  whole screen, the input box included.
- **`revier each log` prints a command that you can paste into a shell.**
  Each word of the command and the filter is quoted as a shell reads it.
  Before, `sh -c 'echo a b; true'` showed as `sh -c echo a b; true`, and a
  filter that held a single quote showed incorrect quotes.
- **`revier session list` and the sessions screen count every saved
  agent.** Before, they counted only agents with a conversation, so a
  session could show `0 agents` and then restore three. Session files do
  not change.
- **A restore starts a Claude Code agent in the directory where it
  worked.** Before, a restore started a Claude Code agent that had no
  conversation yet in the project directory. An agent with a conversation
  resumes it as before.
- **The tmux host names the command that does not start.** Before,
  `revier open` failed with a raw tmux error such as `no such session: $0`.
  Now a `launch` or panel command whose program is not on PATH is refused
  before tmux starts, with `the <kind> panel's command "<prog>" is not on
  PATH`, or `the command "<prog>" is not on PATH` for a launch or a panel
  with no kind. A command that exits at once gives `... command "<cmd>"
  exited as soon as it started`. A program given as a path is not checked in
  advance. Install the command, put it on PATH, or correct the command.
- **A target that no host can open is named in the error.** `revier go`
  printed only `no host can realize this target`. Now the message names the
  target, its project and the kind of host that is missing, for example
  `target "editor" of project "myproj" has only a window realization, and
  no window host is configured or available`.
- **`revier doctor` gives better advice.** In a link project, doctor tells a
  target with no realization to add a `[target.remote.window]` or
  `[target.remote.runtime]` table. Before, it told you to add a
  `[target.window]` table, which a link project refuses. A value of the wrong
  type is advised on its type, not on TOML syntax. A refusal such as
  `action "c" runs nothing` prints once, under its name, not two times. A
  doctor run that finds problems is no longer written to the log as an
  error.

## v0.17.0

- **alt+a raises the config assistant beside the surface.** Before, alt+a
  handed the surface's terminal to Claude Code until it exited. Now it raises
  `revier-config-assistant`, a project that revier builds in: Claude Code in a
  workspace of its own, briefed to change the configuration and to find what
  is wrong with it. Its row has a cog where other rows have a folder. Like any
  other project, it shows in the agent list, a saved session restores its
  conversation, and a second alt+a raises the same window. It continues the
  conversation you held with it last, also the one from the old hand-over. A
  second agent there, while the first one runs, starts a new one. It is not
  told which project the cursor is on, so name the project. The surface reads the files again when it gets
  the focus back. A project file with the same name replaces the built-in
  project, and the built-in project comes back when that file goes. Another
  machine's built-in project is not offered as a link. With no runtime, alt+a
  hands the terminal over as before. When the assistant cannot start, its
  window stays open and shows why until you press Enter.
- **A `[[status]]` shows a command's output on the surface.** A `[[status]]`
  table in `config.toml` runs a command and shows what it prints at one of
  these places: an end of the rule under the top line, the end of the pane's
  first line, or a section of the pane for the selected project. Examples are
  an account's usage limits or a CI result. A project file adds its own status
  to the pane, or changes a shared one. The command can mark its output as a
  warning or as critical. When a run fails, the last answer stays, dimmed.
  `revier doctor` names a table that it refuses. `revier assist --reference`
  prints the format.
- **The pane shows each tab under the target it opens inside.** Before, the
  targets were listed in the configuration order, so a tab looked like a window
  of its own. Now a tab is drawn on a branch line under its container, and a
  glyph marks each window and each container.
- **The agent mirror is easier to read.** A line that is wider than the pane
  now continues on the next line at a word, not in the middle of a word, and
  keeps the indent of its list item. A rule is shortened to fit the pane.
  Claude Code's input box and status lines are left out, so the pane shows more
  of the conversation. `revier agent read --screen` still prints the whole
  screen.

## v0.16.0

- **The surface no longer keeps a CPU core busy.** A TUI or popup left on
  the screen used 60 to 70 percent of a core all the time, with or without
  the focus. With the focus it now uses about 10 percent. In a terminal
  that reports the focus, such as kitty, or tmux with `focus-events` on, a
  surface without the focus refreshes every ten seconds, the working
  spinner stands still, and the hover does not follow the pointer. When
  the focus comes back, it refreshes at once. A launch that is still coming
  up keeps the one-second refresh until its window appears, for at most a
  minute.
- **Enter in the popup hides the popup.** A press that brings a target or an
  agent to the front takes the popup off the screen; the popup key brings
  it back with fresh rows. A press that fails leaves the popup on the screen
  to show why. If you hide and raise the popup again before a slow launch
  lands, the popup stays.
- **A linked host is no longer asked every second.** Before, a linked host
  spent a third of a core answering each surface. Now it is asked at most
  once in five seconds, and a press, or the focus coming back, asks a host
  that answers at once. A host that does not answer is asked less often
  after each round it misses, up to once a minute, and a press or the
  focus that comes back does not ask it again before then. A round to a
  host that stops answering ends after about ten seconds, not after more
  than a minute.
- **A link to the machine revier runs on is left out.** The project files
  are shared between machines, so a link that one machine writes to another
  is read on that other machine too, where it names the machine itself.
  There, revier asked itself over ssh every second for what it lists
  already. Such a link is now left out of every command on that machine,
  by the host's name or by the HostName in `~/.ssh/config`. Only the part
  of a name before the first dot counts, so a link to a machine with the
  same short name in another domain is left out too. `revier doctor` names
  each file it skipped. `revier link` and alt+r do not offer this machine,
  and refuse it as a host.
- **`revier link` and alt+r list a host whose `~/.ssh/config` line uses a
  tab.** A `Host` or `Include` line that separated the keyword from its
  value with a tab was not read.
- **A ctrl+shift target key works inside revier in kitty.** In a terminal that
  reports key disambiguation, such as kitty, pressing ctrl+shift+o in the
  surface runs the target bound to it, as the desktop key does outside, and
  ctrl+o keeps its own meaning. Before, the press arrived as ctrl+o, so a
  target key whose ctrl key the surface used did nothing there. In other
  terminals, keys work as before. The help screen and the footer name the
  ctrl+shift keys that now work.
- **With `NO_COLOR` set, the selected row is bold and a text field shows its
  cursor.** Colour stays off, for any value of `NO_COLOR` that is not empty.
  Before, the surface drew no emphasis at all, and the cursor was invisible.
- **A session saved by v0.3.0 restores without its conversations.** It
  opens its projects as before but resumes no agent conversation. Renaming
  a project that such a session holds rewrites the session, and its
  conversations are then gone for good.

## v0.15.0

- **Action required: `panels` are on a tab target only.** A project file or a
  `config.toml` with `[[target.runtime.panels]]` on a target that has no
  `inside` is refused at load, and `revier doctor` names each file with the
  fix. Give the target the line `tabs = ["agent"]` in its `[target.runtime]`,
  and move the panels into a new target:

  ```toml
  [[target]]
  name = "agent"
    [target.runtime]
    inside = "home"   # the name of the target the panels were on
      # the [[target.runtime.panels]] tables go here, as they were
  ```

  A `dir` on the target moves with the panels: a tab starts in its own `dir`.
  A machine that a link reaches must have this version and its project files
  in this form too, and so must a link file whose home declares `panels`. A
  link home with a window part and a runtime part that has no `launch` is
  refused as well: give the runtime part a `launch`, or remove one of the two
  parts. A session saved before the change restores after it. `revier new`
  writes the new form, and `revier assist --reference` prints a whole file in
  it.
- **A workspace opens with a list of tabs.** `tabs = ["tickets", "agent"]` on
  the runtime of the home target is the order, and `active = "agent"` is the
  tab that has the focus. Each name is a target with `inside = "home"`, which
  holds `panels` or a `launch`. The key of a listed tab goes to that tab and
  opens no second one, also after the first panel of the tab has ended. A
  workspace that is open already keeps its tabs as they are.
  `revier agent new`, `revier shell new`, a save and a restore take the agent
  and the shell from the listed tabs, so the conversation in the `agent` tab
  is resumed. A linked project opens with one tab, `agent`, which holds its
  agent and its shell.
- **The target form has `tabs` and `active`.** The config screen and the
  project screen can make the target that lists its tabs, and change the
  order and the tab that has the focus. A target that lists a tab is saved
  after the tab exists.
- **A refusal on a linked project names a fix that works there.** Panels on a
  target of a link go into a tab of its home, and panels on the home go into
  the tab `agent`. A link whose home has the name `agent` is refused for the
  name. `revier new` is refused beside a shared target named `agent` that is
  no tab. The project screen refuses `tabs` and `active` on a link, where it
  dropped them with no message.
- **The mouse works after an action or a clone.** After a key ran an action,
  or Enter cloned a missing project, a click did nothing, and a drag selected
  text that was lost at once while an agent worked.
- **Fast pointer motion no longer types into a filter.** Text such as
  `<35;76;40M` appeared in the project filter with no key pressed, and the
  list then matched nothing.
- **A raised popup no longer shows the states of the moment it was hidden.**
  The list stopped refreshing when the pointer was over the popup at the
  raise, so a closed project stayed listed as open until the next Esc and
  raise.
- **The log says how a surface ended when its terminal closed.** The
  `command` line of the process has `signal`, and the process exits with 128
  plus the number of the signal. Before, a surface whose terminal closed
  wrote no line.
- **A desktop with no focused window is no error on GNOME.** `revier attach`
  says `no window is focused`, where it printed a JSON error, and the popup
  key writes no `resolve: focused window` warning to the log. The
  `popup: launched` line has the id of the window the key was pressed over,
  as `over`, and an empty one when no window had the focus.
- **The pane beside the project list shows the terminal of one agent, as the
  pane beside the agent list does.** The section is headed `Screen`, follows
  the agent each second in its own colours, and the wheel scrolls back
  through it. It showed the agent's last message, set again from Markdown,
  with the turn above it; both are gone. An agent of any harness is shown
  now, and so is an agent of a linked project in a terminal here.
- **The pane no longer puts an agent with a message before one without.** It
  shows the agent that needs you, else one at rest before one working, and
  the last to speak among equals. It keeps that agent until another needs you
  more, so an agent that speaks does not take the screen from its equal.

## v0.14.0

- **The pane shows the turn an agent is in, above its last message:** your
  last prompt, the tools the agent called since with the failures counted,
  and the call it is running or waits on you for. The section is headed
  `Last turn`. A Claude Code agent that has called tools and said nothing yet
  now shows them, where the pane said nothing could be read.
- **Action required for an out-of-tree probe that compares an `AgentDetail`
  with `==`:** the type holds a slice now, so the comparison no longer
  builds. Call `IsZero` in its place.
- **`revier agent read` prints what an agent said last.** With `--screen` it
  prints the text of the agent's panel, and `--screen --lines <n>` prints the
  last n lines of the panel and its scrollback. Both print plain text, with
  no escape sequence in it. The last message needs an agent on this machine
  whose harness keeps a conversation revier can read, which Claude Code
  does; `--screen` works for every harness and for a linked project.
- **`revier agent send-keys` types named keys into an agent, whatever it is
  doing.** It answers the dialog that `revier agent prompt` refuses, and `esc`
  interrupts a turn. The keys are `esc`, `enter`, `tab`, `shift+tab`, `space`,
  `backspace`, `up`, `down`, `left`, `right`, `ctrl+c`, or one character. One
  unknown name types none of them.
- **`revier agent new --no-focus` adds an agent and leaves the focus where it
  is.** It raises no window and prints the address of the new agent,
  `<project>:<panel>`, which `agent wait`, `prompt`, `read` and `send-keys`
  take once the harness in the tab has started. Under kitty, a kitty OS
  window that does not hold the keyboard still shows the new tab.
- **Action required when `XDG_RUNTIME_DIR` is not set, under kitty:** revier
  starts no kitty for a workspace and says so. It started one. Set
  `XDG_RUNTIME_DIR` to an absolute path, which a login session does. A
  workspace in a kitty that revier already reaches still opens, and so does
  the popup.
- **Action required for a script that reaches a kitty revier started:** the
  control socket is `unix:$XDG_RUNTIME_DIR/kitty-<pid>`. It was
  `unix:@kitty-<pid>`, and for the popup's kitty the one `listen_on` in
  `kitty.conf` names. Pass the new address to `kitten @ --to`.
- **A kitty that revier starts listens on a socket file only you can reach.**
  It listened on the abstract socket `@kitty-<pid>`, which every user of the
  machine can connect to, and through it read and type into each kitty
  window. The popup's kitty gets a socket file of its own, and without
  `XDG_RUNTIME_DIR` it listens where `kitty.conf` says.
- **revier finds a kitty that `kitty.conf` puts on a socket file.** Set
  `listen_on unix:${XDG_RUNTIME_DIR}/kitty` to close the kitty windows you
  start yourself to other users too. A kitty on `@kitty-<pid>` is still found
  in this release, and no longer in the next.
- **A command acts on the state as it is when it acts.** `revier open`,
  `agent`, `session` and `shutdown` used the state read when the command
  started, so a window another process bound in the meantime was not found by
  its binding.
- **A project can be renamed or deleted 5 seconds after one of its actions
  ran.** The surface refused both for 60 seconds, the wait that belongs to a
  target's window.
- **A state file that cannot be read no longer stops a command.** The command
  runs with nothing remembered and the log says why, as the surface does.
- **A theme change repaints the query of the agent list and the name field of
  a session.** Both kept the colours of the theme before.
- **A folder that appears while the surface asks to clone into it is added as
  it is, and the footer says the clone URL is ignored.** Nothing was cloned
  and nothing said so: the project recorded the URL as its repository and was
  opened as if it were cloned.
- **A press and `revier list` say `could not save state` on stderr when the
  state file cannot be written.** Only the log said it; `revier attach` and
  `revier run` already did.
- **Action required for an out-of-tree runtime adapter:** a runtime's
  `Capabilities` no longer have the fields `Layout` and `Persistent`. revier
  never read them. An adapter that sets them no longer builds: delete the two
  fields from what it returns.
- **Action required for a `[[probe]]` script that reads `kind`:** under tmux,
  a pane that runs claude, opencode or aider reaches the script with the kind
  `tool`, as it does under kitty. It was `agent`. A pane that holds a `dash`,
  a `ksh` or a login shell alone has the kind `shell`. It was `tool`. Match on
  `command` in its place.
- **An agent started from a login shell, a `dash` or a `ksh` is found under
  kitty.** The shell was taken for the pane's program, so the agent was not
  listed, saved or counted by a close.
- **`revier agent prompt` says that a pane holds a shell for `dash`, `ksh` and
  a login shell too.** It refused such a pane with `no probe recognises what
  runs in it`, which sent the reader to the probes.
- **revier knows `ash`, `mksh`, `tcsh`, `csh`, `nu`, `elvish`, `xonsh` and
  `pwsh` as shells.** What the three entries above say of a `dash` and a
  `ksh` holds for each of them.
- **`revier events` prints a line that a linked host recorded with `link`,**
  the project here that links to the host's project, where one does. The
  line's `project` is the name on the host, so a reader had to ask `revier
  list --json` to join the two. Where two projects link to one, `link` is the
  first by name.

## v0.13.0

- **Action required for an out-of-tree remote adapter:** the `Remote` port has
  a new method, `Events`. An adapter without it no longer builds.
- **`revier events` prints what revier did in each project,** one JSON line
  per event: a target run or raised, an agent focused, an agent or shell tab
  opened, an action run, and the conversation each agent held. `--days` sets
  how far back it reads. The events are kept in
  `~/.local/state/revier/events.jsonl`, which revier never prunes. It counts
  nothing, so `jq` or a script decides what "most used" means.
- **`revier events` also prints what each linked host recorded,** with its
  `host`. This needs this version of revier on the host; an older one is named
  on stderr and the other events are printed. `--local` asks no host.
- **A linked host that is slow or gone no longer holds the project list.** A
  project closed in the surface stayed listed as open for eight seconds while
  one linked host did not answer. The rows refresh from this machine alone
  now, and each host's answer is laid over them when it arrives. A link's pane
  says `surveying` until its host has answered once, in place of a checkout
  reported missing.
- **A close no longer waits for a linked host it is not about.** With a host
  that was gone, a close of a project on this machine took 10 seconds, and 60
  to 90 with a host that hangs. A close waits only for the hosts of the links
  it closes.
- **A linked host that takes the connection and says nothing holds a save for
  ten seconds,** in place of thirty. The session saved before a close lost the
  conversations of the agents on this machine when a link to such a host was
  open.
- **A query keeps the list's order.** `cap` put a stopped project above an
  open one, because the match alone decided the order. The rows stay in the
  groups the list sorts them into, and within a group a row that holds the
  query whole stands above one that holds its letters apart.
- **A host that fails on every refresh no longer fills the log.** One linked
  host that refused the key wrote 53456 warnings in a day. Each different
  cause is written once until the host recovers.

## v0.12.1

- **A hidden popup comes back in place on GNOME,** in place of the animation
  that grew it out of a corner of the monitor. A window you minimized by hand
  comes back the same way. This needs a wctl whose `activate` takes
  `--no-animation`, with its extension loaded in the shell; with an older one
  the window comes back with the animation as before.

## v0.12.0

- **Action required: `alt+space` is the surface's own key now.** An
  `[[action]]` bound to `alt+space` is refused at load and named by
  `revier doctor`; give it another key.
- **The key that opens revier, pressed again on it, shows every agent in one
  list,** in the project list's place: every agent of every open project, the
  ones that need you first, then the working ones, then the ones at rest, the
  latest to speak on top. A row is the agent's state, what it is on and how
  long ago it spoke, over its project and the worktree it works in. The cursor
  starts on the agent that asked last. Enter goes to the agent and `del`
  closes its tab. revier always opens on the projects.
- **Beside the agent list the pane shows the agent's terminal as it is now,**
  once a second, in its colours. The wheel scrolls back through it, and a line
  wider than the pane continues on the next.
- **The same key switches the lists on a revier in a terminal of your own,**
  not only on the popup. With no revier in the focused window it raises or
  opens the popup as before.
- **A top line too narrow for every button ends in `≡`.** A click on it shows
  the remaining buttons.
- **Esc hides the popup at once on GNOME,** in place of the minimize animation
  that shrank it into a corner of the monitor. This needs wctl 0.13.0 with its
  extension loaded in the shell; with an older one the popup hides with the
  animation as before.
- **The cursor keeps its place when the project under it closes.** A closed
  project sorts down among the stopped rows and took the cursor with it, away
  from the open projects. The row that moved up is selected now, however the
  project closed: `del`, a shutdown, or a window closed by hand. A shutdown
  that closes everything puts the cursor on the first row.
- **A project name with a colon is refused before anything is written.**
  Renaming a project that was already refused for another reason wrote the
  file under the new name and removed the old one. The link dialog says the
  colon as it is typed, in place of refusing on Enter, and its message is no
  longer cut before it says what the name breaks.
- **`revier list --json` names the directory an agent works in,** as the
  agent's `dir`, when it is not the project's own.

## v0.11.0

- **Action required: `alt+t` is the surface's own key now.** An `[[action]]`
  bound to `alt+t` is refused at load and named by `revier doctor`; give it
  another key. A target bound to it keeps its desktop binding and does nothing
  inside the surface.
- **The pane ends in what one of the project's agents said last**, and how
  long ago, in place of the directory tree the path above it already named.
  Without a keypress it shows the agent that needs you, else one with a
  message to show, one at rest before one working, the latest to speak among
  equals; moving the agent cursor chooses instead, and the choice holds until
  another project is under the list's cursor. A message too long for the room
  keeps its end, where an agent says what it did and what it needs. Only
  Claude Code agents are read, and only ones on this machine; for anything
  else the pane says so.
- **Tab now walks the projects and the agents; alt+t reaches the targets.**
  Tab no longer stops at the targets, which have no filter field of their own
  any more, and Tab from the targets goes back to the projects. One click
  selects a target or an agent row and a second opens it, as on a project row,
  so a single target can be reached for `del` and `alt+del`.
- **The top line says which revier is running,** at its right end, where the
  terminal is wide enough for the buttons and the version both. A released
  binary and one built from source no longer look the same.
- **A project open only because you attached a window to it now reads as
  open.** Its row carried no mark, the pane said "stopped" and it sorted with
  the closed projects, while the targets listed right under those facts showed
  it attached and running.
- **A link's row no longer counts agents you cannot reach from here.** A
  project on another machine listed every agent in it, including the ones
  opened over there, so a row drawn as closed counted an agent and Enter on it
  refused: there is no window here to raise. The same held the other way for
  an agent this machine serves to a terminal elsewhere. `revier list` counts
  the same way; `revier list --json` still reports every agent the survey
  found, which is what the revier on the other machine reads.
- **Restoring a link that was open on something other than its home target no
  longer comes back empty.** The save recorded the target and left the agent
  in it out.
- **A message an agent wrote can no longer repaint the surface.** Agents quote
  what tools print, and a captured colour diff, a progress line or a window
  title in that quote was passed to the terminal as an instruction rather than
  shown as text.
- **The surface no longer crashes on a table whose header holds a tab.**
- **The pane no longer flickers or loses the agent you chose** when the
  terminal retitles its window on every refresh.
- **A click on the message no longer runs the row it is level with,** and a
  click on another project's row is no longer half of a double click.
- **A long-running surface no longer grows** by holding on to the last message
  of every agent that has ever run in it, and no longer re-reads every Claude
  Code project folder on every refresh for an agent that has written nothing
  yet.
- **The daily log no longer gains a warning per agent per refresh** for agents
  there is simply nothing to read for - one whose session is not listed, or
  which has not spoken yet.
- **The surface says "running" in one voice.** The clone hint, the link
  dialog's status word and the save screen's count of open projects each read
  differently from the rows beside them.
- **`alt+t` is offered in the footer only where it does something,** and the
  footer no longer calls Tab "next section" in the targets, where it goes back
  to the projects.

## v0.10.0

- **Action required: `del` and `alt+del` are the surface's own keys now.** An
  `[[action]]` bound to `delete` or `alt+delete` is refused at load and named
  by `revier doctor`; give it another key. A target bound to one keeps its
  desktop binding and does nothing inside the surface.
- **`del` closes the row under the cursor, `alt+del` deletes what the
  configuration holds of it.** On a project row that is the whole project, on
  a target row that target, on an agent row that agent. A delete closes first
  and asks before it writes, so a delete the file would refuse costs no
  window. `alt+del` on a link unlinks it and changes nothing on its host.
- **Closing a tab closes every panel in it.** Before, the panel the target is
  found by went and the rest stayed on screen while revier said "closed". An
  agent working in any panel of that tab refuses the close, including one
  that appeared while the confirm was on screen.
- **One project revier cannot read no longer stops the others closing.** A
  shutdown with a dead remote host used to refuse everything, and `--force`
  was the only way past - which also closed every busy agent on the machine
  unasked. Now the unreachable project's own targets are left open with the
  reason beside them and every other project closes. `--force` means one
  thing: do not refuse a busy agent.
- **A slow or unreachable host no longer makes a shutdown fail or close
  nothing.** Reading the agents again, saving the session and closing each
  get their own time to do it in. Ctrl-C still stops a shutdown or a restore
  at any point.
- **A busy agent is protected in more places.** An agent in a terminal you
  attached to a link by hand was invisible to a shutdown and was ended
  mid-turn; so was one in a project the survey could not reach. Both refuse
  the close now. An agent that both a link's host and this machine report is
  listed once, not twice.
- **The surface opens with the rows above the starting project in view**
  rather than with that project alone on the last line, and the rule over the
  list carries the total agents in each state, in the column the rows count
  in.
- **Deleting a project leaves the cursor on the row that took its place,**
  instead of jumping to the top of the list.
- **A workspace whose first panel could not be marked still opens.** Before,
  `revier go` reported a failure with the window already on screen, and the
  next press opened a second copy. The only thing lost is that a return home
  from a tab may land in the wrong pane.
- **Agents no longer all read as unknown when a command is run from a
  directory that was removed** - a worktree deleted while a shell sat in it.
  `revier status`, `list`, `doctor` and `shutdown` were affected; the surface
  was already fixed in v0.9.0.
- **A return home lands in the pane revier opened the workspace at.**
- **A new project writes a home target of its own** when no shared target in
  `config.toml` is home.
- For anyone writing a `[[probe]]`: the optional `since` field is gone from
  the state a probe reports. Nothing read it. A script that still prints it
  is not refused; the value is ignored.

## v0.9.0

- **Esc hides the popup instead of closing it.** The next press of the
  desktop key raises it with the cursor and the query where you left them,
  and the agent states arrive in a fraction of the time a cold start took.
  The popup opens on the project of the window you pressed the key in, and a
  project made, edited or deleted while it was hidden shows at once. Where
  the window manager cannot hide a window, Esc closes the popup as before.
- **A linked host answers over one shared ssh connection.** The first call
  opens it and the calls after it cost milliseconds instead of a handshake,
  for ten minutes after the last use.
- **A window manager that does not answer costs only its own targets.**
  Before, one timeout emptied the whole view and every key. Now those targets
  show as unknown with the reason, a press on one is refused with it, and
  every other target works. The footer and `revier list` name the host that
  did not answer. A project with an unknown target cannot be renamed or
  deleted until the host answers.
- **A mistake in one `[[action]]` or one shared `[[target]]` in `config.toml`
  disables only that entry.** Before, it stopped every project, the TUI, the
  desktop keys and the popup. `revier doctor` reports `config.toml` under its
  own heading.
- **`revier open` and Enter in the TUI do the same thing.** A running
  workspace is raised, a start into a missing checkout with nothing to clone
  from is refused with the reason, and a project with no home target says so.
- **Agents no longer all show as unknown after the directory revier started
  in is removed,** for example a worktree deleted under a running TUI. Before,
  only a restart of revier brought them back.
- **A project rename that is interrupted no longer cuts a saved session
  short.**
- **`revier new` accepts a local checkout at the same path as a link's
  remote checkout,** as the TUI already did.
- **`revier session --help` prints usage** instead of failing with an unknown
  command.
- **The pane's first row reads `Project  <name>  edit alt+e`,** and every key
  hint is grey.
- **revier is licensed MIT.**

## v0.8.0

- **Action required if a project file has a `name = ...` line: delete it.**
  The file name is the project's name now. A file that still has the line is
  listed as invalid and does not open until the line is gone.
- **Action required if a link file declares its own targets: move each
  `[target.window]` or `[target.runtime]` table to `[target.remote.window]` or
  `[target.remote.runtime]`.** A link's targets are its remote part. A link
  that writes a local part has that target refused.
- **Action required for a link made before this release: run `revier
  doctor`.** A target whose template reads `{{.Path}}` or `{{.GitURL}}`
  renders nothing on an older link, and is now refused instead of opening an
  editor on nothing. Add the host's `path` and `git_url` to the link file, or
  make the link again. A new `revier link` records both.
- **A mistake in a project file disables only what is wrong.** Before, one
  bad file stopped every project, the TUI, the desktop keys and `revier
  popup`. Now a bad target shows as invalid and its key says why when
  pressed, a bad file stays listed with the reason, and every other project
  works. Every command warns once on stderr.
- **`revier doctor` lists every problem in the project files,** each under
  its file with the edit that fixes it, and exits non-zero when it finds one.
- **alt+e opens a project screen instead of `$EDITOR`.** A click on the
  project's name at the top of the pane opens it too. You type the name, path
  and git URL in place, and a new name renames the project, its file and its
  saved sessions. Every target the project has is listed with the file it
  comes from. An edit to a target shared with `config.toml` is written to the
  project file as the values that differ, and alt+d on it gives the shared
  target back. A rename is refused while a target's pattern holds the old
  name, and names the value to change.
- **A shared target can say what it does for a project on another machine.**
  In `config.toml`, `[target.remote.window]` or `[target.remote.runtime]`
  beside the local part gives every link that target, for example VS Code
  over ssh where a local project opens IntelliJ. A shared target with no
  remote part is not given to links.
- **A link's workspace is tabs of your terminal.** It opens an agent tab and
  a shell tab, each an ssh that runs the project's agent or shell on the
  host. Tabs, scrollback, keys and focus are your terminal's, with no tmux in
  between, and the host needs no tmux. An agent ends with its tab or its
  connection; a saved session resumes it on the host.
- **A link's agents show their state on a host whose login profile sets the
  PATH.** Before, revier ran on the host without the profile, so claude was
  not found and every agent read as unknown. A host whose shell is fish, or
  whose profile prints, no longer breaks the survey.
- **The popup lists every project in about 80 ms.** Before, it showed
  "surveying" until every linked host answered. The names come first, and the
  state marks and counts appear when the hosts answer. A delete or a rename
  waits until then.

## v0.7.0

- **Action required if an action uses Home, End, Page Up, Page Down, Up,
  Down, Enter, Tab, Shift+Tab or Esc: give it another key.** The TUI now uses
  these keys itself, so such an action never ran. It is now refused at
  startup with "one of revier's own keys".
- **A project row counts its agents by state.** The right column shows one
  glyph and count for each state, with the state closest to needing you
  first. Each state keeps its own column, so the counts line up from row to
  row. The activity line, the open targets and a missing checkout are now
  only in the pane.
- **Home, End, Page Up and Page Down move the cursor.** They move it in the
  project list, the pane's sections and the link dialog. Home and End no
  longer move the cursor in the query; ctrl+a and ctrl+e still do.
- **A host's project list in the link dialog filters as you type.** Esc
  clears the query before it steps back.
- **The new-project screen takes a full path, a name or a clone URL.** Tab
  completes a directory, and an empty field lists the folders your projects
  are in. A full path adds that folder. For a name or a clone URL, you pick
  which of those folders it goes in. revier asks before it creates a missing
  folder or clones into it. A folder that is already there is added with its
  own origin, and a folder that is already a project is refused. A project
  added here records its git origin and is trusted by mise, as `revier new`
  does.
- **The new-project screen refuses your home directory**, as `revier new`
  does.
- **The popup is 1800 px wide on every screen.** On a very wide screen, 70%
  of the width spread the list and the pane too far apart. A screen narrower
  than 1800 px still gets a popup that fills it.
- **Page Up in the agents section moves by a full page.** Before, it moved
  only by the number of rows below the cursor.
- **In the link dialog, the first Esc closes the dialog.** Before, it
  cleared a hidden query that was left from the previous link.

## v0.6.1

- **The popup opens at full screen height.** In v0.6.0 it opened at 70% height,
  leaving blank desktop above and below the window. The width is unchanged: 70%
  on a wide screen, 100% when the screen is too narrow for the detail pane.

## v0.6.0

- **Action required: run `revier keys install` once.** The desktop keys ran
  the scripts `revier-popup` and `revier-go`, which are no longer shipped, so
  Alt+Space and the target keys do nothing until they are installed again.
  The install rewrites revier's own shortcuts in place; the keys now run
  `revier popup` and `revier go <target> --picker`.
- **A new popup opens centred at 70% of the screen, or fills the screen when
  that is too narrow for the detail pane.** A popup already open is raised
  where you left it. The popup needs kitty and the GNOME Window Control
  extension, and `revier popup` names whichever is missing.
- **`revier shutdown` closes what is open, and saves the session first.** It
  closes every project, or one named project, and `--agents` closes only the
  agents while `--targets` closes only what holds no agent. The session is
  saved when it differs from the newest saved one, unless
  `--no-session-save`, so a restore brings back what the shutdown closed.
  While an agent works or waits for an answer it saves and closes nothing and
  names that agent; `--force` shuts down anyway. A window whose application
  asks about unsaved work stays open and is named. In the TUI, alt+q asks
  every project or one, then what of it, and shows the plan before anything
  closes.
- **Action required if an action uses Alt+Q: give it another key.** alt+q now
  opens the TUI's shutdown wizard, so a `[[action]]` with `key = "alt+q"` is
  refused at startup with "one of revier's own keys".
- **The TUI has a sessions screen, on alt+s.** It lists the saved sessions,
  newest first, and the pane beside it shows what a restore of the one under
  the cursor would do to each target and each recorded agent: open or resume
  it, keep what is already open, or skip it with the reason. Enter restores,
  and the pane then says what each step came to. alt+s on the screen saves
  what is open now under an optional name, and names what the save could not
  record.
- **Action required if an action uses Alt+S: give it another key.** alt+s now
  opens the TUI's sessions screen, so a `[[action]]` with `key = "alt+s"` is
  refused at startup with "one of revier's own keys".
- **A restore no longer hangs on a host that never answers.** Each step has
  its own deadline, in the TUI and in `revier session restore`; before, one
  silent host left the TUI on "restoring" for good and refused every later
  restore. A target the restore is launching can no longer be launched a
  second time by a keypress, a save is refused while a restore runs, and an
  agent tab that did not open says why.
- **A search ends as soon as something opens.** A double click, a click on a
  pane row, a target key and an action drop the query the way Enter does, so
  the next popup comes up with an empty field. The cursor stays on the row the
  query found.
- **An agent opened at a panel whose directory is gone starts in the project.**
  When the panel's shell sat in a worktree that was removed, the agent started
  in the root directory instead.
- **Action required on sway: sway is no longer supported.** A `config.toml`
  that names `sway` in `[hosts] window` fails every command with `unknown
  window host "sway"`; remove it from the list.

## v0.5.0

- **Action required: on tmux, every workspace is a session of its own.**
  revier no longer sees workspaces opened by v0.4.0 on tmux: they were windows
  of one shared session. Those windows keep running, but a key launches a new
  workspace beside them. Close the old session (`tmux kill-session`) once its
  agents are done. In return tmux has tabs: `revier agent new` works there, a
  restore brings back every agent, and two terminals on two workspaces no
  longer switch each other.
- **The TUI has three sections: projects, targets and agents.** Tab and
  shift+tab move between them, and each section has its own filter. Enter
  opens a project, runs a target, or goes to an agent's tab and raises its
  window, on a remote project's host too. Esc clears the filter, then returns
  to the projects.
- **A drag selects a box of the screen and copies its text.** The screen holds
  still during the drag, and the text goes to the clipboard on release. Esc
  drops the selection. A click still runs the item it pressed on.
- **In a remote project's window, `revier agent new` and the new `revier shell
  new` open their tab on the host.** Before, the agent key was refused and the
  shell key opened a shell on this machine. A target the link declares locally
  still opens here. A host that runs an older revier is named, with the advice
  to update it.
- **revier writes a log of what it did.** Every operation is one line in a
  daily file under the state root, kept for 14 days, with its duration and any
  error. A failure that repeats on every refresh is written once.
- **The pane's targets and agents line up on one grid**, and a long harness
  name no longer pushes a row out of the pane.
- **A key pressed while a workspace launches no longer opens a second one.**
  This happened when the TUI saved its state at the same moment, or when the
  window came up but could not be focused.
- **On tmux, a key moves only the terminal it was pressed in**, and a grouped
  session (`tmux new-session -t`) shows its workspace's agents.
- **On a machine with no tmux server, every keypress wrote a warning** to the
  log. It no longer does.
- **Enter on an agent works when two kitty processes of one project are
  open.** It was refused as ambiguous.

## v0.4.0

- **A restore brings back every agent, each in the directory it worked in.**
  A save records every agent a workspace holds, including the ones opened by
  hand in extra tabs, and the directory each worked in. An agent that worked in
  a worktree carries on in that worktree, not in the project root. A worktree
  removed since the save starts its agent empty in the project, and the restore
  says so. Sessions saved by v0.3.0 still restore their conversations.
- **`revier agent new` opens an agent beside an open workspace.** In kitty it
  opens a tab with the project's agent panel and its shell, in `--dir`, on the
  conversation `--resume` names. A kitty key can pass the window it was pressed
  in; the README has the `map` line. A restore opens the agents past the
  declared layout the same way. tmux has no tabs, so there a restore names
  those agents as not restored.
- **A target can open as a tab inside another.** `inside = "home"` in a
  target's `[target.runtime]` opens it as a tab of that workspace in kitty,
  for a ticket viewer beside the agents. A second press returns home.
- **A conversation is resumed only by the harness that holds it.** A Claude
  conversation laid over an opencode panel started opencode with Claude's
  resume flag. That agent now starts empty, and the restore names every agent
  whose conversation could not be resumed here.
- **A working agent's glyph spins** in the project rows and the pane.
- **A minimized window is raised by its key.** revier did not see it and
  launched a second copy.
- **A kitty workspace is raised, or the key says why.** A window revier could
  not tell from its siblings was focused inside kitty only, and GNOME showed
  an "is ready" notice instead of the window.
- **An agent is still seen as one after it copies to the clipboard.** The
  clipboard helper Claude Code starts hid the agent, and its panel read as a
  plain tool.

## v0.3.0

- **Action required: an action on alt+n, alt+r, alt+c, alt+h, alt+e, alt+d,
  ctrl+p, ctrl+n or ctrl+c is refused at start.** These keys are revier's
  own, and an action on one of them never ran. Give the action another key in
  `config.toml`; the error names the action.
- **Action required: a Claude agent's state is what Claude Code reports, and
  needs Claude Code 2.1.212 or newer.** revier reads `claude agents --json`,
  so a permission prompt or a question shows as attention on tmux as well as
  kitty, with no hook to install. Update Claude Code; on an older one every
  Claude pane reads as unknown. `CS_STATE` and `CS_TAB` are no longer read, so
  a hook that sets them can be removed. The pane title is the activity line
  only.
- **`revier session save` and `revier session restore` bring the desktop back
  after a restart.** A save records which projects and targets are open;
  restore opens them again from the project files as they read now, leaves
  what is already up alone, and names what has since been deleted. A Claude
  agent in a panel declared `kind = "agent"` comes back on the conversation
  it held. `--dry-run` shows what a restore would open, and `revier session
  list` shows the saved sessions. A save with nothing open writes nothing, so
  it never replaces the session saved before the restart.
- **Targets most projects share are declared once, in `config.toml`.** A
  `[[target]]` there is every project's. A project file overrides any one field
  of it by declaring a target of the same name, and adds targets of its own.
  `revier new` and alt+n write no targets when shared ones exist.
- **alt+c is a configuration screen that applies each change as it is made.**
  It sets the theme, the glyphs, the trigger key and the runtime host, and
  adds, changes and deletes actions and shared targets, panels included. The
  file keeps its comments, and a change that would break a project is refused
  with the project named.
- **The TUI is right on its first screen.** A workspace revier knows only by
  where it last landed showed as stopped, with no agent, until the next
  refresh a second later. Its state and its agent now show on the first
  screen.
- **A top line of actions: new, remote, config, help.** Each is a button and a
  key. alt+n adds a project from a directory on this machine, writing the file
  `revier new` writes; alt+r is the remote link dialog; alt+c is the
  config screen; alt+h lists every key revier answers to, in the
  TUI and on the desktop.
- **A host that cannot be reached says what to do about it.** The remote
  dialog and `revier link` turn ssh's usual failures into one line each: an
  unknown host key, a refused key, a name that does not resolve, nothing on
  port 22, a revier the PATH of an ssh command does not reach, a code-hosting
  service that runs nothing.
- **The remote dialog asks what to name a link.** Enter on a host's project
  offers `rs-<host>-<project>`; typing replaces it, and a name a project here
  already has is said as it is typed and not written. The host's projects are
  listed in the same table as the project list.
- **The pointer lights what it is over.** Rows, targets and buttons take a
  background a step below the selection's. A click on a project selects it and
  a double click opens it; one click runs a target or a button.
- **The header and the border are gone.** The rule over the list carries the
  counts, now by agent state - blockers, working, idle - beside how many rows
  the filter left. The list gains four columns.

## v0.2.0

- **Tab moves the cursor into the pane, and the target list is gone.** The
  list stays on the screen with the pane beside it; Tab moves the cursor onto
  the project's targets and attached instances, where typing filters them,
  Enter runs the one under the cursor, and Tab or Esc brings the cursor back.
  A click on a target row runs it. On a terminal too narrow for both, the
  pane stands in the list's place while the cursor is on it. Clearing the
  query puts the cursor back on the project it was on before the first letter.
- **A project can live on another machine.** A link file, `[remote]` with
  `host` and the project's name there, stands for it here, and revier on
  the host, installed with tmux and the project's own file, reports what
  its agent is doing. The list shows the link as `name@host` with a server
  icon, Enter opens an ssh pane onto the workspace, and `revier agent
  prompt`, `revier agent wait` and `revier run` run on the host, an action
  being one the host's configuration defines. A link may add targets of its
  own. A host that does not answer shows as unreachable and holds nothing
  else up.
- **alt+r links a project on another machine.** It lists the hosts
  `~/.ssh/config` names, then the projects the revier on the chosen host has,
  and Enter on one writes the link and puts it in the list. `revier link
  [host [project]]` is the same three steps as a command, with `--name` for
  a different name here.
- **`revier open --attach` ends with the terminal on the workspace**, on
  tmux. It is what the ssh pane of a remote project runs on the host.
- **`revier list` takes project names** and lists those alone.
- **The rows are a table and the frame takes the whole terminal.** Project and
  agent stand in their own columns on both lines of a row, the list stops at
  the table's width and the pane takes the rest, and on a narrow terminal the
  agent column gives way before the project column.

## v0.1.0

The first release. It ships the `revier` binary and the two desktop scripts,
`revier-popup` and `revier-go`, for Linux x64 and arm64.

- **The TUI shows every project and what its agent is doing.** Projects whose
  agent needs you say so and sort first; the rest read working or idle. Type
  to filter, Enter opens the project's home, Tab lists its targets. A click
  selects a row and a double click opens it.
- **One key runs-or-raises a target, and the same key returns you.** `revier
  go <target>` opens the target when it is absent, focuses it when it exists,
  and takes you back to where you came from when you are already on it.
- **The project is resolved from the focused window.** A desktop key needs no
  working directory: revier reads which project owns the window in front of
  you. `revier attach` binds a window revier did not open.
- **Agent state for Claude Code.** Idle, working, and waiting for you are read
  from the agent's terminal. opencode is recognised but reports no state. An
  external probe command can report state for anything else.
- **Runtime hosts kitty and tmux; window hosts GNOME and sway.** A target
  opened by revier is found again on the next keypress, including windows
  that appear after the launch.
- **Desktop keys on GNOME.** `revier keys install` claims `Alt+Space` for the
  popup and the `Ctrl+Shift` keys for home, editor and web; `revier keys
  status` says who holds each; `revier keys uninstall` gives them back.
- **`revier each -- <cmd>` runs one command in every project's directory** and
  keeps each project's output under `~/.local/state/revier/runs/`.
- **`revier agent wait` and `revier agent prompt`** let a script block until
  an agent reaches a state, and type one line into it.
- **Projects are TOML files** under `~/.config/revier/projects/`; `revier new`
  writes one, and a project whose directory is missing is cloned from its
  `git_url` when opened.
