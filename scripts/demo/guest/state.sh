#!/bin/sh
# Sets the status of a demo project's agent. Usage: state.sh <project> <busy|waiting|idle>
set -eu
. /tmp/rvdemo/env.sh
pid=$(tmux list-panes -a -F '#{session_name} #{pane_pid} #{pane_current_command}' | awk -v s="$1" '$1==s && $3=="claude"{print $2; exit}')
[ -n "$pid" ] || { echo "no agent pane for $1" >&2; exit 1; }
printf '{"pid": %s, "status": "%s", "sessionId": "demo-%s", "cwd": "%s"}' "$pid" "$2" "$1" "$HOME/dev/$1" > "$CLAUDE_CONFIG_DIR/sessions/$pid.json"
