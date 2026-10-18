# The demo's environment in the VM: the logged-in GNOME session, a scratch
# revier configuration, and a fake claude. Sourced by the other guest scripts.
# HOME stays, so kitty keeps its own configuration.
S=/tmp/rvdemo
export XDG_RUNTIME_DIR=/run/user/$(id -u)
export DBUS_SESSION_BUS_ADDRESS=unix:path=$XDG_RUNTIME_DIR/bus
export WAYLAND_DISPLAY=wayland-0 XDG_CURRENT_DESKTOP=ubuntu:GNOME XDG_SESSION_TYPE=wayland
export REVIER_CONFIG_HOME=$S/config REVIER_STATE_HOME=$HOME/.local/state/revier-demo
export REVIER_SSH_CONFIG=$S/ssh_config MISE_STATE_DIR=$S/mise
export CLAUDE_CONFIG_DIR=$S/claude
export PATH="$S/bin:$HOME/.local/bin:$HOME/.local/share/mise/shims:$PATH"
