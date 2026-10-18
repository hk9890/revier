# Turns the captured frames and events.log into the GIF, with each pressed key
# and each caption drawn on. Usage: compose.py <work dir> <output.gif>
import os
import subprocess
import sys

work, out = sys.argv[1], os.path.abspath(sys.argv[2])
font = subprocess.run(["fc-match", "-f", "%{file}", "JetBrainsMono Nerd Font:bold"],
                      capture_output=True, text=True, check=True).stdout

events = [line.rstrip("\n").split(" ", 2) for line in open(f"{work}/events.log")]
start = next(int(e[0]) for e in events if e[1] == "start")
end = next(int(e[0]) for e in events if e[1] == "end")
seconds = lambda ns: (ns - start) / 1e9
length = seconds(end)

frames = sorted(int(f[:-4]) for f in os.listdir(f"{work}/frames") if f.endswith(".ppm"))
frames = [f for f in frames if start <= f <= end]
with open(f"{work}/concat.txt", "w") as concat:
    for frame, following in zip(frames, frames[1:] + [end]):
        concat.write(f"file 'frames/{frame}.ppm'\nduration {(following - frame) / 1e9:.3f}\n")
    concat.write(f"file 'frames/{frames[-1]}.ppm'\n")


def shown(kind, longest):
    """Each mark of a kind, shown until the next one of its kind, for at most longest seconds."""
    marks = [(seconds(int(e[0])), e[2]) for e in events if e[1] == kind]
    for i, (at, text) in enumerate(marks):
        following = marks[i + 1][0] if i + 1 < len(marks) else length
        yield at, min(at + longest, following), text


draw = []
for at, until, text in shown("caption", length):
    draw.append(f"drawtext=fontfile={font}:text='{text}':fontsize=34:fontcolor=white:box=1:"
                f"boxcolor=0x7287fd@0.92:boxborderw=18:x=(w-tw)/2:y=h-th-150:"
                f"enable='between(t,{at:.2f},{until:.2f})'")
for at, until, text in shown("key", 1.4):
    draw.append(f"drawtext=fontfile={font}:text='{text}':fontsize=64:fontcolor=white:box=1:"
                f"boxcolor=0x11111b@0.88:boxborderw=26:x=(w-tw)/2:y=(h-th)/2+260:"
                f"enable='between(t,{at:.2f},{until:.2f})'")

video = f"{work}/demo.mp4"
subprocess.run(["ffmpeg", "-v", "error", "-y", "-f", "concat", "-safe", "0", "-i", "concat.txt",
                "-vf", ",".join(draw + ["fps=8", "scale=1280:-1:flags=lanczos"]),
                "-pix_fmt", "yuv420p", video], check=True, cwd=work)
subprocess.run(["ffmpeg", "-v", "error", "-y", "-i", video, "-vf",
                "split[a][b];[a]palettegen=max_colors=128:stats_mode=diff[p];"
                "[b][p]paletteuse=dither=none:diff_mode=rectangle", out], check=True)
print(f"{out}: {os.path.getsize(out) // 1024} KB, {length:.1f} s")
