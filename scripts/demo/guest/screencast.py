# Records the whole screen with GNOME's own screencast, at 30 fps, until it gets
# SIGTERM. GNOME stops a screencast when its caller leaves the session bus, so
# this process holds the connection for the whole clip.
# Usage: screencast.py <out.webm>
import signal
import sys
from gi.repository import Gio, GLib

bus = Gio.bus_get_sync(Gio.BusType.SESSION)
proxy = Gio.DBusProxy.new_sync(bus, 0, None, "org.gnome.Shell.Screencast",
                               "/org/gnome/Shell/Screencast", "org.gnome.Shell.Screencast")
options = {"framerate": GLib.Variant("i", 30), "draw-cursor": GLib.Variant("b", False)}
ok, _ = proxy.call_sync("Screencast", GLib.Variant("(sa{sv})", (sys.argv[1], options)), 0, -1, None).unpack()
if not ok:
    sys.exit("GNOME refused the screencast")
print("recording", flush=True)

loop = GLib.MainLoop()
GLib.unix_signal_add(GLib.PRIORITY_DEFAULT, signal.SIGTERM, loop.quit)
loop.run()
proxy.call_sync("StopScreencast", None, 0, -1, None)
