# Troubleshooting

Start with `revier doctor`. Then find your problem below.

## revier doctor

```
revier doctor
```

It prints each file with a problem, one line per problem, and a `fix:` line
where it knows the fix. It exits 1 when a file has a problem.

```
/home/you/.config/revier/projects/demo.toml
  project    no target is marked home; nothing to open or return to
             fix: mark the workspace target `home = true`

1 project, 1 file with problems
```

A mistake stops only the part it is in; see
[configuration.md](configuration.md#mistakes). When every other command
fails, `revier doctor` still prints the error, and `revier assist` still
starts. It hands the terminal to Claude Code, briefed to find and fix the
problem. See [Agents](agents.md#let-an-agent-configure-revier).

## A key does nothing

```
revier keys status
```

This lists each desktop key revier wants, and who holds it.

| Status | What to do |
|---|---|
| `active` | revier holds the key. Read on. |
| `free`, `stale`, `inert` | `revier keys install` |
| `taken`, `builtin` | `revier keys install --force`, or give the target another key |

Run `revier keys install --dry-run` first to see what changes. See
[integrations/gnome.md](integrations/gnome.md).

When the key is `active` and still does nothing:

- The focused window belongs to no project, so the key acts on the project
  revier acted on last. Attach the window with `revier attach`, or give the
  target a `match` for it. `revier status` says which project revier finds.
- The project has no such target, or the target is `invalid`.
- In the TUI, a terminal that does not report key disambiguation (kitty does)
  sends Ctrl+Shift+X as Ctrl+X. The TUI then runs the target only if Ctrl+X
  has no other use there.

## A target says no host here

No host on this machine can open the target.

- A window target needs GNOME with the Window Control extension and `wctl`.
  See [integrations/gnome.md](integrations/gnome.md).
- A tab target needs [kitty](integrations/kitty.md) or [tmux](integrations/tmux.md).

The config screen (alt+c) shows which hosts revier uses.

## An agent shows no state

- Claude Code needs version 2.1.212 or newer. See
  [integrations/claude-code.md](integrations/claude-code.md).
- opencode reports no state. See [integrations/opencode.md](integrations/opencode.md).
- Any other harness needs a [probe](extending/probes.md).

When a probe fails, the agent shows `unknown` and the log has the reason.

## A link will not connect

The project shows `unreachable`, and the pane says what ssh said and what to
do. revier cannot type a password: set up key authentication, and run
`ssh <host>` once to accept the host key. revier must be on the PATH of your
login shell on the host. Check it:

```
ssh <host> '$SHELL -lc "command -v revier"'
```

When the link hangs after a VPN change or a suspend, end the shared ssh
connection. The next call opens a new one:

```
ssh -O exit -o ControlPath="$XDG_RUNTIME_DIR/revier/ssh-%C" <host>
```

See [links.md](links.md).

## The popup does not open

A desktop key shows no error. Run `revier popup` in a terminal: the message
names what is missing, kitty or the GNOME Window Control extension.

## The log

Every revier process writes to one log file per day, one JSON line per
record. The log is in `logs/` in the state directory: `$REVIER_STATE_HOME`,
else `$XDG_STATE_HOME/revier`, else `~/.local/state/revier`. revier removes
files older than 14 days.

```bash
L=${REVIER_STATE_HOME:-${XDG_STATE_HOME:-$HOME/.local/state}/revier}/logs/revier-$(date +%F).log
jq -c 'select(.level != "INFO")' "$L"     # everything that went wrong
```

Use `pid` to tell the processes apart. A linked project logs on its host. To
see what revier did in each project, run `revier events --days 3`.
