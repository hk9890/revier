# The scratch substrate of docs/RUNNING.md, under /tmp/rvdemo. Sourced by the
# other guest scripts. HOME moves too, so the TUI shows ~/dev/<project>.
S=/tmp/rvdemo
export MISE_DATA_DIR="${MISE_DATA_DIR:-$HOME/.local/share/mise}"   # a mise shim finds its tool after HOME moves
export MISE_CONFIG_DIR="${MISE_CONFIG_DIR:-$HOME/.config/mise}"
export HOME=$S/home
export TMUX_TMPDIR=$S/tmux
unset TMUX
export REVIER_CONFIG_HOME=$HOME/.config/revier REVIER_STATE_HOME=$HOME/.local/state/revier
export REVIER_SSH_CONFIG=$S/ssh_config MISE_STATE_DIR=$S/mise
export CLAUDE_CONFIG_DIR=$S/claude
export PATH="$S/bin:$PATH"
export TERM=xterm-256color COLORTERM=truecolor
