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

Run `mise run build` first. The recipes call `./bin/revier`, and a fresh
worktree has none.

## Drive the CLI against a scratch configuration

`REVIER_CONFIG_HOME` and `REVIER_STATE_HOME` redirect revier away from the
user's projects and state. `TMUX_TMPDIR` and `unset TMUX` move revier's tmux
runtime, and every `tmux` command after them, onto a private server. Set all
four, every time, in the one shell that runs the recipes below.

```bash
S=$(mktemp -d); mkdir -p "$S/projects" "$S/state"
export TMUX_TMPDIR=$(mktemp -d /tmp/rv.XXXX)   # short: tmux fails on a socket path over 107 bytes
unset TMUX                                     # inside tmux, $TMUX names the user's server
printf '[hosts]\nruntime = ["tmux"]\nwindow = ["none"]\n' > "$S/config.toml"   # never touch a real window
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

tmux kill-server; rm -rf "$S" "$TMUX_TMPDIR"   # the private server only
```

Keep the `[hosts]` line. Without `runtime = ["tmux"]` the workspace opens as a
real kitty OS window; without `window = ["none"]` a window target launches a
real application and moves the user's focus.

`revier new` and `revier open <unknown name>` write into
`$REVIER_CONFIG_HOME/projects` and run `mise trust` on the directory. Export
`MISE_STATE_DIR=$S/mise` as well, so the trust lands outside the user's list.
Point a `git_url` you test cloning with at a local bare repository
(`git init --bare $S/origin.git`).

To exercise the agent monitor, launch the pane as `claude`, so the Claude probe
claims it, and give it a Claude-style title. The leading glyph is the state
signal:

```bash
launch = ["bash", "-c", "exec -a claude sleep 600"]   # in the target, instead of sleep
tmux select-pane -t agent -T '⠧ Working on it'   # spinner -> running
tmux select-pane -t agent -T '✳ Ready'           # at rest  -> idle
./bin/revier agent wait demo --until idle --timeout 5    # exit 0, or 2 on timeout
./bin/revier agent prompt demo 'hello'                    # types into that pane only; warns, as sleep never starts a turn
tmux capture-pane -p -t agent                             # the text arrived
```

`revier agent prompt` types into a pane. Run it only with the scratch
configuration above: without it, it reaches the user's real agent.

## Drive the TUI without a screen

The TUI needs a terminal, and a tmux pane is one. Run it on a private server
against the scratch configuration above and read the screen back:

```bash
t() { tmux -L revier-tui "$@"; }                # a function: zsh does not split a $T variable
t new-session -d -s tui -x 100 -y 20 "REVIER_CONFIG_HOME=$S REVIER_STATE_HOME=$S/state ./bin/revier"
t capture-pane -p -t tui                        # the rows, as rendered
t send-keys -t tui d e m Tab                    # type to filter, Tab lists the targets
for k in back clear quit; do t send-keys -t tui Escape; sleep 0.3; done   # one at a time: two quick Escapes read as alt+Escape
t kill-server
```

Enter on a project runs its home target, so press it only on a scratch project
whose launch is harmless, such as `sh -c "sleep 600"`.

To see the agent line change without a keypress, retitle the agent pane on the
scratch server while the TUI runs, then capture again after a refresh:

```bash
tmux select-pane -t agent -T '⠧ Working on it'
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

Run `kill` when you finish. The server persists between invocations on purpose,
so a stale one skews the next session's first result.

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

Two hosts need the user's live session: GNOME, because `wctl` talks to a Shell
extension in a logged-in desktop, and kitty's `Open` and `Focus`, because they
create and raise real OS windows. Everything else is reachable headless.

Before asking for a manual check, exhaust the substrates above — the core, the
resolution rules, run-or-raise, toggle-back, and every adapter except those two
are all reachable without a screen. Then hand the user a specific command and
say what to look for, rather than running it yourself:

```bash
wctl list --json | jq '.[] | {id, title, wm_class}'   # read-only, safe to run
kitten @ ls | jq '.[] | {id, wm_name, is_focused}'    # read-only, safe to run
```

`wctl list` and `kitten @ ls` read and never activate. Any `wctl activate`,
`revier open`, or `revier go` against a real host moves the user's focus, so it
belongs in a command they run, not one you run for them.

When the user has handed over the screen, verify the kitty host on a scratch
project whose name collides with none of theirs, and close what it opened:

```bash
export REVIER_CONFIG_HOME=$S REVIER_STATE_HOME=$S/state   # $S from the recipe above, with runtime = ["kitty"]
./bin/revier open demo                                    # a kitty OS window titled by the home realization's name
kitten @ ls | jq '.[] | select(.wm_name=="session:demo") | .tabs[].windows[] | {title, user_vars}'
wctl focused --json | jq .title                           # session:demo
./bin/revier go editor -p demo; ./bin/revier go editor -p demo   # away, and back to the workspace
kitten @ close-window --match 'title:^session:demo$'      # or close-window per window id from ls
```

Every `kitten @` command above addresses the socket of the kitty it runs
inside. Pass `--to unix:@kitty-<pid>` to reach another process; the pids are
the kitty entries in `wctl list --json`.

## Verify a keybinding without pressing it

A binding is a command. `revier keys status --json` prints the one each key
would carry. Run it by hand, with the same login shell the binding uses, and
watch focus:

```bash
sh -lc 'revier go editor -p demo'; wctl focused --json | jq .title
sh -lc 'revier go editor -p demo'; wctl focused --json | jq .title   # back at the workspace
hyperfine --warmup 3 -N './bin/revier go home -p demo'               # the keypress budget
```

`revier keys install` and `revier keys uninstall` rewrite the user's desktop
configuration, so they are the user's step and never a verification step. Their
`--dry-run` reads and prints only, and is safe to run. `dconf` writes go through
the session's own service, so `DCONF_PROFILE` does not isolate them: there is no
scratch desktop to install into, and the layers above are where this is proved.
