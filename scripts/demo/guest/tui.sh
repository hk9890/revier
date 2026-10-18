#!/bin/sh
# Runs the TUI on the demo projects. Fifteen seconds in, web-shop's agent starts
# waiting, so the recording shows its row move to the top.
. /tmp/rvdemo/env.sh
sh /tmp/rvdemo/state.sh web-shop busy
( sleep 15; sh /tmp/rvdemo/state.sh web-shop waiting ) &
cd "$HOME"
exec revier
