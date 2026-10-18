#!/bin/sh
# Sets the status of a demo project's agent. Usage: state.sh <project> <busy|waiting|idle>
set -eu
. /tmp/rvdemo/env.sh
pid=$(cat "$S/pids/$1")
printf '{"pid": %s, "status": "%s", "sessionId": "demo-%s", "cwd": "%s"}' "$pid" "$2" "$1" "$HOME/dev/$1" > "$CLAUDE_CONFIG_DIR/sessions/$pid.json"
