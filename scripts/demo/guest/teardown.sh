#!/bin/sh
# Closes the demo and puts back what setup.sh changed.
S=/tmp/rvdemo
. "$S/env.sh"
revier shutdown --force >/dev/null 2>&1
pkill -f "$S/bin/[r]evier"
[ -f "$S/restore.sh" ] && sh "$S/restore.sh" >/dev/null
[ -f "$S/created" ] && xargs rmdir < "$S/created"
rm -rf "$REVIER_STATE_HOME" "$S"
