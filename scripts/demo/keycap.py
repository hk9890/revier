# Draws a pressed key as keycaps on a transparent PNG, for compose.py to lay
# over a clip. Usage: keycap.py <font file> <label> <out.png>
# A label like "Ctrl+Shift+O" draws one cap per key.
import sys
from PIL import Image, ImageDraw, ImageFilter, ImageFont

font_file, label, out = sys.argv[1:4]
font = ImageFont.truetype(font_file, 30)
keys = label.split("+")
pad_x, height, gap, radius, shadow = 22, 60, 12, 12, 10

widths = [max(height, int(font.getlength(k)) + 2 * pad_x) for k in keys]
width = sum(widths) + gap * (len(keys) - 1)
image = Image.new("RGBA", (width + 2 * shadow, height + 2 * shadow + 4), (0, 0, 0, 0))

shade = Image.new("RGBA", image.size, (0, 0, 0, 0))
draw = ImageDraw.Draw(shade)
x = shadow
for w in widths:
    draw.rounded_rectangle((x, shadow + 4, x + w, shadow + 4 + height), radius, fill=(0, 0, 0, 150))
    x += w + gap
image.alpha_composite(shade.filter(ImageFilter.GaussianBlur(shadow / 2)))

draw = ImageDraw.Draw(image)
x = shadow
for key, w in zip(keys, widths):
    box = (x, shadow, x + w, shadow + height)
    draw.rounded_rectangle(box, radius, fill=(49, 50, 68, 245), outline=(108, 112, 134, 255), width=2)
    draw.rounded_rectangle((x + 3, shadow + height - 8, x + w - 3, shadow + height - 3), 4, fill=(30, 30, 46, 200))
    draw.text((x + w / 2, shadow + height / 2 - 3), key, font=font, fill=(205, 214, 244, 255), anchor="mm")
    x += w + gap
image.save(out)
