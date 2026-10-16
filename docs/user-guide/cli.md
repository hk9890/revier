# Command line

`revier` with no command opens the TUI. Every other command does one thing and
exits. `revier help` lists the commands. `revier agent help`, `shell help`,
`session help`, `keys help`, `each help` and `events help` give each family
in full, with its flags.

- `-p <name>` names the project. Without it, revier uses the project of the
  current directory, else of the focused window, else the one it acted on last.
- An agent is `<project>`, or `<project>:<target>` or `<project>:<number>`
  when the project has more than one ([Agents](agents.md#drive-an-agent-from-a-script)).
- Put `--` before text that starts with a dash:
  `revier agent prompt demo -- "-v is the flag"`.

## Commands

| Command | Does |
|---|---|
| `revier` | The TUI. See [The TUI](tui.md). |
| `revier popup` | The TUI in a kitty window of its own. |
| `revier list [--json] [<name>..]` | Print every project, its state, agent and targets. |
| `revier new [<name>]` | Write a project file for the current directory. |
| `revier open [<name>] [--attach]` | Run-or-raise the project's workspace. `--attach` (tmux only) ends with this terminal on it. |
| `revier go <target> [--picker]` | Run-or-raise a target. Run again, it returns to the home. This is what a desktop key runs. |
| `revier attach` | Bind the focused window to the project. |
| `revier link [<host> [<project>]]` | List hosts, list a host's projects, or link one. See [Projects on another machine](links.md). |
| `revier run <action>` | Run an `[[action]]` in the project. |
| `revier each [--filter '<sh>'] -- <cmd>` | Run a command in every project's directory. `revier each log` lists past runs; `revier each log <run> <project>` prints what the command printed there. |
| `revier agent new` / `revier shell new` | Add an agent tab or a shell tab to an open workspace. |
| `revier agent prompt <agent> <text>` | Type one line into the agent and submit it. |
| `revier agent wait <agent> --until <status>` | Wait for `idle`, `running`, `attention` or `stopped`. |
| `revier agent read <agent> [--screen]` | Print the agent's last reply, or its screen. |
| `revier agent send-keys <agent> <key>..` | Type keys, such as `esc` or `enter`. |
| `revier agent focus <agent>` | Go to the agent's tab. |
| `revier session save` / `restore` / `list` | Record the open projects, open them again, list the saves. |
| `revier shutdown [<project>]` | Save the session, then close what is open. Refuses while an agent works or waits for you, unless `--force`. |
| `revier keys status` / `install` / `uninstall` | Show, claim or release the desktop keys. |
| `revier status` | The project revier finds here (directory, focused window, last project), and the hosts it uses. |
| `revier doctor` | Every configuration problem, and its fix. |
| `revier assist` | Start Claude Code, briefed on your revier configuration. See [Agents](agents.md#let-an-agent-configure-revier). |
| `revier events [--days <n>]` | Each time you used a project, as JSON lines. |
| `revier version` | The installed version. |

`revier agent exec` and `revier shell exec` are what a link's panels run on the
host. You do not run them by hand.

[Agents](agents.md) and [Projects](projects.md) show these commands in use.

## Output

Commands print tables for a person. `revier list`, `revier session list` and
the `revier keys` commands take `--json` for a script. `revier events` always
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
| `4` | `revier keys install` or `uninstall`: some key is not as asked. |
| `5` | `revier each`: the command failed in at least one project. |
| `6` | `revier assist`: `claude` is not on `PATH`. |
| other | `revier run` passes on the action's own exit status. |

## Environment variables

| Variable | Does |
|---|---|
| `REVIER_CONFIG_HOME` | The configuration directory. Default `$XDG_CONFIG_HOME/revier`, else `~/.config/revier`. |
| `REVIER_STATE_HOME` | The state directory: logs, events, sessions. Default `$XDG_STATE_HOME/revier`, else `~/.local/state/revier`. |
| `REVIER_SSH_CONFIG` | The file revier reads ssh hosts from, in place of `~/.ssh/config`. |
| `NO_COLOR` | A value that is not empty turns colour off in the TUI. |
| `CLAUDE_CONFIG_DIR` | Where revier reads Claude Code's files. Default `~/.claude`. |

revier sets `REVIER_PROJECT`, `REVIER_PROJECT_PATH` and `REVIER_PROJECT_HOST`
for a status command; see [Statuses](extending/status.md#environment).
