# Prints the primary monitor's mode, and with an argument sets it for this
# session only. Usage: display.py [1920x1080]
import sys
from gi.repository import Gio, GLib

bus = Gio.bus_get_sync(Gio.BusType.SESSION)
proxy = Gio.DBusProxy.new_sync(bus, 0, None, "org.gnome.Mutter.DisplayConfig",
                               "/org/gnome/Mutter/DisplayConfig", "org.gnome.Mutter.DisplayConfig")
serial, monitors, _, _ = proxy.call_sync("GetCurrentState", None, 0, -1, None).unpack()
connector, modes = monitors[0][0][0], monitors[0][1]
current = next(m for m in modes if m[6].get("is-current"))
print(f"{current[1]}x{current[2]}")
if len(sys.argv) > 1:
    mode = next(m for m in modes if m[0].startswith(sys.argv[1] + "@"))
    config = [(0, 0, 1.0, 0, True, [(connector, mode[0], {})])]
    proxy.call_sync("ApplyMonitorsConfig",
                    GLib.Variant("(uua(iiduba(ssa{sv}))a{sv})", (serial, 1, config, {})), 0, -1, None)
