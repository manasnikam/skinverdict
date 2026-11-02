# Make 5 "selfies" from one AI face: only framing/exposure change, skin untouched.
import sys
from pathlib import Path
from PIL import Image, ImageEnhance

d = Path(sys.argv[1])
src = Image.open(next(p for p in d.iterdir() if p.stem == "face")).convert("RGB")
w, h = src.size
# (crop shift x, y as fraction, rotation deg, brightness, warmth)
shots = [(0, 0, 0, 1.00, 0), (.015, -.01, .8, 1.04, 4), (-.01, .012, -.6, .97, -3), (.008, .006, .4, 1.02, 2), (-.012, -.008, -.9, .99, -2)]
dates = ["2026-10-05", "2026-10-12", "2026-10-19", "2026-10-26", "2026-11-02"]
rows = []
for i, (dx, dy, rot, br, warm) in enumerate(shots):
    im = src.rotate(rot, resample=Image.BICUBIC)
    m = int(w * .04)
    x, y = m + int(dx * w), m + int(dy * h)
    im = im.crop((x, y, x + w - 2 * m, y + h - 2 * m))
    im = ImageEnhance.Brightness(im).enhance(br)
    r, g, b = im.split()
    im = Image.merge("RGB", (r.point(lambda v: min(255, v + warm)), g, b.point(lambda v: max(0, v - warm))))
    name = f"day{i*7}.jpg"
    im.save(d / name, quality=92)
    rows.append(f"{name},{dates[i]}\n")
(d / "photos.csv").write_text("".join(rows))
print("wrote", len(rows), "selfies + photos.csv")
