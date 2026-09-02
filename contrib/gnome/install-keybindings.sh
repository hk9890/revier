#!/bin/sh
# Installs revier's GNOME keybindings beside the ones already configured.
#
# Loads revier-keybindings.dconf under the custom-keybindings tree and appends
# its four paths to the custom-keybindings list. Existing entries are neither
# rewritten nor reordered, and running this twice changes nothing. To remove
# the bindings, delete the four paths from the list in Settings > Keyboard.
set -eu

here=$(cd "$(dirname "$0")" && pwd)
schema=org.gnome.settings-daemon.plugins.media-keys
base=/org/gnome/settings-daemon/plugins/media-keys/custom-keybindings

current=$(gsettings get "$schema" custom-keybindings)
new=$current
for entry in revier-popup revier-home revier-editor revier-web; do
	path="$base/$entry/"
	case "$current" in *"'$path'"*) continue ;; esac
	case "$new" in
		"@as []") new="['$path']" ;;
		*) new="${new%]}, '$path']" ;;
	esac
done

dconf load "$base/" < "$here/revier-keybindings.dconf"
gsettings set "$schema" custom-keybindings "$new"
echo "custom-keybindings: $new"
