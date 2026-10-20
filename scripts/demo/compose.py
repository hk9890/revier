# Turns one clip's screencast and key log into an animated WebP, with each
# pressed key shown as keycaps in the bottom right corner.
# Usage: compose.py <work dir> <clip> <output.webp>
import os
import subprocess
import sys

work, clip, out = sys.argv[1], sys.argv[2], os.path.abspath(sys.argv[3])
here = os.path.dirname(os.path.abspath(__file__))
font = subprocess.run(["fc-match", "-f", "%{file}", "JetBrainsMono Nerd Font:bold"],
                      capture_output=True, text=True, check=True).stdout

events = [line.rstrip("\n").split(" ", 2) for line in open(f"{work}/{clip}.log")]
start = next(int(e[0]) for e in events if e[1] == "start")
length = (next(int(e[0]) for e in events if e[1] == "end") - start) / 1e9
keys = [((int(e[0]) - start) / 1e9, e[2]) for e in events if e[1] == "key"]

inputs, chain = ["-i", f"{work}/{clip}.webm"], []
# The crop takes off GNOME's top bar, which shows a dot while it records.
chain.append("[0]setpts=PTS-STARTPTS,fps=30,tpad=stop_mode=clone:stop_duration=10,"
             f"trim=duration={length:.2f},crop=iw:ih-ih*32/1080:0:ih*32/1080[v0];")
for i, (at, label) in enumerate(keys, 1):
    until = min(at + 1.3, keys[i][0] - 0.05 if i < len(keys) else length)
    png = f"{work}/{clip}-key{i}.png"
    subprocess.run([sys.executable, f"{here}/keycap.py", font, label, png], check=True)
    inputs += ["-loop", "1", "-framerate", "30", "-t", f"{length:.2f}", "-i", png]
    chain.append(f"[{i}]format=rgba,fade=in:st={at:.2f}:d=0.12:alpha=1,"
                 f"fade=out:st={until - 0.15:.2f}:d=0.15:alpha=1[k{i}];"
                 f"[v{i - 1}][k{i}]overlay=W-w-48:H-h-40:enable='between(t,{at:.2f},{until:.2f})'[v{i}];")
chain.append(f"[v{len(keys)}]fps=20,scale=1280:-1:flags=lanczos[out]")

subprocess.run(["ffmpeg", "-v", "error", "-y", *inputs, "-filter_complex", "".join(chain),
                "-map", "[out]", "-c:v", "libwebp_anim", "-q:v", "90", "-compression_level", "6",
                "-loop", "0", out], check=True)
print(f"{out}: {os.path.getsize(out) // 1024} KB, {length:.1f} s")
