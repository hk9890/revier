# A pane that looks like Claude Code and runs as "claude", so the probe claims it.
# Usage: fake-claude.sh <project> <title> <last message>
mkdir -p /tmp/rvdemo/pids
echo $$ > "/tmp/rvdemo/pids/$1"
printf '\033]2;%s\033\\' "$2"
clear
printf '\n \033[38;5;209m✻\033[0m \033[1mClaude Code\033[0m\n   \033[2m~/dev/%s\033[0m\n\n' "$1"
printf ' \033[2m>\033[0m %s\n\n' "$2"
printf ' \033[38;5;114m●\033[0m %s\n\n' "$3"
printf ' \033[2m────────────────────────────────────────────────\033[0m\n \033[2m>\033[0m \n'
exec -a claude sleep 3600
