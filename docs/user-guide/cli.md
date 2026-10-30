# Command line

`revier` with no command opens the TUI. Every other command does one thing and
exits. `revier help` lists the commands and their flags. `revier <command>
--help` gives the usage of one command. `revier agent help`, `shell help`,
`session help`, `keys help`, `term help`, `each help`, `events help` and `state help` give each family
in full.

- `-p <name>` names the project. Without it, revier finds the project as
  [Concepts](concepts.md#project) says.
- An agent is named by its [address](agents.md#drive-an-agent-from-a-script).
- Put `--` before text that starts with a dash:
  `revier agent prompt demo -- "-v is the flag"`.

## Commands

| Command | Does |
|---|---|
| `revier` | The TUI. See [The TUI](tui.md). |
| `revier popup` | The TUI in a kitty window of its own. |
| `revier list` | Print every project, its state, agents and targets. |
| `revier new` | Write a project file for the current directory. |
| `revier open` | Run-or-raise the project's workspace. |
| `revier go <target>` | Run-or-raise a target. Run again, it returns to the home. This is what a desktop key runs. |
| `revier attach` | Bind the focused window to the project. |
| `revier link` | List the machines you added, list a host's projects, or link one. See [Projects on another machine](links.md). |
| `revier run <action>` | Run an `[[action]]` in the project. |
| `revier each -- <cmd>` | Run a command in every project's directory. `revier each log` shows past runs. |
| `revier agent new` / `revier shell new` | Add an agent tab, of the default agent or a named one, or a shell tab to an open workspace. |
| `revier agent prompt` | Type one line into an agent and submit it. |
| `revier agent wait` | Wait for an agent to reach a state. |
| `revier agent read` | Print an agent's last reply, or its screen. |
| `revier agent send-keys` | Type keys into an agent, such as `esc` or `enter`. |
| `revier agent focus` | Go to an agent's tab. |
| `revier agent close` | End one agent: close its tab, or in the workspace's own tab its panel alone. |
| `revier session save` / `restore` / `list` | Record the open projects, open them again, list the saves. See [Save and restore sessions](projects.md#save-and-restore-sessions). |
| `revier shutdown` | Save the session, then close what is open. After a full shutdown the next TUI start offers the restore. |
| `revier keys status` / `install` / `uninstall` | Show, claim or release the desktop keys. |
| `revier term status` / `install` / `uninstall` | Show, write or remove the terminal keys in your terminal's configuration. See [kitty](integrations/kitty.md#add-tabs-from-a-kitty-key). |
| `revier status` | The project revier finds here (directory, focused window, last project), and the hosts it uses. |
| `revier doctor` | Every configuration problem, and its fix. |
| `revier assist` | Start Claude Code, briefed on your revier configuration. See [Agents](agents.md#let-an-agent-configure-revier). |
| `revier events` | Each time you used a project, as JSON lines. |
| `revier state` | What revier had open at a moment, per machine, and why a saved session differs from what was open when it was saved. |
| `revier version` | The installed version. |

`revier agent exec` and `revier shell exec` are what a link's panels run on the
host. You do not run them by hand.

[Agents](agents.md) and [Projects](projects.md) show these commands in use.

## Output

Commands print tables for a person. `revier list`, `revier session list`,
`revier state`, the `revier keys` and the `revier term` commands take `--json` for a script. `revier events` always
prints JSON. Warnings and errors go to stderr, so stdout stays clean.

The TARGETS column of `revier list` marks each target: `*` open, `x` no host
on this machine can open it, `!` its configuration is refused (`revier
doctor` says why), `?` its host did not answer.

## Exit codes

| Code | Means |
|---|---|
| `0` | Success. |
| `1` | Failure. `revier doctor` also exits `1` when a file has a problem. |
| `2` | `revier agent wait` timed out. |
| `3` | No project found: not from the directory, the focused window or the last project. |
| `4` | `revier keys install` or `uninstall`, or `revier term install` or `uninstall`: some key is not as asked. |
| `5` | `revier each`: the command failed in at least one project. |
| `6` | `revier assist`: `claude` is not on `PATH`. |
| other | `revier run` passes on the action's own exit status. |

## Environment variables

| Variable | Does |
|---|---|
| `REVIER_CONFIG_HOME` | The configuration directory; see [Files](configuration.md#files). |
| `REVIER_STATE_HOME` | The state directory: logs, events, sessions, the state record. Default `$XDG_STATE_HOME/revier`, else `~/.local/state/revier`. |
| `REVIER_SSH_CONFIG` | The file revier reads ssh hosts from, in place of `~/.ssh/config`. |
| `NO_COLOR` | A value that is not empty turns colour off in the TUI. |
| `CLAUDE_CONFIG_DIR` | Where revier reads Claude Code's files. Default `~/.claude`. |

revier sets `REVIER_PROJECT`, `REVIER_PROJECT_PATH` and `REVIER_PROJECT_HOST`
for a status command; see [Statuses](extending/status.md#environment).
