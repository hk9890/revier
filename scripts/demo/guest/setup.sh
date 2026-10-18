#!/bin/sh
# Writes the demo projects and opens their workspaces on a private tmux server.
# Each agent is a fake: a pane named claude, a session file, a transcript.
set -eu
S=/tmp/rvdemo
. "$S/env.sh"
mkdir -p "$REVIER_CONFIG_HOME/projects" "$REVIER_STATE_HOME" "$CLAUDE_CONFIG_DIR/sessions" "$S/mise" "$TMUX_TMPDIR" "$HOME/dev"
printf '[hosts]\nruntime = ["tmux"]\nwindow = ["none"]\n' > "$REVIER_CONFIG_HOME/config.toml"
printf '#!/bin/sh\nprintf "["; sep=; for f in "$CLAUDE_CONFIG_DIR"/sessions/*.json; do [ -e "$f" ] && printf "$sep" && cat "$f" && sep=,; done; printf "]"\n' > "$S/bin/claude"
chmod +x "$S/bin/claude"

# project | status | pane title | last thing the agent said
cat > "$S/demo.txt" <<'D'
api-gateway|waiting|Rate limiter for /v2 routes|May I run the staging migration?
web-shop|busy|Checkout page: fix tax rounding|Running the cart tests.
billing|idle|Invoice PDF export|Done. All 214 tests pass.
infra|busy|Terraform: move DNS to the new zone|Planning 38 DNS record changes.
docs-site|-|-|-
D

cat > "$S/fake-claude.sh" <<'F'
printf '\033]2;%s\033\\' "$1"
printf '\n \033[1;38;5;209m*\033[0m Claude Code\n\n \033[2m>\033[0m %s\n\n \033[38;5;114m●\033[0m %s\n' "$1" "$2"
exec -a claude sleep 3600
F

while IFS='|' read -r p status title said; do
	mkdir -p "$HOME/dev/$p"
	cat > "$REVIER_CONFIG_HOME/projects/$p.toml" <<P
path = "$HOME/dev/$p"
git_url = "https://github.com/acme/$p"
[[target]]
name = "home"
home = true
  [target.runtime]
  name = "$p"
  match = { title = "^$p\$" }
  tabs = ["agent"]
[[target]]
name = "agent"
  [target.runtime]
  inside = "home"
    [[target.runtime.panels]]
    kind = "agent"
    command = ["bash", "$S/fake-claude.sh", "$title", "$said"]
    [[target.runtime.panels]]
    kind = "shell"
P
done < "$S/demo.txt"

cd /tmp
while IFS='|' read -r p status title said; do
	[ "$status" = - ] && continue
	revier open "$p" >/dev/null
done < "$S/demo.txt"
sleep 1

while IFS='|' read -r p status title said; do
	[ "$status" = - ] && continue
	sh "$S/state.sh" "$p" "$status"
	dir="$CLAUDE_CONFIG_DIR/projects/$(printf '%s' "$HOME/dev/$p" | sed 's/[^A-Za-z0-9]/-/g')"
	mkdir -p "$dir"
	printf '{"type":"user","timestamp":"2026-10-18T10:00:00Z","message":{"role":"user","content":"%s"}}\n{"type":"assistant","timestamp":"2026-10-18T10:01:00Z","message":{"role":"assistant","content":[{"type":"text","text":"%s"}]}}\n' "$title" "$said" > "$dir/demo-$p.jsonl"
done < "$S/demo.txt"

revier list
