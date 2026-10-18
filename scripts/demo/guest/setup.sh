#!/bin/sh
# Writes the demo projects, opens their workspaces as kitty windows, and sets
# the agents' states. Points the trigger key at popup.sh, and sets a plain
# background and 1920x1080. Writes restore.sh, which teardown.sh runs.
set -eu
S=/tmp/rvdemo
. "$S/env.sh"
mkdir -p "$REVIER_CONFIG_HOME/projects" "$REVIER_STATE_HOME" "$CLAUDE_CONFIG_DIR/sessions" "$S/mise" "$S/pids"
printf '#!/bin/sh\nprintf "["; sep=; for f in "$CLAUDE_CONFIG_DIR"/sessions/*.json; do [ -e "$f" ] && printf "$sep" && cat "$f" && sep=,; done; printf "]"\n' > "$S/bin/claude"
chmod +x "$S/bin/claude" "$S/popup.sh"
printf '%s\n' "PS1='\\[\\e[38;5;111m\\]\\w\\[\\e[0m\\] \$ '" > "$S/bashrc"

media=org.gnome.settings-daemon.plugins.media-keys
trigger=
for path in $(gsettings get $media custom-keybindings | tr -d "[],'"); do
	case $(gsettings get "$media.custom-keybinding:$path" command) in
	*"revier popup"*) trigger=$path ;;
	esac
done
[ -n "$trigger" ] || { echo "no desktop key runs revier popup: run revier keys install in the VM" >&2; exit 1; }
background=org.gnome.desktop.background
quoted() { printf "'%s'" "$(printf '%s' "$1" | sed "s/'/'\\\\''/g")"; }
saved() { echo "gsettings set $(quoted "$1") $2 $(quoted "$(gsettings get "$1" "$2")")"; }
{
	saved "$media.custom-keybinding:$trigger" command
	saved $background picture-options
	saved $background primary-color
	echo "/usr/bin/python3 $S/display.py $(/usr/bin/python3 "$S/display.py")"
} > "$S/restore.sh"

cat > "$REVIER_CONFIG_HOME/config.toml" <<'C'
[ui]
theme = "catppuccin-mocha"
glyphs = "nerd"

[hosts]
runtime = ["kitty"]
window = ["gnome"]
C

# project | status | title | last message | minutes since it
cat > "$S/demo.txt" <<'D'
api-gateway|waiting|Rate limiter for /v2 routes|May I run the staging migration?|2
web-shop|busy|Checkout page: fix tax rounding|Running the cart tests.|1
billing|idle|Invoice PDF export|Done. All 214 tests pass.|14
infra|busy|Terraform: move DNS to the new zone|Planning 38 DNS record changes.|4
docs-site|-|-|-|-
D

while IFS='|' read -r p status title said ago; do
	[ -e "$HOME/dev/$p" ] || { mkdir -p "$HOME/dev/$p" && echo "$HOME/dev/$p" >> "$S/created"; }
	cat > "$REVIER_CONFIG_HOME/projects/$p.toml" <<P
path = "$HOME/dev/$p"
git_url = "https://github.com/acme/$p"
[[target]]
name = "home"
home = true
  [target.runtime]
  name = "$p"
  match = { title = "^$p\$" }
  place = "right top 70% 100%"
  tabs = ["agent"]
[[target]]
name = "agent"
  [target.runtime]
  inside = "home"
    [[target.runtime.panels]]
    kind = "agent"
    command = ["bash", "$S/fake-claude.sh", "$p", "$title", "$said"]
    [[target.runtime.panels]]
    kind = "shell"
    command = ["bash", "--rcfile", "$S/bashrc", "-i"]
P
done < "$S/demo.txt"

/usr/bin/python3 "$S/display.py" 1920x1080 >/dev/null
gsettings set "$media.custom-keybinding:$trigger" command "$S/popup.sh"
gsettings set $background picture-options none
gsettings set $background primary-color '#11111b'

cd "$HOME"
for p in infra web-shop api-gateway billing; do
	revier open "$p" >/dev/null
	sleep 2
done

while IFS='|' read -r p status title said ago; do
	[ "$status" = - ] && continue
	sh "$S/state.sh" "$p" "$status"
	dir="$CLAUDE_CONFIG_DIR/projects/$(printf '%s' "$HOME/dev/$p" | sed 's/[^A-Za-z0-9]/-/g')"
	mkdir -p "$dir"
	asked=$(date -u -d "-$((ago + 1)) min" +%Y-%m-%dT%H:%M:%SZ)
	answered=$(date -u -d "-$ago min" +%Y-%m-%dT%H:%M:%SZ)
	printf '{"type":"user","timestamp":"%s","message":{"role":"user","content":"%s"}}\n{"type":"assistant","timestamp":"%s","message":{"role":"assistant","content":[{"type":"text","text":"%s"}]}}\n' "$asked" "$title" "$answered" "$said" > "$dir/demo-$p.jsonl"
done < "$S/demo.txt"

revier list
