# Running

Call the Skill tool with `run` for the generic launch-and-drive flow.
**Local delta:** everything below wins where the two disagree.
The automated suites are [TESTING.md](TESTING.md)'s.

## The rule

revier manipulates the user's live desktop: it opens terminal windows, raises
editors, and switches focus. **Never verify a change by driving the user's
session.** A stray `Open` steals focus mid-sentence, and a stray `Focus` moves
a window the user is reading.

Every recipe below runs on a private substrate instead: an hc core of its
own, shown in no view, or a werft lease, whose desktop is its own
([Drive the desktop on a werft lease](#drive-the-desktop-on-a-werft-lease)).
Nothing here reaches the user's display, hc core or service manager.

`revier keys install`, `revier term install` and their `uninstall` rewrite the
user's desktop and kitty configuration, the kitty one often a link into a
dotfiles repository. On this machine, leave them to the user, except `term` on
a scratch kitty directory; `keys status`, `term status` and `--dry-run` read
only. In a lease, run them.

Run `mise run build` first. The recipes call `./bin/revier`, and a fresh
worktree has none.

## Drive the CLI against a scratch configuration

`REVIER_CONFIG_HOME` and `REVIER_STATE_HOME` redirect revier away from the
user's projects and state. `hcp` runs a command on a private hc core: its own
config, state, runtime directory and socket, and no session bus. Any `hc`
command starts a core when none answers its socket, so run every `hc` and
every `./bin/revier` through `hcp`. revier starts a core as the unit
`revier-hc-<socket hash>` of the user's service manager; under `hcp` it reaches no manager
and starts the core detached. Set all of it, every time, in the one shell
that runs the recipes below. `REVIER_SSH_CONFIG` points the remote screen's
"new" (alt+r, then alt+n) and the check for this machine at a scratch ssh
configuration instead of `~/.ssh/config`; set it whenever a recipe reaches
the remote screen.

```bash
S=$(mktemp -d); mkdir -p "$S/projects" "$S/state"
C=$(mktemp -d /tmp/hc.XXXX); mkdir -m 700 "$C/run"   # short: a socket path over 107 bytes cannot be bound
hcp() { env -u DBUS_SESSION_BUS_ADDRESS -u HC_TERMINAL_ID -u HC_TERMINAL_SOCKET -u HC_SESSION SHELL=/bin/sh XDG_CONFIG_HOME=$C XDG_STATE_HOME=$C XDG_RUNTIME_DIR=$C/run HC_SOCKET=$C/s.sock "$@"; }
printf '[hosts]\nruntime = ["hc"]\nview = ["none"]\nwindow = ["none"]\n' > "$S/config.toml"   # never touch a real window
cat > "$S/projects/demo.toml" <<EOF
path = "$S"
[[target]]
name = "home"
home = true
  [target.runtime]
  name = "home"
  match = { title = "^home$" }
  tabs = ["agent"]
[[target]]
name = "agent"
  [target.runtime]
  inside = "home"                                         # panels are on a tab alone
    [[target.runtime.panels]]
    kind = "agent"
    command = ["bash", "-c", "exec -a claude sleep 600"]   # the Claude probe claims a terminal named claude
    [[target.runtime.panels]]
    kind = "shell"
EOF
export REVIER_CONFIG_HOME=$S REVIER_STATE_HOME=$S/state
tabs() { for a in $(hcp hc ls --json | jq -r '.terminals[].address'); do hcp hc get "$a" --json | jq -c '{address, tab: .labels["revier.tab"], place: .labels["revier.tabplace"], argv: .foreground.argv}'; done; }

hcp ./bin/revier status -p demo    # which hosts were selected
hcp ./bin/revier doctor            # what did not load whole, and its fix; last line: runtime hc is shown by no view
hcp ./bin/revier open demo         # run-or-raise the workspace: its tabs as hc terminals
tabs                               # each terminal, its tab's token and place
hcp ./bin/revier list              # agent state and target availability
hcp ./bin/revier list --json       # what both renderers read
```

Keep the `[hosts]` lines. Without `runtime = ["hc"]` the workspace opens as
a real kitty OS window; without `view = ["none"]` kitty shows hc in windows
on the user's desktop; without `window = ["none"]` a window target launches a
real application and moves the user's focus.

Leave the core and `$S` in place for the recipes below;
[Clean up](#clean-up) removes them.

`revier new` and `revier open <unknown name>` write into
`$REVIER_CONFIG_HOME/projects` and run `mise trust` on the directory. Export
`MISE_STATE_DIR=$S/mise` as well, so the trust lands outside the user's list.
Point a `git_url` you test cloning with at a local bare repository
(`git init --bare $S/origin.git`).

Do not run `revier open --attach` here: the attach takes over the shell
that runs it.

To exercise the agent monitor, use the demo's agent panel. Its state is what
`claude agents --json` lists for the terminal's pid (`docs/design/decisions.md`
D57), so put a `claude` first on PATH that lists the files of a scratch
sessions directory, and write the terminal's file there. hc reads a
terminal's foreground process about twice a second, so `agentpane` finds a
new agent a moment after it starts:

```bash
export CLAUDE_CONFIG_DIR=$S/claude; mkdir -p "$S/bin" "$CLAUDE_CONFIG_DIR/sessions"
printf '#!/bin/sh\nprintf "["; sep=; for f in "$CLAUDE_CONFIG_DIR"/sessions/*.json; do [ -e "$f" ] && printf "$sep" && cat "$f" && sep=,; done; printf "]"\n' > "$S/bin/claude"; chmod +x "$S/bin/claude"
export PATH="$S/bin:$PATH"                            # every revier below asks this claude, never the real one
agentpane() {                                         # the workspace's agent terminal, the first listed; again after a restart
  set -- $(for a in $(hcp hc ls --json | jq -r '.terminals[].address'); do hcp hc get "$a" --json | jq -r 'select(.foreground.argv[0]=="claude") | "\(.foreground.pid) \(.address)"'; done | head -1)
  pid=$1 pane=$2
}
agentpane
st() { printf '{"pid": %s, "status": "%s"}' "$pid" "$1" > "$CLAUDE_CONFIG_DIR/sessions/$pid.json"; }
st busy       # -> running
st waiting    # -> attention
st idle       # -> idle
hcp ./bin/revier agent wait demo --until idle --timeout 5    # exit 0, or 2 on timeout
hcp ./bin/revier agent prompt demo 'hello'                    # types into that terminal only; warns, as sleep never starts a turn
hcp hc read "$pane"                                           # the text arrived
hcp ./bin/revier agent send-keys demo x enter                 # typed in any state, st waiting included
hcp ./bin/revier agent read demo --screen --lines 5           # the terminal's last lines, as plain text
hcp ./bin/revier agent read demo                              # refused: the session file names no conversation
hcp ./bin/revier agent new -p demo --no-focus                 # prints demo:home:<address>, a tab of its own token
tabs                                                          # the new tab's two terminals, after the layout's
```

`revier agent prompt` and `revier agent send-keys` type into a terminal, and
`revier agent close` closes one. Run them only with the scratch configuration
above: without it, they reach the user's real agent.

## Drive save and restore across a reboot

`hcp hc server stop` is the reboot: every terminal and binding is gone,
project files and the saved session stay.

```bash
hcp ./bin/revier session save --name before-reboot   # what is open now
hcp ./bin/revier session list                        # id, reason, label, counts
ls "$REVIER_STATE_HOME/sessions"                     # <id>.toml: names, conversations, dirs; never a host or argv
hcp hc server stop                                   # the private core only
hcp ./bin/revier session restore --dry-run           # what it would open
hcp ./bin/revier session restore                     # the newest whole save, in file order
agentpane                                            # the restore started a new agent terminal
```

The save asks `claude agents --json` which conversation each agent
terminal's process holds, and in which directory. To run the resume path,
give the terminal's file a `sessionId` and a `cwd`, with the fake `claude`
from the agent monitor recipe on PATH. File a transcript for the
conversation too: revier resumes only a conversation Claude Code has filed.

```bash
printf '{"pid": %s, "status": "idle", "sessionId": "abc-123", "cwd": "%s"}' "$pid" "$S" > "$CLAUDE_CONFIG_DIR/sessions/$pid.json"
mkdir -p "$CLAUDE_CONFIG_DIR/projects/-filed" && echo '{"type":"user"}' > "$CLAUDE_CONFIG_DIR/projects/-filed/abc-123.jsonl"
hcp ./bin/revier session save   # prints "1 agent conversation recorded"
```

To run the agents past the layout, open more agents before the save, each in
its own directory, and list each one's pid the same way. The restore opens
each as a tab of the workspace:

```bash
hcp ./bin/revier agent new -p demo --dir "$S/state"
tabs                                             # the layout, then one tab per agent
```

Run a real `claude` in the terminal instead to check against Claude Code
itself: answer its trust prompt for the directory with "Yes", since the
default exits.

To drive a shutdown, open the workspace again and close it on the private
core only:

```bash
hcp ./bin/revier open demo; agentpane            # the terminal st writes for
hcp ./bin/revier shutdown --dry-run              # what it would close
st busy; hcp ./bin/revier shutdown               # refused, names the agent
hcp ./bin/revier shutdown --force                # saves when changed, then closes
hcp hc ls                                        # no terminal left
```

## Drive a link without a host

Run the agent monitor recipe first: this one needs its `$S/bin` on PATH and its
session file.

Put a stand-in `ssh` first on PATH that runs the panel's command line here,
under a second configuration that plays the host:

```bash
H=$(mktemp -d); mkdir -p "$H/projects" "$H/state"            # the host: no runtime, it is reached over ssh alone
printf '[hosts]\nruntime = ["none"]\nwindow = ["none"]\n' > "$H/config.toml"
cp "$S/projects/demo.toml" "$H/projects/demo.toml"           # with the demo's agent panel
printf '[remote]\nhost = "fakehost"\nproject = "demo"\n' > "$S/projects/far.toml"
printf '#!/bin/sh\nshift\nexec sh -c "$1"\n' > "$S/bin/loginsh"   # a login shell that keeps this PATH
printf '#!/bin/sh\nwhile [ "$1" != "--" ]; do shift; done; shift; shift\nREVIER_CONFIG_HOME=%s REVIER_STATE_HOME=%s/state SHELL=%s/bin/loginsh exec sh -c "$1"\n' "$H" "$H" "$S" > "$S/bin/ssh"
chmod +x "$S/bin/ssh" "$S/bin/loginsh"; ln -s "$PWD/bin/revier" "$S/bin/revier"

hcp ./bin/revier open far                                    # two terminals, each the stand-in ssh become the host's panel
REVIER_CONFIG_HOME=$H ./bin/revier list --json demo          # the host's view: the agent under its tag, on host "proc"
hcp ./bin/revier list --json far                             # the view here: the same agent, by its terminal here
```

The agent's pid for the session file is the process whose environment carries
`REVIER_TAG`. `revier agent prompt`, `wait`, `new`, `session save` and
`shutdown --agents` then run against `far` as they do against `demo`.

## Drive the TUI without a screen

The TUI needs a terminal, and an hc terminal is one. Run it on the private
core against the scratch configuration above and read the screen back:

```bash
hcp hc run --bg --name tui ./bin/revier         # the TUI, in a terminal of the private core
hcp hc read tui                                 # the rows, as rendered
hcp hc keys tui d e m alt+t                     # type to filter, alt+t moves the cursor onto the targets
for k in back clear quit; do hcp hc keys tui esc; sleep 0.3; done   # one at a time: two quick Escapes read as alt+Escape
hcp hc kill tui; hcp hc rm tui
```

Enter on a project runs its home target, so press it only on a scratch project
whose panels are harmless, such as the demo's.

To see the agent line change without a keypress, write the agent terminal's
session file while the TUI runs, then read the screen again after a refresh.
Start the TUI with the fake `claude` and `CLAUDE_CONFIG_DIR` from the agent
monitor recipe in its environment:

```bash
st waiting    # the row moves to the top as attention
```

## Drive the core without any configuration

`scripts/drive` builds a project in memory and runs it against an hc core of
its own in `$TMPDIR/revier-drive-<uid>`, private to it, with no view.

```bash
go run ./scripts/drive go home      # run-or-raise the workspace
go run ./scripts/drive go diff      # opens diff
go run ./scripts/drive go diff      # raises the same diff: with no view nothing reports the focus
go run ./scripts/drive survey       # the view both renderers read, as JSON
go run ./scripts/drive kill         # stop the private core and remove its directory
```

Expect one WARN on the first `go`, `the service manager did not start the
unit; starting it detached`: the driver's environment reaches no service
manager, so the core starts detached, as it should.

Run `kill` when you finish. The core persists between invocations on purpose,
so a stale one skews the next session's first result.

Read `survey` for `available`: a window-only target reports `false` on a machine
with no window host, which is the expected headless result and not a failure.

## Record the README clips

`scripts/demo/record` re-records the README's four clips in
`.github/assets/`: `popup.webp` (search and jump), `agents.webp` (the agent
list), `tools.webp` (the editor and ticket keys) and `restore.webp` (shutdown
and restore). It drives a libvirt VM's GNOME desktop with `virsh send-key` and
records it with GNOME's own screencast, so nothing reaches this desktop:

```bash
scripts/demo/record <lease-id>          # builds revier, writes .github/assets/*.webp
scripts/demo/record <lease-id> /tmp/x   # anywhere else, to compare first
```

- Give it a new lease that `scripts/lease-desktop` prepared, as
  [Drive the desktop on a werft lease](#drive-the-desktop-on-a-werft-lease)
  says, but in tier `full`: the clips take kitty's theme, font and window
  frame from the user's setup, which a `plain` lease lacks. A lease that ran
  other checks first keeps their workspace kitty, and the popup shows their
  projects.
- This machine needs ffmpeg with libwebp, `python3-pil` for `/usr/bin/python3`,
  and the JetBrainsMono Nerd Font, which draws the keycaps.
- The recording uses a scratch configuration and fake agents. The script
  points the trigger key and the editor and ticket keys at them, sets a plain
  background and 1920x1080, and puts all of it back when it exits.
- Open each clip in a browser before you commit: ffmpeg cannot decode an
  animated WebP.

## Drive the desktop on a werft lease

The GNOME host, kitty's `Open` and `Focus`, the kitty view over hc, the
desktop keys and a real reboot need a GNOME session. Drive them on a werft
lease, a disposable VM.

Call the Skill tool with `werft:werft` for the lease flow.
**Local delta:**

- Lease `ubuntu-26-04` in tier `plain` with `--memory 4096`: its image ships
  GNOME.
- Run `mise run build`, then `scripts/lease-desktop <lease-id>`. It logs GNOME
  in, installs kitty, `wctl`, hc and the tools below, puts `./bin/revier` on
  the lease's PATH and runs `revier keys install` there.
- Run revier and hc in the lease without `REVIER_CONFIG_HOME` or `hcp`: its
  configuration, state and hc core are scratch already.
- Write each block below to a file under `hako-private/`, copy it in with
  `scp -F <ssh_config> <file> <lease-id>:`, and run it in one ssh call:
  `ssh -F <ssh_config> <lease-id> 'sh ./<file>'`. A variable then lives
  across its lines, and the block's quotes need no escaping.
- Start a block that reaches the desktop with `. ./session.sh`: it sets the
  GNOME session's environment and defines `kitty_socket` and `gnome_wait`.
- After a rebuild, run `scripts/lease-desktop <lease-id>` again: it replaces
  `revier` in the lease, also while the popup runs it.
- Send a key the way the user presses it to the lease's `domain`, which
  `werft --json leases` prints: `virsh -c qemu:///system send-key <domain> KEY_LEFTALT KEY_SPACE`.

Write the demo project of the scratch configuration recipe in the lease as
`~/.config/revier/projects/demo.toml`, with `path` set to the lease's `$HOME`.
Then verify the kitty host:

```bash
. ./session.sh
printf '[hosts]\nruntime = ["kitty"]\nwindow = ["gnome"]\n' > ~/.config/revier/config.toml   # gnome raises the OS window
revier open demo                                  # a kitty OS window titled home
wctl focused --json | jq .title                   # home
W=$(kitty_socket)
kitten @ --to "$W" ls | jq '.[] | select(.wm_name=="home") | .tabs[].windows[] | {id, title, user_vars}'
revier go agent -p demo; revier go agent -p demo  # the agent tab, and back to home
env PATH=/usr/bin:/bin sh -lc 'revier go agent -p demo'; wctl focused --json | jq .title   # home: the login shell the desktop runs a key in finds revier
hyperfine --warmup 3 -N 'revier go home -p demo'  # the keypress budget
for id in $(kitten @ --to "$W" ls | jq '.[] | select(.wm_name=="home") | .tabs[].windows[].id'); do kitten @ --to "$W" close-window --match id:$id; done
```

Then the kitty view over hc. Each view window runs `hc attach` of one
terminal:

```bash
. ./session.sh
printf '[hosts]\nruntime = ["hc"]\nwindow = ["gnome"]\n' > ~/.config/revier/config.toml
revier open demo                                  # one kitty OS window titled home, a tab per tab
W=$(kitty_socket)
kitten @ --to "$W" ls | jq '.[] | select(.wm_name=="home") | .tabs[].windows[] | {id, user_vars}'   # REVIER_VIEW_PANEL names each terminal
revier go agent -p demo; revier go agent -p demo  # the agent tab, and back to home
for id in $(kitten @ --to "$W" ls | jq '.[] | select(.wm_name=="home") | .tabs[].windows[].id'); do kitten @ --to "$W" close-window --match id:$id; done
hc ls --json | jq '.terminals | length'           # 2: the terminals run on
revier open demo                                  # the window again, over the same terminals
revier shutdown --force                           # the terminals end, and their windows with them
```

A `kitten @` command without `--to` addresses the socket of the kitty it runs
inside. Pass `--to unix:$XDG_RUNTIME_DIR/kitty-<pid>` to reach another
process; the pids are the kitty entries in `wctl list --json`. A kitty that
`kitty.conf` puts on `listen_on unix:@kitty` is at `--to unix:@kitty-<pid>`;
revier does not look for it.

To drive save and restore across a real reboot, run the first block, reboot
with `ssh -F <ssh_config> <lease-id> 'sudo -n systemctl reboot'`, wait for ssh
as the werft skill says, then run the second:

```bash
. ./session.sh
revier open demo                                  # something to save
revier session save --name before-reboot
```

```bash
. ./session.sh
gnome_wait                                        # GNOME logs in by itself, after ssh answers
revier session restore before-reboot
wctl focused --json | jq .title                   # home
```

## Verify a keybinding without pressing it

A binding is a command. Run `./bin/revier keys status --json` to print the one
each key would carry; it reads and never binds. Run the command itself, or
press the key, on a lease, as
[Drive the desktop on a werft lease](#drive-the-desktop-on-a-werft-lease) says.

## Verify the terminal keys without the user's kitty

Run `revier term install` and `revier term uninstall` on a scratch kitty
directory, with `$S` from the scratch configuration recipe:

```bash
export KITTY_CONFIG_DIRECTORY=$S/kitty; mkdir -p "$KITTY_CONFIG_DIRECTORY"
printf 'map ctrl+shift+t new_tab\n' > "$KITTY_CONFIG_DIRECTORY/kitty.conf"
./bin/revier term install       # writes $S/kitty/revier.conf and the include
./bin/revier term status        # each key written; ctrl+shift+t shadowed by kitty.conf:3
./bin/revier term uninstall     # kitty.conf as it was
```

To try a generated map, run `revier term install` in a lease, as
[Drive the desktop on a werft lease](#drive-the-desktop-on-a-werft-lease) says,
and press the key in the lease's kitty.

## Clean up

Run this in the shell that ran the recipes, once the last recipe that uses
`$S` is done; they share its state:

```bash
[ -n "$C" ] && hcp hc server stop   # the private core only; ends every terminal it runs
rm -rf "$S" "$H" "$C"
```
