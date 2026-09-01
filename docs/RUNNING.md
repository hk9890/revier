# Running

**Local delta —** use the `run` skill for the generic launch-and-drive flow;
everything below is this repository's delta and wins where the two disagree.
The automated suites are [TESTING.md](TESTING.md)'s.

## The rule

revier manipulates the user's live desktop: it opens terminal windows, raises
editors, and switches focus. **Never verify a change by driving the user's
session.** A stray `Open` steals focus mid-sentence, and a stray `Focus` moves
a window the user is reading.

Every recipe below runs on a private substrate instead. Nothing here attaches a
client, reaches a display, or is visible in the user's own tmux.

## Drive the CLI against a scratch configuration

`REVIER_CONFIG_HOME` and `REVIER_STATE_HOME` redirect revier away from the
user's projects and state. Set both, every time.

```bash
S=$(mktemp -d); mkdir -p "$S/projects" "$S/state"
printf '[hosts]\nwindow = ["none"]\n' > "$S/config.toml"   # never touch a real window
cat > "$S/projects/demo.toml" <<EOF
name = "demo"
path = "$S"
[[target]]
name = "home"
home = true
  [target.runtime]
  name = "home"
  launch = ["sh", "-c", "sleep 600"]
  match = { title = "^home$" }
EOF
export REVIER_CONFIG_HOME=$S REVIER_STATE_HOME=$S/state

./bin/revier status -p demo    # which hosts were selected
./bin/revier open demo         # run-or-raise the workspace
./bin/revier list              # agent state and target availability
./bin/revier list --json       # what both renderers read

tmux kill-session -t revier; rm -rf "$S"
```

`window = ["none"]` is the important line. This machine has a working GNOME
adapter, so without it a window target would launch a real application and move
the user's focus.

To exercise the agent monitor, give a pane a Claude-style title. The leading
glyph is the state signal:

```bash
tmux select-pane -t agent -T '⠧ Working on it'   # spinner -> running
tmux select-pane -t agent -T '✳ Ready'           # at rest  -> idle
```

## Drive the core without any configuration

`scripts/drive` builds a project in memory and runs it against a tmux server on
socket `revier-drive`, which is private to it.

```bash
go run ./scripts/drive go home      # run-or-raise the workspace
go run ./scripts/drive go diff      # opens, and leaves it focused
go run ./scripts/drive go diff      # focused already, so this returns home
go run ./scripts/drive survey       # the view both renderers read, as JSON
go run ./scripts/drive kill         # remove the private server
```

Run `kill` when you finish. The server persists between invocations on purpose
— that is what makes the second `go diff` a round trip rather than a fresh open
— so a stale one makes the next session's first result confusing.

Read `survey` for `available`: a window-only target reports `false` on a machine
with no window host, which is the expected headless result and not a failure.

## Inspect the private server

```bash
tmux -L revier-drive list-panes -a -F '#{window_id} #{window_name} #{pane_current_command}'
tmux -L revier-drive display-message -p -t revier-drive: '#{window_id}'   # current window
```

`-L revier-drive` is what keeps this off the user's default server. A tmux
command without it addresses their real sessions.

## Verify an adapter against its real tool

```bash
mise run test:live      # L4 tmux and the CLI end-to-end, L5 sway; all headless
```

Each test starts a server on a socket named after itself and kills it in
cleanup, so suites cannot collide and a crashed run leaves at most one stray
server. Any substrate that is not installed skips rather than fails.

## When a screen is unavoidable

Only the GNOME window host needs the user's live session: `wctl` talks to a
GNOME Shell extension, which needs a real logged-in desktop. Nothing else does.

Before asking for a manual check, exhaust the substrates above — the core, the
resolution rules, run-or-raise, toggle-back, and every adapter except GNOME are
all reachable without one. Then hand the user a specific command and say what
to look for, rather than running it yourself:

```bash
wctl list --json | jq '.[] | {id, title, class}'   # read-only, safe to run
```

`wctl list` reads and never activates. Any `wctl activate` moves the user's
focus, so it belongs in a command they run, not one you run for them.
